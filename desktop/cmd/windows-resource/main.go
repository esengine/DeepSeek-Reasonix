// Command windows-resource stamps Reasonix branding and metadata into a Windows
// support executable after it has been built. Wails already uses the same
// winres library for the desktop executable; keeping the support binaries on the
// same resource path avoids generic Explorer, shortcut, and taskbar icons.
package main

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/tc-hib/winres"
	"github.com/tc-hib/winres/version"
)

const (
	companyName = "Reasonix"
	copyright   = "Copyright © 2026 Reasonix Contributors"
)

type resourceOptions struct {
	executable      string
	icon            string
	numericVersion  string
	fileDescription string
	internalName    string
	originalName    string
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "windows-resource:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	fs := flag.NewFlagSet("windows-resource", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	var opts resourceOptions
	fs.StringVar(&opts.executable, "exe", "", "Windows executable to stamp")
	fs.StringVar(&opts.icon, "icon", "", "ICO file to embed")
	fs.StringVar(&opts.numericVersion, "version", "", "numeric X.Y.Z version")
	fs.StringVar(&opts.fileDescription, "description", "", "Windows FileDescription")
	fs.StringVar(&opts.internalName, "internal-name", "", "Windows InternalName")
	fs.StringVar(&opts.originalName, "original-filename", "", "Windows OriginalFilename")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("unexpected arguments: %s", strings.Join(fs.Args(), " "))
	}
	for name, value := range map[string]string{
		"-exe":               opts.executable,
		"-icon":              opts.icon,
		"-version":           opts.numericVersion,
		"-description":       opts.fileDescription,
		"-internal-name":     opts.internalName,
		"-original-filename": opts.originalName,
	} {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("%s is required", name)
		}
	}
	return stampExecutable(opts)
}

func stampExecutable(opts resourceOptions) error {
	numericVersion, err := parseNumericVersion(opts.numericVersion)
	if err != nil {
		return err
	}

	iconFile, err := os.Open(opts.icon)
	if err != nil {
		return fmt.Errorf("open icon: %w", err)
	}
	ico, err := winres.LoadICO(iconFile)
	closeErr := iconFile.Close()
	if err != nil {
		return fmt.Errorf("load icon: %w", err)
	}
	if closeErr != nil {
		return fmt.Errorf("close icon: %w", closeErr)
	}

	rs := winres.ResourceSet{}
	if err := rs.SetIcon(winres.ID(1), ico); err != nil {
		return fmt.Errorf("set icon: %w", err)
	}
	rs.SetManifest(winres.AppManifest{
		Identity: winres.AssemblyIdentity{
			Name:    "Reasonix." + opts.internalName,
			Version: numericVersion,
		},
		Description:         opts.fileDescription,
		ExecutionLevel:      winres.AsInvoker,
		DPIAwareness:        winres.DPIPerMonitorV2,
		LongPathAware:       true,
		UseCommonControlsV6: true,
	})

	info := version.Info{
		FileVersion:    numericVersion,
		ProductVersion: numericVersion,
	}
	for key, value := range map[string]string{
		version.CompanyName:      companyName,
		version.FileDescription:  opts.fileDescription,
		version.FileVersion:      opts.numericVersion,
		version.InternalName:     opts.internalName,
		version.LegalCopyright:   copyright,
		version.OriginalFilename: opts.originalName,
		version.ProductName:      "Reasonix",
		version.ProductVersion:   opts.numericVersion,
		version.Comments:         "Reasonix desktop support component.",
	} {
		if err := info.Set(version.LangDefault, key, value); err != nil {
			return fmt.Errorf("set version field %s: %w", key, err)
		}
	}
	rs.SetVersionInfo(info)

	source, err := os.ReadFile(opts.executable)
	if err != nil {
		return fmt.Errorf("read executable: %w", err)
	}
	var stamped bytes.Buffer
	if err := rs.WriteToEXE(&stamped, bytes.NewReader(source)); err != nil {
		return fmt.Errorf("write resources: %w", err)
	}
	if err := verifyResources(stamped.Bytes(), opts, numericVersion); err != nil {
		return fmt.Errorf("verify resources: %w", err)
	}

	mode := os.FileMode(0o755)
	if stat, err := os.Stat(opts.executable); err == nil {
		mode = stat.Mode()
	}
	if err := replaceFile(opts.executable, stamped.Bytes(), mode); err != nil {
		return fmt.Errorf("replace executable: %w", err)
	}
	return nil
}

func parseNumericVersion(value string) ([4]uint16, error) {
	parts := strings.Split(strings.TrimSpace(value), ".")
	if len(parts) != 3 && len(parts) != 4 {
		return [4]uint16{}, fmt.Errorf("version %q must have three or four numeric parts", value)
	}
	var parsed [4]uint16
	for i, part := range parts {
		if part == "" {
			return [4]uint16{}, fmt.Errorf("version %q contains an empty part", value)
		}
		n, err := strconv.ParseUint(part, 10, 16)
		if err != nil {
			return [4]uint16{}, fmt.Errorf("version %q is not a Windows numeric version: %w", value, err)
		}
		parsed[i] = uint16(n)
	}
	return parsed, nil
}

func verifyResources(executable []byte, opts resourceOptions, numericVersion [4]uint16) error {
	rs, err := winres.LoadFromEXE(bytes.NewReader(executable))
	if err != nil {
		return err
	}
	if _, err := rs.GetIcon(winres.ID(1)); err != nil {
		return fmt.Errorf("application icon: %w", err)
	}
	manifest := rs.Get(winres.RT_MANIFEST, winres.ID(1), winres.LCIDDefault)
	if len(manifest) == 0 || !bytes.Contains(manifest, []byte(`requestedExecutionLevel level="asInvoker"`)) {
		return errors.New("asInvoker application manifest is missing")
	}
	versionData := rs.Get(winres.RT_VERSION, winres.ID(1), version.LangDefault)
	info, err := version.FromBytes(versionData)
	if err != nil {
		return fmt.Errorf("version info: %w", err)
	}
	if info.FileVersion != numericVersion || info.ProductVersion != numericVersion {
		return fmt.Errorf("numeric version mismatch: file=%v product=%v want=%v", info.FileVersion, info.ProductVersion, numericVersion)
	}
	table := info.Table()[version.LangDefault]
	if table == nil {
		return errors.New("English version string table is missing")
	}
	for key, want := range map[string]string{
		version.CompanyName:      companyName,
		version.FileDescription:  opts.fileDescription,
		version.InternalName:     opts.internalName,
		version.OriginalFilename: opts.originalName,
		version.ProductName:      "Reasonix",
	} {
		if got := (*table)[key]; got != want {
			return fmt.Errorf("version field %s=%q, want %q", key, got, want)
		}
	}
	return nil
}

func replaceFile(path string, data []byte, mode os.FileMode) error {
	temp, err := os.CreateTemp(filepath.Dir(path), ".reasonix-resource-*.exe")
	if err != nil {
		return err
	}
	tempName := temp.Name()
	defer os.Remove(tempName)
	if err := temp.Chmod(mode); err != nil {
		temp.Close()
		return err
	}
	if _, err := temp.Write(data); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Sync(); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	return os.Rename(tempName, path)
}
