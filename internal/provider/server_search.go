package provider

import (
	"encoding/json"
	"strings"
)

// ServerSearchHit is one provider-executed web search result shown on a card.
// Encrypted page bodies stay on ServerSearchCall.Raw for API replay only.
type ServerSearchHit struct {
	Title string `json:"title,omitempty"`
	URL   string `json:"url,omitempty"`
}

// ServerSearchCall is one provider-executed web_search turn. UI cards read
// Query/Results; Anthropic replay uses ID + Raw. omitempty keeps old sessions
// byte-compatible.
type ServerSearchCall struct {
	SourcesStatus string            `json:"sources_status,omitempty"`
	ID            string            `json:"id"`
	Query         string            `json:"query,omitempty"`
	Results       []ServerSearchHit `json:"results,omitempty"`
	Raw           json.RawMessage   `json:"raw,omitempty"`
}

type wireSearchHit struct {
	Title string `json:"title"`
	URL   string `json:"url"`
}

// ParseServerSearchHits extracts title/url pairs and ignores encrypted_content.
func ParseServerSearchHits(raw json.RawMessage) []ServerSearchHit {
	if len(raw) == 0 {
		return nil
	}
	var parsed []wireSearchHit
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil
	}
	var out []ServerSearchHit
	for _, hit := range parsed {
		if hit.Title == "" && hit.URL == "" {
			continue
		}
		out = append(out, ServerSearchHit(hit))
	}
	return out
}

// ParseServerSearchQuery reads {"query":"..."} from a server_tool_use input.
func ParseServerSearchQuery(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	var parsed struct {
		Query string `json:"query"`
	}
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		return ""
	}
	return strings.TrimSpace(parsed.Query)
}

// FormatServerSearchArgs is the tool-card argument JSON {"query":"..."}.
func FormatServerSearchArgs(query string) string {
	query = strings.TrimSpace(query)
	if query == "" {
		return ""
	}
	raw, err := json.Marshal(map[string]string{"query": query})
	if err != nil {
		return ""
	}
	return string(raw)
}

// FormatServerSearchFootnotes is the post-answer source list the desktop and
// CLI render as markdown. It is display-only and must not be written into
// Message.Content.
func FormatServerSearchFootnotes(hits []ServerSearchHit) string {
	if len(hits) == 0 {
		return ""
	}
	var b strings.Builder
	for _, hit := range hits {
		if hit.Title == "" && hit.URL == "" {
			continue
		}
		b.WriteString("\n- **")
		b.WriteString(hit.Title)
		b.WriteString("**")
		if hit.URL != "" {
			b.WriteString("\n  <")
			b.WriteString(hit.URL)
			b.WriteString(">")
		}
	}
	if b.Len() == 0 {
		return ""
	}
	return "\n" + b.String() + "\n"
}

// ParseServerSearchOutput reads title/URL pairs from FormatServerSearchOutput.
func ParseServerSearchOutput(output string) []ServerSearchHit {
	// Independent searches carry a summary alongside sources. Keep accepting
	// the original title/URL output so old sessions render unchanged.
	var result struct {
		Sources []ServerSearchHit `json:"sources"`
	}
	if strings.HasPrefix(strings.TrimSpace(output), "{") && json.Unmarshal([]byte(output), &result) == nil && result.Sources != nil {
		return result.Sources
	}
	var out []ServerSearchHit
	for line := range strings.SplitSeq(output, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "http://") || strings.HasPrefix(line, "https://") {
			if n := len(out); n > 0 && out[n-1].URL == "" {
				out[n-1].URL = line
			} else {
				out = append(out, ServerSearchHit{URL: line})
			}
			continue
		}
		out = append(out, ServerSearchHit{Title: line})
	}
	return out
}

