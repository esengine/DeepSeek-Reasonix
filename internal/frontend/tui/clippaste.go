package tui

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"runtime"

	tea "charm.land/bubbletea/v2"
	"github.com/atotto/clipboard"

	"reasonix/internal/base/i18n"
	"reasonix/internal/frontend/termrender"
	"reasonix/internal/platform/clipimage"
)

type (
	clipImageMsg struct {
		ref string
		err error
	}
	clipTextMsg struct {
		text string
		err  error
	}
)

// Attach stores image bytes where the kernel keeps attachments and returns
// the @-reference a message carries them by.
func (c *Client) Attach(ctx context.Context, raw []byte) (string, error) {
	var out struct {
		Ref string `json:"ref"`
	}
	body := map[string]string{"mime": http.DetectContentType(raw), "data": base64.StdEncoding.EncodeToString(raw)}
	err := c.do(ctx, http.MethodPost, "/attachments", body, &out)
	return out.Ref, err
}

// imagePasteKey is the key that pastes the clipboard's image. Windows
// terminals take Ctrl+V for their own text paste.
func imagePasteKey(k string) bool {
	if runtime.GOOS == "windows" {
		return k == "alt+v"
	}
	return k == "ctrl+v"
}

// pasteClipboard reads the clipboard of the machine the terminal runs on: an
// image goes up as an attachment, and without one the text comes in the way
// a terminal paste would. Over SSH that clipboard is the wrong machine's, so
// the terminal's own paste is the only one.
func (m *model) pasteClipboard() tea.Cmd {
	if termrender.RemoteClipboardSession() {
		m.tr.AddNotice("warn", i18n.M.ClipboardTextPasteRemoteHint)
		return m.commit()
	}
	return tea.Batch(m.showFlash(i18n.M.ClipboardImagePastingHint), func() tea.Msg {
		raw, err := clipimage.Read(m.ctx)
		if errors.Is(err, clipimage.ErrNoImage) {
			text, err := clipboard.ReadAll()
			return clipTextMsg{text: text, err: err}
		}
		if err != nil {
			return clipImageMsg{err: err}
		}
		ref, err := m.client.Attach(m.ctx, raw)
		return clipImageMsg{ref: ref, err: err}
	})
}

// pasteClipboardText is the terminal's own text paste, for the terminals
// that hand Shift+Insert to the app instead of pasting it themselves.
func (m *model) pasteClipboardText() tea.Cmd {
	if termrender.RemoteClipboardSession() {
		m.tr.AddNotice("warn", i18n.M.ClipboardTextPasteRemoteHint)
		return m.commit()
	}
	return func() tea.Msg {
		text, err := clipboard.ReadAll()
		return clipTextMsg{text: text, err: err}
	}
}

func (m *model) onClipImage(msg clipImageMsg) tea.Cmd {
	m.clearFlash()
	if msg.err != nil {
		m.tr.AddNotice("error", fmt.Sprintf(i18n.M.ClipboardImagePasteFailedFmt, msg.err))
		return m.commit()
	}
	m.composer.InsertString(m.pastes.image(msg.ref) + " ")
	return nil
}

func (m *model) onClipText(msg clipTextMsg) tea.Cmd {
	m.clearFlash()
	if msg.err != nil {
		m.tr.AddNotice("error", fmt.Sprintf(i18n.M.ClipboardTextPasteFailedFmt, msg.err))
		return m.commit()
	}
	if msg.text != "" {
		m.composer.InsertString(m.pastes.fold(msg.text))
	}
	return nil
}
