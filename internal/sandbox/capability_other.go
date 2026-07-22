//go:build !darwin && !windows

package sandbox

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"sync"
	"time"
)

var (
	bwrapCapabilityUsability sync.Map // resolved executable path -> bool
	bwrapNetworkUsability    sync.Map // resolved executable path -> bool
	bwrapDeviceUsability     sync.Map // resolved executable path -> bool
)

func capabilityPlatformNoDelta() bool { return runtime.GOOS != "linux" }

func capabilityBaseWriteRoots(spec Spec) []string {
	roots := append([]string(nil), spec.WriteRoots...)
	if !spec.MinimalWrites && runtime.GOOS == "linux" {
		roots = append(roots, linuxWriteDirs()...)
	}
	return roots
}

// capabilityBaseReadCovers reports whether the base sandbox already exposes
// the complete requested host scope. Linux replaces several host mount trees;
// write roots are bound after those replacements and therefore re-expose only
// their complete subtrees. Other Unix backends represented by this file leave
// host reads visible by default.
func capabilityBaseReadCovers(spec Spec, requested CapabilityPath) bool {
	if runtime.GOOS != "linux" {
		return true
	}
	for _, root := range []string{"/tmp", "/dev", "/proc"} {
		replacement, ok := existingCapabilityRoot(root)
		if !ok {
			return false
		}
		if capabilityPathsIntersect(requested, replacement) {
			return capabilityPathCoveredByRoots(requested, capabilityBaseWriteRoots(spec))
		}
	}
	return true
}

func capabilityPlatformSupports(_ context.Context, base Spec, delta CapabilitySet) (bool, string) {
	if runtime.GOOS != "linux" {
		return false, "path and network relaxation are supported only on Linux and macOS"
	}
	if capabilitySetContainsDevScope(delta) {
		return false, "Linux device paths require an explicit device capability; ordinary /dev path relaxation is unsupported"
	}
	bwrap, ok := usableBwrap()
	if !ok {
		return false, "bubblewrap is unavailable"
	}
	if len(delta.Reads)+len(delta.Writes) == 0 {
		if len(delta.Devices) == 0 {
			return true, ""
		}
	}
	if !base.Network && !delta.Network && !usableBwrapNetworkIsolation(bwrap) {
		return false, "bubblewrap cannot create the network namespace required by the base sandbox"
	}
	if len(delta.Reads)+len(delta.Writes) > 0 && !usableBwrapCapabilityPaths(bwrap) {
		return false, "bubblewrap does not pass the descriptor-bound mount probe"
	}
	if len(delta.Devices) > 0 && !usableBwrapDevices(bwrap) {
		return false, "bubblewrap does not pass the exact-device --dev-bind behavioral probe"
	}
	return true, ""
}

func capabilitySetContainsDevScope(set CapabilitySet) bool {
	dev := CapabilityPath{Canonical: "/dev", Kind: CapabilityDirectory}
	if canonical, ok := existingCapabilityRoot("/dev"); ok {
		dev = canonical
	}
	for _, path := range append(append([]CapabilityPath(nil), set.Reads...), set.Writes...) {
		if capabilityPathsIntersect(dev, path) {
			return true
		}
	}
	return false
}