// FormatServerSearchOutput is the tool-card body: title then URL, one result
// per pair. It is not assistant answer text.
func FormatServerSearchOutput(hits []ServerSearchHit) string {
	if len(hits) == 0 {
		return ""
	}
	var b strings.Builder
	for i, hit := range hits {
		if i > 0 {
			b.WriteByte('\n')
		}
		if hit.Title != "" {
			b.WriteString(hit.Title)
			if hit.URL != "" {
				b.WriteByte('\n')
			}
		}
		if hit.URL != "" {
			b.WriteString(hit.URL)
		}
	}
	return b.String()
}

// ServerSearchFromResponsesItem maps a completed web_search_call replay item
// onto the shared card/replay record. Unknown shapes return nil.
func ServerSearchFromResponsesItem(raw json.RawMessage) *ServerSearchCall {
	var item struct {
		ID     string `json:"id"`
		Type   string `json:"type"`
		Status string `json:"status"`
		Action struct {
			Type    string   `json:"type"`
			Query   string   `json:"query"`
			Queries []string `json:"queries"`
			URL     string   `json:"url"`
			Sources []struct {
				URL   string `json:"url"`
				Title string `json:"title"`
			} `json:"sources"`
		} `json:"action"`
	}
	if err := json.Unmarshal(raw, &item); err != nil || item.Type != "web_search_call" || strings.TrimSpace(item.ID) == "" {
		return nil
	}
	call := &ServerSearchCall{ID: item.ID, Query: strings.TrimSpace(item.Action.Query), Raw: append(json.RawMessage(nil), raw...)}
	if call.Query == "" {
		call.Query = strings.Join(item.Action.Queries, "\n")
	}
	// DeepSeek returns visited URLs on open_page/find_in_page actions even
	// when search actions omit sources and message annotations are empty.
	// Use that structured evidence; never infer sources from generated prose.
	if item.Status == "completed" && (item.Action.Type == "open_page" || item.Action.Type == "find_in_page") && item.Action.URL != "" {
		call.Results = append(call.Results, ServerSearchHit{URL: item.Action.URL})
	}
	for _, src := range item.Action.Sources {
		if src.Title == "" && src.URL == "" {
			continue
		}
		call.Results = append(call.Results, ServerSearchHit{Title: src.Title, URL: src.URL})
	}
	return call
}

// WalkServerSearchEstimate visits the compact and overflow-estimate surface
// for one provider search: id, query, and visible hit titles/URLs. Encrypted
// Raw is omitted because Anthropic does not count it toward input tokens
// when the block is replayed. Replay still sends Raw verbatim.
func WalkServerSearchEstimate(search ServerSearchCall, visit func(string)) {
	if visit == nil {
		return
	}
	visit(search.ID)
	visit(search.Query)
	for _, hit := range search.Results {
		visit(hit.Title)
		visit(hit.URL)
	}
}

// MergeServerSearch upserts call into dst by ID, filling query/results/raw.
func MergeServerSearch(dst []ServerSearchCall, call ServerSearchCall) []ServerSearchCall {
	call.SourcesStatus = ServerSearchSourcesStatus(call)
	if strings.TrimSpace(call.ID) == "" {
		return dst
	}
	for i := range dst {
		if dst[i].ID != call.ID {
			continue
		}
		if call.Query != "" {
			dst[i].Query = call.Query
		}
		if call.SourcesStatus != "" {
			dst[i].SourcesStatus = call.SourcesStatus
		}
		if len(call.Results) > 0 {
			dst[i].Results = append([]ServerSearchHit(nil), call.Results...)
		}
		if len(call.Raw) > 0 {
			dst[i].Raw = append(json.RawMessage(nil), call.Raw...)
		}
		return dst
	}
	copied := call
	if len(call.Results) > 0 {
		copied.Results = append([]ServerSearchHit(nil), call.Results...)
	}
	if len(call.Raw) > 0 {
		copied.Raw = append(json.RawMessage(nil), call.Raw...)
	}
	return append(dst, copied)
}

// SearchPolicy separates native search injection from an independent client tool.
type SearchPolicy struct {
	NativeEnabled bool
	ClientEnabled bool
}
