package clipboard

import (
	"strings"
	"testing"
)

func TestReadUnsupported(t *testing.T) {
	orig := readCmd
	readCmd = nil
	defer func() { readCmd = orig }()

	_, err := Read()
	if err == nil {
		t.Fatal("expected error for unsupported platform")
	}
	if !strings.Contains(err.Error(), "not supported") {
		t.Fatalf("expected 'not supported' error, got %q", err.Error())
	}
}

func TestWriteUnsupported(t *testing.T) {
	orig := writeCmd
	writeCmd = nil
	defer func() { writeCmd = orig }()

	err := Write("test")
	if err == nil {
		t.Fatal("expected error for unsupported platform")
	}
	if !strings.Contains(err.Error(), "not supported") {
		t.Fatalf("expected 'not supported' error, got %q", err.Error())
	}
}

func TestProbeUnsupported(t *testing.T) {
	orig := writeCmd
	writeCmd = nil
	defer func() { writeCmd = orig }()

	err := Probe()
	if err == nil {
		t.Fatal("expected error for unsupported platform")
	}
	if !strings.Contains(err.Error(), "not supported") {
		t.Fatalf("expected 'not supported' error, got %q", err.Error())
	}
}

func TestReadSuccess(t *testing.T) {
	orig := readCmd
	readCmd = []string{"echo", "clipboard content"}
	defer func() { readCmd = orig }()

	out, err := Read()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.TrimSpace(out) != "clipboard content" {
		t.Fatalf("expected 'clipboard content', got %q", out)
	}
}

func TestReadTrailingNewline(t *testing.T) {
	orig := readCmd
	readCmd = []string{"echo", "data with newline"}
	defer func() { readCmd = orig }()

	out, err := Read()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "data with newline") {
		t.Fatalf("expected to contain 'data with newline', got %q", out)
	}
}

func TestReadCmdFailure(t *testing.T) {
	orig := readCmd
	readCmd = []string{"sh", "-c", "exit 1"}
	defer func() { readCmd = orig }()

	_, err := Read()
	if err == nil {
		t.Fatal("expected error from failing read command")
	}
	if !strings.Contains(err.Error(), "clipboard read") {
		t.Fatalf("expected error to mention 'clipboard read', got %q", err.Error())
	}
}

func TestWriteSuccess(t *testing.T) {
	orig := writeCmd
	writeCmd = []string{"tee"}
	defer func() { writeCmd = orig }()

	err := Write("hello clipboard")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestWriteCmdFailure(t *testing.T) {
	orig := writeCmd
	writeCmd = []string{"sh", "-c", "exit 1"}
	defer func() { writeCmd = orig }()

	err := Write("test")
	if err == nil {
		t.Fatal("expected error from failing write command")
	}
	if !strings.Contains(err.Error(), "clipboard write") {
		t.Fatalf("expected error to mention 'clipboard write', got %q", err.Error())
	}
}

func TestWriteCmdStderr(t *testing.T) {
	orig := writeCmd
	writeCmd = []string{"sh", "-c", "echo error msg >&2; exit 1"}
	defer func() { writeCmd = orig }()

	err := Write("test")
	if err == nil {
		t.Fatal("expected error from failing write command")
	}
	if !strings.Contains(err.Error(), "error msg") {
		t.Fatalf("expected stderr in error message, got %q", err.Error())
	}
}

func TestSuffixNil(t *testing.T) {
	if s := suffix(nil); s != "" {
		t.Fatalf("expected empty string, got %q", s)
	}
}

func TestSuffixEmpty(t *testing.T) {
	if s := suffix([]byte{}); s != "" {
		t.Fatalf("expected empty string, got %q", s)
	}
}

func TestSuffixNonEmpty(t *testing.T) {
	s := suffix([]byte("some error text"))
	want := ": some error text"
	if s != want {
		t.Fatalf("expected %q, got %q", want, s)
	}
}

func TestSuffixTrailingSpaces(t *testing.T) {
	s := suffix([]byte("  message with spaces  "))
	want := ": message with spaces"
	if s != want {
		t.Fatalf("expected %q, got %q", want, s)
	}
}

func TestProbeSuccess(t *testing.T) {
	orig := writeCmd
	writeCmd = []string{"tee"}
	defer func() { writeCmd = orig }()

	err := Probe()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestReadWhiteSpaceOutput(t *testing.T) {
	orig := readCmd
	readCmd = []string{"printf", "  spaced output  "}
	defer func() { readCmd = orig }()

	out, err := Read()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out != "  spaced output  " {
		t.Fatalf("expected '  spaced output  ', got %q", out)
	}
}

func TestReadMissingCmd(t *testing.T) {
	orig := readCmd
	readCmd = []string{"nonexistent_command_xyz"}
	defer func() { readCmd = orig }()

	_, err := Read()
	if err == nil {
		t.Fatal("expected error for missing command")
	}
}
