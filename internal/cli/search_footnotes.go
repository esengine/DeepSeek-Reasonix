package cli

import (
	"encoding/json"
	"reasonix/internal/event"
	"reasonix/internal/i18n"
	"reasonix/internal/provider"
	"strings"
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

// Only an explicitly recorded status supplies the missing-source notice. Old
// histories and links in generated summary text cannot establish provenance.
func searchHistorySections(m provider.Message, width int, renderAssistant func(string, int) string) []string {
	if m.Role == provider.RoleAssistant {
		for _, call := range m.ServerSearch {
			if call.SourcesStatus == provider.SourcesNotProvided {
				return []string{"  · " + i18n.M.SearchSourcesNotProvided + "\n\n"}
			}
		}
	}
	if m.Role == provider.RoleTool && m.Name == "web_search" {
		var result struct {
			SourcesStatus string `json:"sources_status"`
			Summary       string `json:"summary"`
		}
		if json.Unmarshal([]byte(m.Content), &result) == nil && result.SourcesStatus == provider.SourcesNotProvided {
			out := []string{"  · " + i18n.M.SearchSourcesNotProvided + "\n\n"}
			if strings.TrimSpace(result.Summary) != "" {
				out = append(out, renderAssistant(result.Summary, width)+"\n\n")
			}
			return out
		}
	}
	return nil
}
