package agent

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
)

func (a *Agent) boundProviderVisibleResult(raw, toolName, callID string) (body, notice, original string) {
	summarized := summarizeCIOutput(raw)
	body, notice = truncateToolOutputFor(summarized, toolName, callID)
	deduped := a.dedupeProviderVisibleResultForTool(callID, raw, body, toolName)
	if deduped != body {
		original = raw
	}
	body = deduped
	if summarized != raw || notice != "" {
		original = raw
	}
	return body, notice, original
}

func (a *Agent) dedupeProviderVisibleResult(callID, raw, visible string) string {
	return a.dedupeProviderVisibleResultForTool(callID, raw, visible, "")
}

// dedupeProviderVisibleResultForTool applies the generic result guard while
// preserving state-machine acknowledgements. todo_write's output contains
// only counts, so two different valid lists can otherwise look identical.
func (a *Agent) dedupeProviderVisibleResultForTool(callID, raw, visible, toolName string) string {
	if a == nil || strings.TrimSpace(raw) == "" {
		return visible
	}
	// todo_write is a host state transition. Its short count-based result is
	// not a stable identity for the submitted list, so never hide it as a
	// duplicate. This also keeps a replay visible after compaction.
	if toolName == "todo_write" {
		return visible
	}
	sum := sha256.Sum256([]byte(raw))
	fp := hex.EncodeToString(sum[:12])
	prev, seen := a.turn.loop.rememberFingerprint(fp, callID)
	if seen && prev != callID {
		message := fmt.Sprintf("duplicate tool result omitted (identical to call_id=%s, fingerprint=%s). Full original remains locally; page it with session:tool_result if needed.", prev, fp)
		return message + canonicalTodoStateHint(a)
	}
	return visible
}

// canonicalTodoStateHint gives the model a recovery snapshot when a generic
// duplicate result is suppressed. The snapshot is deliberately taken from
// host state, not from the compacted provider transcript.
func canonicalTodoStateHint(a *Agent) string {
	if a == nil {
		return ""
	}
	todos := a.CanonicalTodoState()
	if len(todos) == 0 {
		return ""
	}
	b, err := json.Marshal(todos)
	if err != nil {
		return ""
	}
	return "\nCurrent canonical todo state: " + string(b)
}
