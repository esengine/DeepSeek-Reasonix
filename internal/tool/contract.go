package tool

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"reasonix/internal/provider"
)

// ContractEntry is the provider-visible contract for a tool schema snapshot.
type ContractEntry struct {
	Name        string
	Description string
	ReadOnly    bool
	Schema      json.RawMessage
}

// BuiltinContractEntries returns a stable snapshot of compile-time built-ins.
func BuiltinContractEntries() []ContractEntry {
	return contractEntriesFromTools(Builtins(), nil)
}

func contractEntriesFromTools(tools []Tool, canonical map[string]json.RawMessage) []ContractEntry {
	entries := make([]ContractEntry, 0, len(tools))
	for _, t := range tools {
		schema := provider.CanonicalizeSchema(t.Schema())
		if canonical != nil {
			if c := canonical[t.Name()]; len(c) > 0 {
				schema = append(json.RawMessage(nil), c...)
			}
		}
		entries = append(entries, ContractEntry{
			Name:        t.Name(),
			Description: strings.TrimSpace(t.Description()),
			ReadOnly:    t.ReadOnly(),
			Schema:      schema,
		})
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name < entries[j].Name })
	return entries
}

// ContractEntries returns the registry's provider-visible contract snapshot.
// The tool list is captured under the lock, but the per-tool method calls
// (Schema/Description/ReadOnly) run AFTER it is released: a lazy MCP
// placeholder's ReadOnly takes the spawn mutex, and the spawn's trySwap takes
// this registry's write lock — holding the read lock across ReadOnly is an
// AB-BA deadlock (boot's snapshot assembly hit it with a live swap in flight).
func (r *Registry) ContractEntries() []ContractEntry {
	return r.contractEntries(true)
}

// AllContractEntries returns every registered tool's contract, including tools
// hidden from the provider schema. Capability catalogs use this so
// use_capability can list and call tool:<name> targets.
func (r *Registry) AllContractEntries() []ContractEntry {
	return r.contractEntries(false)
}

func (r *Registry) contractEntries(providerVisibleOnly bool) []ContractEntry {
	r.mu.RLock()
	tools := make([]Tool, 0, len(r.order))
	canonical := make(map[string]json.RawMessage, len(r.order))
	for _, name := range r.order {
		if providerVisibleOnly && !r.isProviderVisibleLocked(name) {
			continue
		}
		t := r.tools[name]
		if t == nil {
			continue
		}
		tools = append(tools, t)
		canonical[name] = r.canon[name]
	}
	r.mu.RUnlock()
	return contractEntriesFromTools(tools, canonical)
}

// RenderContractMarkdown renders entries as committed documentation. Tests use
// the same entries, so docs drift when tool names, descriptions, read-only
// flags, or canonical schemas change.
func RenderContractMarkdown(entries []ContractEntry) string {
	var b strings.Builder
	b.WriteString("# Tool Contract\n\n")
	b.WriteString("This document records the provider-visible contract for Reasonix compile-time built-in tools. It is generated from the same canonical schema path used by the runtime registry.\n\n")
	b.WriteString("| Tool | Read-only | Description |\n")
	b.WriteString("| --- | --- | --- |\n")
	for _, e := range entries {
		fmt.Fprintf(&b, "| `%s` | %t | %s |\n", e.Name, e.ReadOnly, markdownCell(e.Description))
	}
	b.WriteString("\n## Schemas\n")
	for _, e := range entries {
		fmt.Fprintf(&b, "\n### `%s`\n\n", e.Name)
		fmt.Fprintf(&b, "- Read-only: `%t`\n", e.ReadOnly)
		if e.Description != "" {
			fmt.Fprintf(&b, "- Description: %s\n", e.Description)
		}
		b.WriteString("\n```json\n")
		b.WriteString(prettyJSON(e.Schema))
		b.WriteString("\n```\n")
	}
	return b.String()
}

func markdownCell(s string) string {
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "|", `\|`)
	return strings.Join(strings.Fields(s), " ")
}

func prettyJSON(raw json.RawMessage) string {
	var out bytes.Buffer
	if err := json.Indent(&out, raw, "", "  "); err != nil {
		return strings.TrimSpace(string(raw))
	}
	return out.String()
}
