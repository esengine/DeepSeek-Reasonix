package cli

import "testing"

// TestMouseCaptureDefaultsOffOverSSH pins the #8345 behavior: mouse capture
// (which blocks the terminal's native selection over SSH) starts off in a
// remote session unless the user forces it back on.
func TestMouseCaptureDefaultsOffOverSSH(t *testing.T) {
	for _, k := range []string{"SSH_CONNECTION", "SSH_CLIENT", "SSH_TTY", "REASONIX_DISABLE_MOUSE"} {
		t.Setenv(k, "")
	}
	if mouseCaptureOffByDefault() {
		t.Fatal("local sessions keep in-app mouse capture on by default")
	}
	t.Setenv("SSH_TTY", "/dev/pts/3")
	if !mouseCaptureOffByDefault() {
		t.Fatal("SSH sessions must start with native mouse (capture off)")
	}
	t.Setenv("REASONIX_DISABLE_MOUSE", "0")
	if mouseCaptureOffByDefault() {
		t.Fatal("REASONIX_DISABLE_MOUSE=0 must force capture on even over SSH")
	}
	t.Setenv("REASONIX_DISABLE_MOUSE", "1")
	t.Setenv("SSH_TTY", "")
	if !mouseCaptureOffByDefault() {
		t.Fatal("REASONIX_DISABLE_MOUSE=1 keeps capture off locally")
	}
}
