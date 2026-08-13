package cli

import (
	"reasonix/internal/event"
	"reasonix/internal/provider"
)

func (m *chatTUI) rememberSearchResult(t event.Tool) {
	if t.Name == "web_search" && t.Err == "" {
		m.searchSources = provider.ParseServerSearchOutput(t.Output)
	}
}

func (m *chatTUI) writeSearchFootnotes() {
	notes := provider.FormatServerSearchFootnotes(m.searchSources)
	if notes == "" {
		return
	}
	m.pending.WriteString(notes)
	m.searchSources = nil
}
