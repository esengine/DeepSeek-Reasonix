package sandbox

import (
	"runtime"
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

// --- Command ---

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

// --- Command (platform-specific) ---

func TestCommandNonDarwin(t *testing.T) {
	if runtime.GOOS == "darwin" {
		t.Skip("testing non-darwin path")
	}
	spec := Spec{Mode: "enforce", WriteRoots: []string{"/tmp"}}
	cmd, wrapped := Command(spec, "sh", "echo hi")
	if wrapped {
		t.Error("non-darwin should never wrap")
	}
	if len(cmd) != 3 || cmd[0] != "sh" || cmd[1] != "-c" || cmd[2] != "echo hi" {
		t.Errorf("unexpected cmd: %v", cmd)
	}
}

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
	if len(cmd) != 6 {
		t.Errorf("cmd length = %d, want 6", len(cmd))
	}
}

func TestCommandDarwinNonEnforce(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("darwin-only test")
	}
	spec := Spec{Mode: "off", WriteRoots: []string{"/workspace"}}
	_, wrapped := Command(spec, "sh", "echo hi")
	if wrapped {
		t.Error("non-enforce should not wrap even on darwin")
	}
}

// --- Available ---

func TestAvailableNonDarwin(t *testing.T) {
	if runtime.GOOS == "darwin" {
		t.Skip("testing non-darwin path")
	}
	if Available() {
		t.Error("non-darwin should report unavailable")
	}
}
