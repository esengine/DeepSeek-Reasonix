package termrender

import (
	"errors"
	"testing"
)

func TestCopyToClipboard(t *testing.T) {
	t.Setenv("SSH_CONNECTION", "")
	t.Setenv("SSH_CLIENT", "")
	t.Setenv("SSH_TTY", "")
	previous := writeNativeClipboardText
	t.Cleanup(func() { writeNativeClipboardText = previous })

	var written string
	writeNativeClipboardText = func(text string) error {
		written = text
		return nil
	}
	message := CopyToClipboard("hello")()
	got, ok := message.(ClipboardCopyMsg)
	if !ok {
		t.Fatalf("CopyToClipboard returned %T, want ClipboardCopyMsg", message)
	}
	if written != "hello" || got.Text != "hello" || got.Err != nil || got.OSC52 {
		t.Fatalf("native clipboard result = %+v, written %q", got, written)
	}

	wantErr := errors.New("clipboard unavailable")
	writeNativeClipboardText = func(string) error { return wantErr }
	got = CopyToClipboard("fallback")().(ClipboardCopyMsg)
	if !errors.Is(got.Err, wantErr) || got.OSC52 {
		t.Fatalf("failed native clipboard result = %+v", got)
	}

	t.Setenv("SSH_CONNECTION", "host 22 client 1234")
	writeNativeClipboardText = func(string) error {
		t.Fatal("SSH copy must not write the remote host's native clipboard")
		return nil
	}
	got = CopyToClipboard("remote")().(ClipboardCopyMsg)
	if !got.OSC52 || got.Text != "remote" {
		t.Fatalf("SSH clipboard result = %+v, want OSC 52", got)
	}
}
