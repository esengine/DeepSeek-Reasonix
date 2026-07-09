package main

import (
	"fmt"
	"hash/fnv"

	"reasonix/internal/config"
	"reasonix/internal/memory"
	"reasonix/internal/memorycompiler"
)

const (
	// compilerSuggestionLimit bounds Memory v5 candidates separately from the
	// history-derived ones so a busy history cannot crowd them out.
	compilerSuggestionLimit = 3
	// compilerSuggestionMinCount requires a pattern to recur across at least
	// this many separate turns before it is stable enough to suggest.
	compilerSuggestionMinCount = 2
)

// suggestCompilerMemories converts stable Memory v5 execution learnings into
// memory candidates for the settings Memory page. Read-only: candidates are
// only persisted through the regular AcceptMemorySuggestion confirmation flow.
func suggestCompilerMemories(workspaceRoot string, set *memory.Set, already []MemorySuggestion) []MemorySuggestion {
	rt := memorycompiler.New(config.MemoryCompilerDir(workspaceRoot))
	if rt == nil || set == nil {
		return nil
	}
	existing := existingMemoryText(set)
	seen := map[string]bool{}
	for _, s := range already {
		seen[normalizeSuggestionKey(s.Description)] = true
	}
	var out []MemorySuggestion
	for _, p := range rt.StableNoisePatterns(compilerSuggestionMinCount, compilerSuggestionLimit*2) {
		statement := compilerPatternStatement(p.Pattern)
		key := normalizeSuggestionKey(statement)
		if key == "" || seen[key] || existingCovers(existing, key) {
			continue
		}
		seen[key] = true
		name := compilerCandidateName(p.Pattern)
		out = append(out, MemorySuggestion{
			ID:          "memory-" + name,
			Name:        name,
			Title:       suggestionTitle(statement, "Memory v5 pattern"),
			Description: oneLine(statement),
			Type:        string(memory.TypeProject),
			Body:        compilerCandidateBody(statement, p.Count),
			Reason:      fmt.Sprintf("Memory v5 observed this failure pattern in %d separate turns", p.Count),
			Evidence:    []string{fmt.Sprintf("memory-v5 execution traces: %s (x%d)", truncateRunes(p.Pattern, 160), p.Count)},
		})
		if len(out) >= compilerSuggestionLimit {
			break
		}
	}
	return out
}

// compilerCandidateName always appends a hash: compiler patterns are
// inherently homogeneous (same tool + similar error prefix) so the ASCII
// slug alone cannot distinguish them even when it is non-empty.
func compilerCandidateName(pattern string) string {
	base := suggestionName("", "memory-v5 "+pattern, "memory-v5-pattern")
	h := fnv.New32a()
	_, _ = h.Write([]byte(pattern))
	return fmt.Sprintf("%s-%08x", base, h.Sum32())
}

func compilerPatternStatement(pattern string) string {
	pattern = oneLine(pattern)
	if pattern == "" {
		return ""
	}
	return "Known repeated failure in this workspace: " + pattern + "."
}

func compilerCandidateBody(statement string, count int) string {
	return statement + "\n\n" +
		fmt.Sprintf("**Why:** Memory v5 recorded this failure pattern in %d separate execution traces for this workspace.\n", count) +
		"**How to apply:** Address the known cause before retrying the same command; drop this memory once the failure stops reproducing.\n"
}
