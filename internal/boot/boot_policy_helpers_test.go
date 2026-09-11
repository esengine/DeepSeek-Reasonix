package boot

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"reasonix/internal/config"
	"reasonix/internal/provider"
)

func bootLastUser(req provider.Request) string {
	for _, v := range slices.Backward(req.Messages) {
		if v.Role == provider.RoleUser {
			return v.Content
		}
	}
	return ""
}

func subagentRefFromHistory(t *testing.T, msgs []provider.Message) string {
	t.Helper()
	for _, msg := range msgs {
		if msg.Role != provider.RoleTool {
			continue
		}
		for line := range strings.SplitSeq(msg.Content, "\n") {
			line = strings.TrimSpace(line)
			for _, prefix := range []string{"Subagent reference: ", "Subagent reference (failed): "} {
				if after, ok := strings.CutPrefix(line, prefix); ok {
					return strings.TrimSpace(after)
				}
			}
		}
	}
	if ref := firstPersistedSubagentRef(t, config.SessionDir()); ref != "" {
		return ref
	}
	t.Fatalf("no subagent reference in history: %+v", msgs)
	return ""
}

func firstPersistedSubagentRef(t *testing.T, sessionDir string) string {
	t.Helper()
	dir := filepath.Join(sessionDir, "subagents")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	for _, entry := range entries {
		name := entry.Name()
		if strings.HasPrefix(name, "sa_") && strings.HasSuffix(name, ".jsonl") && !strings.Contains(name, ".events.") {
			return strings.TrimSuffix(name, ".jsonl")
		}
	}
	return ""
}
