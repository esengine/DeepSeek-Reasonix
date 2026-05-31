package sandbox

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// --- Spec.enforce ---

func TestEnforce(t *testing.T) {
	cases := []struct {
		mode string
		want bool
	}{
		{"", false},
		{"off", false},
		{"enforce", true},
		{"Enforce", false}, // case-sensitive
		{"something", false},
	}
	for _, c := range cases {
		s := Spec{Mode: c.mode}
		if got := s.enforce(); got != c.want {
			t.Errorf("Spec{%q}.enforce() = %v, want %v", c.mode, got, c.want)
		}
	}
}

// --- Spec zero value ---

func TestSpecZeroValue(t *testing.T) {
	var s Spec
	if s.enforce() {
		t.Error("zero-value Spec should not enforce")
	}
	if s.Network {
		t.Error("zero-value Spec should not allow network")
	}
	if len(s.WriteRoots) != 0 {
		t.Error("zero-value Spec should have no write roots")
	}
}

// --- Command (non-Darwin) ---

func TestCommandNonDarwin(t *testing.T) {
	if runtime.GOOS == "darwin" {
		t.Skip("testing non-darwin path")
	}
	// On non-darwin, Command always returns unwrapped.
	spec := Spec{Mode: "enforce", WriteRoots: []string{"/tmp"}}
	cmd, wrapped := Command(spec, "sh", "echo hi")
	if wrapped {
		t.Error("non-darwin should never wrap")
	}
	if len(cmd) != 3 || cmd[0] != "sh" || cmd[1] != "-c" || cmd[2] != "echo hi" {
		t.Errorf("unexpected cmd: %v", cmd)
	}
}

func TestCommandNonEnforce(t *testing.T) {
	spec := Spec{Mode: "off"}
	cmd, wrapped := Command(spec, "bash", "ls")
	if wrapped {
		t.Error("non-enforce should not wrap")
	}
	if cmd[0] != "bash" {
		t.Errorf("cmd[0] = %q, want bash", cmd[0])
	}
}

func TestCommandEmptyMode(t *testing.T) {
	spec := Spec{}
	cmd, wrapped := Command(spec, "sh", "echo hi")
	if wrapped {
		t.Error("empty mode should not wrap")
	}
	if len(cmd) != 3 {
		t.Errorf("cmd length = %d, want 3", len(cmd))
	}
}

// --- Available (non-Darwin) ---

func TestAvailableNonDarwin(t *testing.T) {
	if runtime.GOOS == "darwin" {
		t.Skip("testing non-darwin path")
	}
	if Available() {
		t.Error("non-darwin should report unavailable")
	}
}

// --- sbplString ---