func usableBwrapNetworkIsolation(bwrap string) bool {
	if cached, ok := bwrapNetworkUsability.Load(bwrap); ok {
		return cached.(bool)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	usable := exec.CommandContext(ctx, bwrap,
		"--unshare-net",
		"--ro-bind", "/", "/",
		"--dev", "/dev",
		"--proc", "/proc",
		"--", "true",
	).Run() == nil
	actual, _ := bwrapNetworkUsability.LoadOrStore(bwrap, usable)
	return actual.(bool)
}

func usableBwrapCapabilityPaths(bwrap string) bool {
	if cached, ok := bwrapCapabilityUsability.Load(bwrap); ok {
		return cached.(bool)
	}
	roDir, err := os.MkdirTemp("", "reasonix-bwrap-ro-*")
	if err != nil {
		return false
	}
	defer os.RemoveAll(roDir)
	rwDir, err := os.MkdirTemp("", "reasonix-bwrap-rw-*")
	if err != nil {
		return false
	}
	defer os.RemoveAll(rwDir)
	fileDir, err := os.MkdirTemp("", "reasonix-bwrap-files-*")
	if err != nil {
		return false
	}
	defer os.RemoveAll(fileDir)
	roPath := fileDir + "/read-only"
	rwPath := fileDir + "/writable"
	if err := os.WriteFile(roPath, []byte("read-only"), 0o600); err != nil {
		return false
	}
	if err := os.WriteFile(rwPath, []byte("before"), 0o600); err != nil {
		return false
	}
	roFD, err := openCapabilityDescriptor(CapabilityPath{Canonical: roDir, Kind: CapabilityDirectory})
	if err != nil {
		return false
	}
	defer roFD.Close()
	rwFD, err := openCapabilityDescriptor(CapabilityPath{Canonical: rwDir, Kind: CapabilityDirectory})
	if err != nil {
		return false
	}
	defer rwFD.Close()
	roFileFD, err := openCapabilityDescriptor(CapabilityPath{Canonical: roPath, Kind: CapabilityFile})
	if err != nil {
		return false
	}
	defer roFileFD.Close()
	rwFileFD, err := openCapabilityDescriptor(CapabilityPath{Canonical: rwPath, Kind: CapabilityFile})
	if err != nil {
		return false
	}
	defer rwFileFD.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	dirProbe := rwDir + "/probe"
	roDirProbe := roDir + "/rejected"
	cmd := exec.CommandContext(ctx, bwrap,
		"--ro-bind", "/", "/",
		"--dev", "/dev",
		"--proc", "/proc",
		"--tmpfs", roDir,
		"--tmpfs", rwDir,
		"--tmpfs", fileDir,
		"--ro-bind-fd", "3", roDir,
		"--bind-fd", "4", rwDir,
		"--ro-bind-fd", "5", roPath,
		"--bind-fd", "6", rwPath,
		"--", "/bin/sh", "-c",
		"test -d \"$1\" && "+
			"! ( : > \"$2\" ) 2>/dev/null && "+
			": > \"$3\" && "+
			"test \"$(cat \"$4\")\" = read-only && "+
			"! ( printf changed > \"$4\" ) 2>/dev/null && "+
			"printf changed > \"$5\"",
		"sh", roDir, roDirProbe, dirProbe, roPath, rwPath,
	)
	cmd.ExtraFiles = []*os.File{roFD, rwFD, roFileFD, rwFileFD}
	usable := cmd.Run() == nil
	if usable {
		_, dirWriteErr := os.Stat(dirProbe)
		_, roDirWriteErr := os.Stat(roDirProbe)
		roContents, roReadErr := os.ReadFile(roPath)
		rwContents, rwReadErr := os.ReadFile(rwPath)
		usable = dirWriteErr == nil && os.IsNotExist(roDirWriteErr) &&
			roReadErr == nil && string(roContents) == "read-only" &&
			rwReadErr == nil && string(rwContents) == "changed"
	}
	actual, _ := bwrapCapabilityUsability.LoadOrStore(bwrap, usable)
	return actual.(bool)
}

func usableBwrapDevices(bwrap string) bool {
	if cached, ok := bwrapDeviceUsability.Load(bwrap); ok {
		return cached.(bool)
	}
	device, err := inspectCapabilityDevice("/dev/null")
	if err != nil || device.Kind != CapabilityCharacterDevice {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	const probeDestination = "/dev/reasonix-device-capability-probe"
	usable := exec.CommandContext(ctx, bwrap,
		"--ro-bind", "/", "/",
		"--dev", "/dev",
		"--proc", "/proc",
		"--dev-bind", device.Canonical, probeDestination,
		"--", "/bin/sh", "-c",
		`test -c "$0" && dd if="$0" of=/dev/null count=1 status=none`, probeDestination,
	).Run() == nil
	actual, _ := bwrapDeviceUsability.LoadOrStore(bwrap, usable)
	return actual.(bool)
}

func prepareCapabilityPlatformLaunch(_ context.Context, base Spec, delta CapabilitySet, sh Shell, command string) (CapabilityLaunch, error) {
	if runtime.GOOS != "linux" {
		return CapabilityLaunch{}, fmt.Errorf("capability relaxation is unsupported on %s", runtime.GOOS)
	}
	bwrap, ok := usableBwrap()
	if !ok {
		return CapabilityLaunch{}, fmt.Errorf("bubblewrap is unavailable")
	}
	if len(delta.Reads)+len(delta.Writes) > 0 && !usableBwrapCapabilityPaths(bwrap) {
		return CapabilityLaunch{}, fmt.Errorf("descriptor-bound Bubblewrap mounts are unavailable")
	}
	if len(delta.Devices) > 0 && !usableBwrapDevices(bwrap) {
		return CapabilityLaunch{}, fmt.Errorf("exact-device Bubblewrap --dev-bind is unavailable")
	}

	files := make([]*os.File, 0, len(delta.Reads)+len(delta.Writes))
	closeFiles := func() {
		for _, file := range files {
			_ = file.Close()
		}
	}
	for _, path := range append(append([]CapabilityPath(nil), delta.Reads...), delta.Writes...) {
		file, err := openCapabilityDescriptor(path)
		if err != nil {
			closeFiles()
			return CapabilityLaunch{}, fmt.Errorf("open descriptor for %q: %w", path.Canonical, err)
		}
		files = append(files, file)
	}
	activation, statusWriter, err := newCapabilityActivationWitness()
	if err != nil {
		closeFiles()
		return CapabilityLaunch{}, fmt.Errorf("create Bubblewrap activation witness: %w", err)
	}
	statusFD := 3 + len(files)
	files = append(files, statusWriter)

	spec := cloneSpec(base)
	if delta.Network {
		spec.Network = true
	}
	args := []string{
		"--unshare-net",
		"--ro-bind", "/", "/",
		"--dev", "/dev",
		"--proc", "/proc",
		"--tmpfs", "/tmp",
	}
	if spec.Network {
		args = args[1:]
	}
	for _, root := range spec.WriteRoots {
		args = append(args, "--bind", root, root)
	}
	if !spec.MinimalWrites {
		for _, root := range linuxWriteDirs() {
			args = append(args, "--bind", root, root)
		}
	}
	args = append(args, bwrapForbidReadArgs(spec.ForbidReadRoots)...)
	fd := 3
	for _, path := range delta.Reads {
		args = append(args, "--ro-bind-fd", strconv.Itoa(fd), path.Canonical)
		fd++
	}
	for _, path := range delta.Writes {
		args = append(args, "--bind-fd", strconv.Itoa(fd), path.Canonical)
		fd++
	}
	for _, device := range delta.Devices {
		args = append(args, "--dev-bind", device.Canonical, device.Canonical)
	}
	args = append(args, "--json-status-fd", strconv.Itoa(statusFD))
	args = append(args, sh.argv(command)...)
	launch := CapabilityLaunch{Argv: append([]string{bwrap}, args...), ExtraFiles: files, Wrapped: true, activation: activation}
	if len(delta.Devices) > 0 {
		launch.Materialization = CapabilityMaterializationPathStringDevBind
	}
	return launch, nil
}
