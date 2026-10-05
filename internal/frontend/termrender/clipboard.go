package termrender

import (
	"os"

	tea "charm.land/bubbletea/v2"
	"github.com/atotto/clipboard"
)

// ClipboardCopyMsg reports a CopyToClipboard attempt. OSC52 means nothing was
// written locally and the caller must emit an OSC 52 request; a non-nil Err
// leaves the caller that same fallback, labelled as unverified.
type ClipboardCopyMsg struct {
	Text  string
	Err   error
	OSC52 bool
}

var writeNativeClipboardText = clipboard.WriteAll

// RemoteClipboardSession reports an SSH session, where the native clipboard
// is the remote host's rather than the user's.
func RemoteClipboardSession() bool { return remoteClipboardSession() }

func remoteClipboardSession() bool {
	return os.Getenv("SSH_CONNECTION") != "" || os.Getenv("SSH_CLIENT") != "" || os.Getenv("SSH_TTY") != ""
}

// CopyToClipboard prefers the operating system clipboard in a local session,
// where success can be verified (pbcopy on macOS, the selected Wayland/X11
// utility on Linux, and the Win32 clipboard on Windows). SSH cannot reliably
// reach the user's local desktop clipboard, so it falls back to OSC 52.
func CopyToClipboard(text string) tea.Cmd {
	return func() tea.Msg {
		if remoteClipboardSession() {
			return ClipboardCopyMsg{Text: text, OSC52: true}
		}
		return ClipboardCopyMsg{Text: text, Err: writeNativeClipboardText(text)}
	}
}