func TestSbplString(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"/tmp", `"/tmp"`},
		{`/path/with"quote`, `"/path/with\"quote"`},
		{`/path/with\backslash`, `"/path/with\\backslash"`},
		{`/both"and\`, `"/both\"and\\"`},
		{"", `""`},
	}
	for _, c := range cases {
		got := sbplString(c.input)
		if got != c.want {
			t.Errorf("sbplString(%q) = %q, want %q", c.input, got, c.want)
		}
	}
}

// --- writeAllowDirs ---

func TestWriteAllowDirsDeduplication(t *testing.T) {
	dirs := writeAllowDirs([]string{"/tmp", "/tmp", "/tmp"})
	// /tmp may resolve to /private/tmp on macOS, so count unique.
	seen := map[string]bool{}
	for _, d := range dirs {
		if seen[d] {
			t.Errorf("duplicate dir: %s", d)
		}
		seen[d] = true
	}
}

func TestWriteAllowDirsIncludesRoots(t *testing.T) {
	root := t.TempDir()
	dirs := writeAllowDirs([]string{root})
	found := false
	for _, d := range dirs {
		real, _ := filepath.EvalSymlinks(root)
		if d == real {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("writeAllowDirs should include root %s, got %v", root, dirs)
	}
}

func TestWriteAllowDirsIncludesTemp(t *testing.T) {
	dirs := writeAllowDirs(nil)
	tmpDir := os.TempDir()
	realTmp, _ := filepath.EvalSymlinks(tmpDir)
	found := false
	for _, d := range dirs {
		if d == realTmp {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("writeAllowDirs should include temp dir %s, got %v", tmpDir, dirs)
	}
}

func TestWriteAllowDirsSkipsEmpty(t *testing.T) {
	dirs := writeAllowDirs([]string{"", "", ""})
	for _, d := range dirs {
		if d == "" {
			t.Error("writeAllowDirs should skip empty strings")
		}
	}
}

func TestWriteAllowDirsNoDuplicates(t *testing.T) {
	// Pass many overlapping paths and verify no duplicates in output.
	roots := []string{"/tmp", "/private/tmp", os.TempDir()}
	dirs := writeAllowDirs(roots)
	seen := map[string]bool{}
	for _, d := range dirs {
		if seen[d] {
			t.Errorf("duplicate: %s", d)
		}
		seen[d] = true
	}
}

// --- seatbeltProfile ---

func TestSeatbeltProfileDeniesNetwork(t *testing.T) {
	spec := Spec{Mode: "enforce", Network: false, WriteRoots: []string{"/workspace"}}
	profile := seatbeltProfile(spec)
	if !strings.Contains(profile, "(deny network*)") {
		t.Error("profile should deny network when Network=false")
	}
}

func TestSeatbeltProfileAllowsNetwork(t *testing.T) {
	spec := Spec{Mode: "enforce", Network: true, WriteRoots: []string{"/workspace"}}
	profile := seatbeltProfile(spec)
	if strings.Contains(profile, "(deny network*)") {
		t.Error("profile should not deny network when Network=true")
	}
}

func TestSeatbeltProfileContainsVersion(t *testing.T) {
	spec := Spec{Mode: "enforce", WriteRoots: []string{"/workspace"}}
	profile := seatbeltProfile(spec)
	if !strings.Contains(profile, "(version 1)") {
		t.Error("profile should contain version 1")
	}
	if !strings.Contains(profile, "(allow default)") {
		t.Error("profile should allow default")
	}
	if !strings.Contains(profile, "(deny file-write*)") {
		t.Error("profile should deny file-write")
	}
}

func TestSeatbeltProfileContainsRoots(t *testing.T) {
	root := t.TempDir()
	spec := Spec{Mode: "enforce", WriteRoots: []string{root}}
	profile := seatbeltProfile(spec)
	if !strings.Contains(profile, "(allow file-write*") {
		t.Error("profile should have allow file-write section")
	}
	if !strings.Contains(profile, "(subpath ") {
		t.Error("profile should contain subpath entries")
	}
}

// --- Command (Darwin) ---

func TestCommandDarwinEnforce(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("darwin-only test")
	}
	if !Available() {
		t.Skip("sandbox-exec not available")
	}
	spec := Spec{Mode: "enforce", WriteRoots: []string{"/workspace"}}
	cmd, wrapped := Command(spec, "sh", "echo hi")
	if !wrapped {
		t.Error("darwin enforce with sandbox-exec should wrap")
	}
	if cmd[0] != "sandbox-exec" {
		t.Errorf("cmd[0] = %q, want sandbox-exec", cmd[0])
	}
	if cmd[1] != "-p" {
		t.Errorf("cmd[1] = %q, want -p", cmd[1])
	}
	// cmd[2] is the profile, cmd[3] is the shell, cmd[4] is -c, cmd[5] is the command.
	if len(cmd) != 6 {
		t.Errorf("cmd length = %d, want 6", len(cmd))
	}
}

func TestCommandDarwinNonEnforce(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("darwin-only test")
	}
	spec := Spec{Mode: "off", WriteRoots: []string{"/workspace"}}
	cmd, wrapped := Command(spec, "sh", "echo hi")
	if wrapped {
		t.Error("non-enforce should not wrap even on darwin")
	}
	if cmd[0] != "sh" {
		t.Errorf("cmd[0] = %q, want sh", cmd[0])
	}
}
