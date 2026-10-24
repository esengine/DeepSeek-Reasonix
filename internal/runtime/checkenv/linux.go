//go:build linux

package checkenv

import (
	"context"
	"errors"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"slices"
	"strings"
	"sync"

	"reasonix/internal/base/filelock"
	"reasonix/internal/base/fileutil"
	"reasonix/internal/contract/tool"
	"reasonix/internal/safety/evidence"
)

func (i *Invocation) Prepare(ctx context.Context, request tool.ControlledShellRequest) ([]string, error) {
	i.mu.Lock()
	defer i.mu.Unlock()
	if i.state.closed || i.state.directory != "" || request.Command != i.command || i.command == "" || i.executionID == "" || i.callID == "" {
		return nil, errors.New("verification invocation is stale or changed")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := i.freeze(ctx, request); err != nil {
		i.remove()
		return nil, err
	}
	argv, err := i.launchArguments(ctx, request)
	if err != nil {
		i.remove()
		return nil, err
	}
	i.state.argv = argv
	return append([]string(nil), argv...), nil
}

func (i *Invocation) freeze(ctx context.Context, request tool.ControlledShellRequest) error {
	if !filepath.IsAbs(i.spec.RuntimeRoot) || !filepath.IsAbs(i.spec.StorageRoot) || !filepath.IsAbs(i.spec.SourceRoot) || !filepath.IsAbs(i.spec.Directory) {
		return ErrUnsupported
	}
	source, err := filepath.EvalSymlinks(i.spec.SourceRoot)
	if err != nil {
		return err
	}
	directory, err := filepath.EvalSymlinks(i.spec.Directory)
	if err != nil {
		return err
	}
	requested, err := filepath.EvalSymlinks(request.Directory)
	if err != nil || requested != directory {
		return errors.Join(ErrUnsupported, err)
	}
	if rel, err := filepath.Rel(source, directory); err != nil || !filepath.IsLocal(rel) {
		return ErrUnsupported
	}
	runtimeRoot, err := filepath.EvalSymlinks(i.spec.RuntimeRoot)
	if err != nil {
		return err
	}
	storage, err := fileutil.CanonicalWritePath(i.spec.StorageRoot)
	if err != nil {
		return err
	}
	for _, input := range []string{source, runtimeRoot} {
		if rel, err := filepath.Rel(input, storage); err != nil || filepath.IsLocal(rel) {
			return errors.New("verification storage intersects its input")
		}
	}
	denied, err := relativeDenies(source, request.ReadDenies)
	if err != nil {
		return err
	}
	runtimeDenied, err := relativeDenies(runtimeRoot, request.ReadDenies)
	if err != nil || len(runtimeDenied) != 0 {
		return errors.Join(err, errors.New("verification runtime intersects a forbidden read root"))
	}
	i.spec.SourceRoot, i.spec.Directory = source, directory
	i.state.sourcePolicy = treePolicy{source: true, denied: denied}
	i.state.input = evidence.CheckInputs{Directory: request.Directory, Platform: runtime.GOOS + "/" + runtime.GOARCH,
		CommandDigest: evidence.CheckCommandDigest(i.command), Gaps: []string{"controlled_environment_incomplete"}}
	i.state.input.Before = evidence.CaptureContentSnapshot(ctx, source, 50_000)
	if !i.state.input.Before.Complete {
		return errors.New("verification source observation is incomplete")
	}
	if err := os.MkdirAll(storage, 0700); err != nil {
		return err
	}
	if err := recoverContainerJournals(ctx, storage, "", nil); err != nil {
		return err
	}
	i.state.directory, err = os.MkdirTemp(storage, "check-")
	if err != nil {
		return err
	}
	i.state.release, err = filelock.TryAcquire(filepath.Join(i.state.directory, "owner.lock"))
	if err != nil {
		return err
	}
	i.state.runtimeRoot = filepath.Join(i.state.directory, "runtime")
	if _, err = freezeTree(ctx, runtimeRoot, i.state.runtimeRoot, treePolicy{}); err != nil {
		return err
	}
	i.state.sourceRoot = filepath.Join(i.state.directory, "source")
	i.state.originalDigest, err = freezeTree(ctx, source, i.state.sourceRoot, i.state.sourcePolicy)
	if err != nil {
		return err
	}
	i.state.sourceDigest, err = i.spec.applyCapture(ctx, i.state.sourceRoot, i.state.sourcePolicy)
	if err != nil {
		return err
	}
	for _, path := range []string{source, "/tmp", "/home", "/proc", "/dev"} {
		if err := ensureMountpoint(i.state.runtimeRoot, path); err != nil {
			return err
		}
	}
	entries, err := scanTree(ctx, i.state.runtimeRoot, treePolicy{})
	if err != nil {
		return err
	}
	i.state.runtimeDigest = digest(entries)
	return nil
}

func ensureMountpoint(rootPath, virtual string) error {
	root, err := os.OpenRoot(rootPath)
	if err != nil {
		return err
	}
	defer root.Close()
	path := "."
	for component := range strings.SplitSeq(strings.TrimPrefix(filepath.Clean(virtual), "/"), "/") {
		path = filepath.Join(path, component)
		if err := root.Mkdir(path, 0755); err != nil && !os.IsExist(err) {
			return err
		}
		info, err := root.Lstat(path)
		if err != nil || !info.IsDir() {
			return errors.Join(err, errors.New("verification mountpoint contains an alias"))
		}
	}
	return nil
}

func (i *Invocation) launchArguments(ctx context.Context, request tool.ControlledShellRequest) ([]string, error) {
	launcher, err := exec.LookPath("bwrap")
	if err != nil {
		return nil, errors.Join(ErrUnsupported, err)
	}
	launcher, err = filepath.EvalSymlinks(launcher)
	if err != nil {
		return nil, err
	}
	i.state.launcher, err = fileutil.ReadContentVersion(ctx, launcher)
	if err != nil || i.state.launcher.Kind != "file" {
		return nil, errors.Join(ErrUnsupported, err)
	}
	interpreter, err := virtualFile(i.state.runtimeRoot, "/bin/bash")
	if err != nil {
		return nil, err
	}
	i.state.input.Interpreter, err = fileutil.ReadContentVersion(ctx, interpreter)
	if err != nil || i.state.input.Interpreter.Kind != "file" {
		return nil, errors.Join(ErrUnsupported, err)
	}
	i.state.input.Executable = i.state.launcher
	argv := []string{launcher, "--unshare-user", "--unshare-pid", "--as-pid-1", "--unshare-net", "--unshare-ipc", "--unshare-uts",
		"--disable-userns", "--new-session", "--die-with-parent", "--cap-drop", "ALL", "--clearenv", "--json-status-fd", "3",
		"--info-fd", "4", "--block-fd", "5",
		"--ro-bind", i.state.runtimeRoot, "/", "--proc", "/proc", "--dev", "/dev", "--tmpfs", "/tmp", "--tmpfs", "/home",
		"--ro-bind", i.state.sourceRoot, i.spec.SourceRoot,
		"--setenv", "HOME", "/home", "--setenv", "TMPDIR", "/tmp", "--setenv", "PATH", "/usr/local/bin:/usr/bin:/bin",
		"--setenv", "LANG", "C", "--setenv", "LC_ALL", "C", "--chdir", i.spec.Directory}
	for _, key := range slices.Sorted(maps.Keys(request.DeclaredEnvironment)) {
		if key == "" || strings.ContainsAny(key, "=\x00") || strings.ContainsRune(request.DeclaredEnvironment[key], 0) {
			return nil, errors.New("invalid declared verification environment")
		}
		argv = append(argv, "--setenv", key, request.DeclaredEnvironment[key])
	}
	payload, err := tool.BashVerificationPayload(i.command, request.PipeStatusMarker)
	if err != nil {
		return nil, err
	}
	return append(argv, "--", "/bin/bash", "--noprofile", "--norc", "-c", payload), nil
}

// Command verifies the invocation before supplying its only extra descriptor
// and inherited environment. Finish reads the actual process and namespace status.
func (i *Invocation) Command(ctx context.Context, launch tool.ShellLaunch) (*exec.Cmd, func(), error) {
	i.mu.Lock()
	defer i.mu.Unlock()
	directory, directoryErr := filepath.EvalSymlinks(launch.Directory)
	if i.state.closed || i.state.active || i.state.cmd != nil || len(i.state.argv) == 0 ||
		directoryErr != nil || launch.Command != i.command || directory != i.spec.Directory || !reflect.DeepEqual(launch.Arguments, i.state.argv) {
		return nil, nil, errors.New("verification launch identity changed")
	}
	status, err := os.CreateTemp(i.state.directory, "namespace-status-")
	if err != nil {
		return nil, nil, err
	}
	if err := i.scope.prepare(); err != nil {
		status.Close()
		return nil, nil, err
	}
	i.scope.ctx = ctx
	cmd := exec.CommandContext(ctx, i.state.argv[0], i.state.argv[1:]...)
	cmd.Dir, cmd.Env, cmd.ExtraFiles = "/", []string{"PATH=/usr/bin:/bin", "LANG=C", "LC_ALL=C"}, []*os.File{status, i.scope.infoWrite, i.scope.block}
	cmd.Cancel = i.Cancel
	i.state.status, i.state.cmd, i.state.ctx, i.state.active = status, cmd, ctx, true
	i.state.input.ArgumentsDigest, i.state.input.EnvironmentHash = digest(cmd.Args), digest(cmd.Environ())
	var once sync.Once
	return cmd, func() { once.Do(i.finish) }, nil
}
