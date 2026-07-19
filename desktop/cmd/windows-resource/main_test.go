package main

import (
	"bytes"
	"debug/pe"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/tc-hib/winres"
	"github.com/tc-hib/winres/version"
)

func TestParseNumericVersion(t *testing.T) {
	for _, test := range []struct {
		value string
		want  [4]uint16
	}{
		{"1.17.13", [4]uint16{1, 17, 13, 0}},
		{"2.3.4.5", [4]uint16{2, 3, 4, 5}},
	} {
		got, err := parseNumericVersion(test.value)
		if err != nil {
			t.Fatalf("parseNumericVersion(%q): %v", test.value, err)
		}
		if got != test.want {
			t.Fatalf("parseNumericVersion(%q) = %v, want %v", test.value, got, test.want)
		}
	}
	for _, value := range []string{"", "1.2", "1.2.3-beta", "1.2.70000"} {
		if _, err := parseNumericVersion(value); err == nil {
			t.Errorf("parseNumericVersion(%q) unexpectedly succeeded", value)
		}
	}
}

func TestStampExecutableSupportsWindowsReleaseArchitectures(t *testing.T) {
	icon, err := filepath.Abs(filepath.Join("..", "..", "build", "windows", "icon.ico"))
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		arch    string
		machine uint16
	}{
		{"amd64", pe.IMAGE_FILE_MACHINE_AMD64},
		{"arm64", pe.IMAGE_FILE_MACHINE_ARM64},
	} {
		t.Run(test.arch, func(t *testing.T) {
			dir := t.TempDir()
			source := filepath.Join(dir, "main.go")
			if err := os.WriteFile(source, []byte("package main\nfunc main() {}\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			executable := filepath.Join(dir, "reasonix-launcher.exe")
			cmd := exec.Command("go", "build", "-trimpath", "-o", executable, source)
			cmd.Env = append(os.Environ(), "GOOS=windows", "GOARCH="+test.arch, "CGO_ENABLED=0")
			if output, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("cross-build %s fixture: %v\n%s", test.arch, err, output)
			}

			opts := resourceOptions{
				executable:      executable,
				icon:            icon,
				numericVersion:  "1.17.13",
				fileDescription: "Reasonix Launcher",
				internalName:    "reasonix-launcher",
				originalName:    "reasonix-launcher.exe",
			}
			if err := stampExecutable(opts); err != nil {
				t.Fatal(err)
			}

			peFile, err := pe.Open(executable)
			if err != nil {
				t.Fatal(err)
			}
			if peFile.Machine != test.machine {
				t.Errorf("PE machine = %#x, want %#x", peFile.Machine, test.machine)
			}
			peFile.Close()

			data, err := os.ReadFile(executable)
			if err != nil {
				t.Fatal(err)
			}
			rs, err := winres.LoadFromEXE(bytes.NewReader(data))
			if err != nil {
				t.Fatal(err)
			}
			if _, err := rs.GetIcon(winres.ID(1)); err != nil {
				t.Fatalf("embedded icon: %v", err)
			}
			info, err := version.FromBytes(rs.Get(winres.RT_VERSION, winres.ID(1), version.LangDefault))
			if err != nil {
				t.Fatal(err)
			}
			if got := (*info.Table()[version.LangDefault])[version.OriginalFilename]; got != "reasonix-launcher.exe" {
				t.Errorf("OriginalFilename = %q", got)
			}
			manifest := rs.Get(winres.RT_MANIFEST, winres.ID(1), winres.LCIDDefault)
			if !bytes.Contains(manifest, []byte(`dpiAwareness xmlns="http://schemas.microsoft.com/SMI/2016/WindowsSettings">permonitorv2,system`)) {
				t.Errorf("per-monitor-v2 manifest setting is missing: %s", manifest)
			}
		})
	}
}

func TestReplaceFilePreservesMode(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows does not expose Unix executable mode bits")
	}
	path := filepath.Join(t.TempDir(), "program.exe")
	if err := os.WriteFile(path, []byte("old"), 0o751); err != nil {
		t.Fatal(err)
	}
	if err := replaceFile(path, []byte("new"), 0o751); err != nil {
		t.Fatal(err)
	}
	stat, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := stat.Mode().Perm(); got != 0o751 {
		t.Fatalf("mode = %#o, want 0751", got)
	}
}
