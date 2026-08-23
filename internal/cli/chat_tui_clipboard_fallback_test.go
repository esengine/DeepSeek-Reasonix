package cli

import (
	"errors"
	"runtime"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"reasonix/internal/control"
)

func stubEmptyImageClipboard(t *testing.T, text string) {
	t.Helper()
	prevImage := readClipboardImage
	prevText := readNativeClipboardText
	t.Cleanup(func() {
		readClipboardImage = prevImage
		readNativeClipboardText = prevText
	})
	readClipboardImage = func() (string, error) { return "", control.ErrNoClipboardImage }
	readNativeClipboardText = func() (string, error) { return text, nil }
}

func imagePasteKey() tea.KeyPressMsg {
	if runtime.GOOS == "windows" {
		return tea.KeyPressMsg{Code: 'v', Mod: tea.ModAlt}
	}
	return tea.KeyPressMsg{Code: 'v', Mod: tea.ModCtrl}
}

func TestCtrlVPastesTextWhenClipboardHasNoImage(t *testing.T) {
	setLocalClipboardSession(t)
	stubEmptyImageClipboard(t, "https://example.com/x")

	m := newComposerMouseTestTUI(t, 60, 16)
	m.input.SetValue("before ")

	next, cmd := m.Update(imagePasteKey())
	m = next.(chatTUI)
	if cmd == nil {
		t.Fatal("image paste shortcut produced no clipboard command")
	}
	next, cmd = m.Update(cmd())
	m = next.(chatTUI)

	if got := strings.Join(m.transcript, "\n"); strings.Contains(got, "wl-paste") {
		t.Fatalf("empty image clipboard surfaced a tooling notice:\n%s", got)
	}
	result := clipboardTextPasteResultFromCmd(t, cmd)
	next, _ = m.Update(result)
	m = next.(chatTUI)

	if got, want := m.input.Value(), "before https://example.com/x"; got != want {
		t.Fatalf("clipboard text fallback produced %q, want %q", got, want)
	}
}

func TestCtrlVDoesNotPasteTwiceWhenTerminalAlreadyPasted(t *testing.T) {
	setLocalClipboardSession(t)
	stubEmptyImageClipboard(t, "term text")

	m := newComposerMouseTestTUI(t, 60, 16)
	m.input.SetValue("before ")

	next, cmd := m.Update(imagePasteKey())
	m = next.(chatTUI)
	if cmd == nil {
		t.Fatal("image paste shortcut produced no clipboard command")
	}
	next, _ = m.Update(tea.PasteMsg{Content: "term text"})
	m = next.(chatTUI)
	if got, want := m.input.Value(), "before term text"; got != want {
		t.Fatalf("bracketed paste produced %q, want %q", got, want)
	}

	next, cmd = m.Update(cmd())
	m = next.(chatTUI)
	if cmd != nil {
		t.Fatal("fallback ran even though the terminal already pasted")
	}
	if got, want := m.input.Value(), "before term text"; got != want {
		t.Fatalf("text was pasted twice: %q, want %q", got, want)
	}
}

func TestRapidCtrlVDoesNotDropSecondTextFallback(t *testing.T) {
	setLocalClipboardSession(t)
	stubEmptyImageClipboard(t, "text")

	m := newComposerMouseTestTUI(t, 60, 16)
	next, firstImage := m.Update(imagePasteKey())
	m = next.(chatTUI)
	next, firstText := m.Update(firstImage())
	m = next.(chatTUI)

	next, secondImage := m.Update(imagePasteKey())
	m = next.(chatTUI)
	if secondImage == nil {
		t.Fatal("second image paste shortcut did not start a probe")
	}

	firstResult := clipboardTextPasteResultFromCmd(t, firstText)
	next, _ = m.Update(firstResult)
	m = next.(chatTUI)

	next, secondText := m.Update(secondImage())
	m = next.(chatTUI)
	if secondText == nil {
		t.Fatal("second text fallback was mistaken for a terminal-owned paste")
	}
	secondResult := clipboardTextPasteResultFromCmd(t, secondText)
	next, _ = m.Update(secondResult)
	m = next.(chatTUI)

	if got, want := m.input.Value(), "texttext"; got != want {
		t.Fatalf("rapid clipboard fallbacks produced %q, want %q", got, want)
	}
}

