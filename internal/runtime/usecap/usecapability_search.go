package usecap

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"reasonix/internal/base/retrieval"
	"reasonix/internal/runtime/capability"
)

type capabilitySearchHit struct {
	ID          string            `json:"id"`
	Aliases     []string          `json:"aliases,omitempty"`
	Kind        capability.Kind   `json:"kind"`
	Name        string            `json:"name"`
	Description string            `json:"description,omitempty"`
	Status      capability.Status `json:"status"`
	ReadOnly    bool              `json:"read_only,omitempty"`
	InputSchema json.RawMessage   `json:"input_schema,omitempty"`
}

func (t *UseCapabilityTool) searchCapabilities(query string, limit int) (string, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return "", fmt.Errorf("query is required for action=search")
	}
	if limit <= 0 {
		limit = 5
	}
	if limit > 20 {
		limit = 20
	}

	catalog := t.currentCatalog()
	docs := make([]retrieval.FieldedDoc, 0, len(catalog.Entries))
	entries := make(map[string]capability.Entry, len(catalog.Entries))
	for _, entry := range catalog.Entries {
		if entry.Status == capability.StatusDisabled || entry.Status == capability.StatusFailed {
			continue
		}
		fields := map[string]string{
			"name":        entry.ID + " " + entry.Name + " " + entry.ToolName,
			"description": entry.Description,
			"keywords":    strings.Join(append(slices.Clone(entry.Aliases), entry.Triggers...), " "),
		}
		if entry.Kind == capability.KindTool && t.registry != nil {
			if target, ok := t.registry.Get(entry.ToolName); ok {
				fields["body"] = string(target.Schema())
			}
		}
		docs = append(docs, retrieval.FieldedDoc{ID: entry.ID, Fields: fields})
		entries[entry.ID] = entry
	}

	ranked := retrieval.RankV2(query, docs)
	if len(ranked) > limit {
		ranked = ranked[:limit]
	}
	hits := make([]capabilitySearchHit, 0, len(ranked))
	for _, rankedHit := range ranked {
		entry, ok := entries[rankedHit.ID]
		if !ok {
			continue
		}
		hit := capabilitySearchHit{
			ID:          entry.ID,
			Aliases:     entry.Aliases,
			Kind:        entry.Kind,
			Name:        entry.Name,
			Description: capabilityLead(entry.Description),
			Status:      entry.Status,
			ReadOnly:    entry.ReadOnly,
		}
		if entry.Kind == capability.KindTool && t.registry != nil {
			if target, ok := t.registry.Get(entry.ToolName); ok {
				hit.InputSchema = target.Schema()
			}
		}
		hits = append(hits, hit)
	}

	payload := map[string]any{
		"query":   query,
		"matches": hits,
		"note":    "Call action=call with a returned capability id. An empty matches array means this catalog search found no lexical match; it does not prove that every possible external capability is unavailable.",
	}
	b, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func FilterCapabilitySearchResult(raw string, allowed map[string]bool) string {
	var payload struct {
		Query   string                `json:"query"`
		Matches []capabilitySearchHit `json:"matches"`
		Note    string                `json:"note"`
	}
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return `{"query":"","matches":[],"note":"search is filtered to this subagent's allowed capabilities."}`
	}
	filtered := payload.Matches[:0]
	for _, hit := range payload.Matches {
		if allowed[hit.ID] || anyAllowedAlias(hit.Aliases, allowed) {
			filtered = append(filtered, hit)
		}
	}
	payload.Matches = filtered
	payload.Note = "search is filtered to this subagent's allowed capabilities."
	b, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return `{"query":"","matches":[],"note":"search is filtered to this subagent's allowed capabilities."}`
	}
	return string(b)
}

func anyAllowedAlias(aliases []string, allowed map[string]bool) bool {
	for _, alias := range aliases {
		if allowed[alias] {
			return true
		}
	}
	return false
}
