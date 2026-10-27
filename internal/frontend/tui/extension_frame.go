package tui

import (
	"encoding/json"
	"strings"

	"reasonix/internal/base/textutil"
	"reasonix/internal/contract/eventwire"
)

func (t *Transcript) foldExtensionFrame(ext *eventwire.ExtensionSurface) {
	if ext == nil || ext.Frame == nil || ext.Code != "" {
		return
	}
	identity, _ := json.Marshal([]any{ext.SessionID, ext.Generation, ext.PluginID, ext.SurfaceID})
	code := "extension.frame:" + string(identity)
	text := textutil.SanitizeDisplay("[" + ext.PluginID + "] " + ext.Frame.Title + "\n" + ext.Frame.Fallback)
	text = strings.TrimSpace(text)
	for i := range t.Items {
		if t.Items[i].Kind == ItemNotice && t.Items[i].Code == code {
			t.Items[i].Text = text
			return
		}
	}
	t.Items = append(t.Items, Item{ID: t.id(), Kind: ItemNotice, Level: "info", Code: code, Text: text})
}