func TestOverlappingCtrlVDoesNotDropSecondTextFallback(t *testing.T) {
	setLocalClipboardSession(t)
	stubEmptyImageClipboard(t, "text")

	m := newComposerMouseTestTUI(t, 60, 16)
	next, imageProbe := m.Update(imagePasteKey())
	m = next.(chatTUI)
	next, duplicateProbe := m.Update(imagePasteKey())
	m = next.(chatTUI)
	if duplicateProbe != nil {
		t.Fatal("overlapping clipboard requests should share one image probe")
	}

	next, textFallback := m.Update(imageProbe())
	m = next.(chatTUI)
	result := clipboardTextPasteResultFromCmd(t, textFallback)
	next, _ = m.Update(result)
	m = next.(chatTUI)

	if got, want := m.input.Value(), "texttext"; got != want {
		t.Fatalf("overlapping clipboard fallbacks produced %q, want %q", got, want)
	}
}

func TestOverlappingCtrlVPreservesRequestNotOwnedByTerminal(t *testing.T) {
	setLocalClipboardSession(t)
	stubEmptyImageClipboard(t, "text")

	m := newComposerMouseTestTUI(t, 60, 16)
	next, imageProbe := m.Update(imagePasteKey())
	m = next.(chatTUI)
	next, _ = m.Update(imagePasteKey())
	m = next.(chatTUI)

	// One bracketed paste satisfies one request while the shared image probe is
	// in flight. The other request must still use the native-text fallback.
	next, _ = m.Update(tea.PasteMsg{Content: "text"})
	m = next.(chatTUI)
	next, textFallback := m.Update(imageProbe())
	m = next.(chatTUI)
	result := clipboardTextPasteResultFromCmd(t, textFallback)
	next, _ = m.Update(result)
	m = next.(chatTUI)

	if got, want := m.input.Value(), "texttext"; got != want {
		t.Fatalf("mixed terminal and clipboard fallbacks produced %q, want %q", got, want)
	}
}

func TestOverlappingCtrlVStillAttachesImageOnce(t *testing.T) {
	setLocalClipboardSession(t)
	m := newComposerMouseTestTUI(t, 60, 16)

	next, imageProbe := m.Update(imagePasteKey())
	m = next.(chatTUI)
	if imageProbe == nil {
		t.Fatal("first image paste shortcut did not start a probe")
	}
	next, duplicateProbe := m.Update(imagePasteKey())
	m = next.(chatTUI)
	if duplicateProbe != nil {
		t.Fatal("overlapping image paste started a duplicate probe")
	}

	next, _ = m.Update(clipboardImageMsg{path: ".reasonix/attachments/test.png"})
	m = next.(chatTUI)
	if got, want := m.input.Value(), "[image #1] "; got != want {
		t.Fatalf("overlapping image paste produced %q, want %q", got, want)
	}
	if m.clipboardImagePending || m.clipboardImageRequests != 0 {
		t.Fatalf("completed image paste kept pending state: pending=%v requests=%d", m.clipboardImagePending, m.clipboardImageRequests)
	}
}

func TestLateTerminalPasteCancelsScheduledTextFallback(t *testing.T) {
	setLocalClipboardSession(t)
	stubEmptyImageClipboard(t, "term text")

	m := newComposerMouseTestTUI(t, 60, 16)
	next, imageProbe := m.Update(imagePasteKey())
	m = next.(chatTUI)
	next, textFallback := m.Update(imageProbe())
	m = next.(chatTUI)
	if textFallback == nil {
		t.Fatal("empty image clipboard did not schedule a text fallback")
	}

	// The terminal paste arrives after the image result but before the native
	// text read completes. It still owns this paste and must cancel the fallback.
	next, _ = m.Update(tea.PasteMsg{Content: "term text"})
	m = next.(chatTUI)
	result := clipboardTextPasteResultFromCmd(t, textFallback)
	next, _ = m.Update(result)
	m = next.(chatTUI)

	if got, want := m.input.Value(), "term text"; got != want {
		t.Fatalf("late terminal paste was duplicated: %q, want %q", got, want)
	}
}

func TestClipboardImagePasteKeepsNoticeForRealFailures(t *testing.T) {
	setLocalClipboardSession(t)
	m := newComposerMouseTestTUI(t, 60, 16)
	m.input.SetValue("before ")

	next, cmd := m.Update(clipboardImageMsg{err: errors.New("clipboard image paste needs wl-paste (Wayland) or xclip (X11)")})
	m = next.(chatTUI)

	if cmd != nil {
		t.Fatal("a real clipboard failure must not trigger a text paste")
	}
	if got := strings.Join(m.transcript, "\n"); !strings.Contains(got, "wl-paste") {
		t.Fatalf("a real clipboard failure lost its notice:\n%s", got)
	}
	if got := m.input.Value(); got != "before " {
		t.Fatalf("a real clipboard failure changed the composer: %q", got)
	}
}
