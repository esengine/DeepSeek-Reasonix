package agent

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
)

func (a *Agent) boundProviderVisibleResult(raw, toolName, callID string) (body, notice, original string) {
	summarized := summarizeCIOutput(raw)
	body, notice = truncateToolOutputFor(summarized, toolName, callID)
	deduped := a.dedupeProviderVisibleResult(callID, raw, body)
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
	if a == nil || strings.TrimSpace(raw) == "" {
		return visible
	}
	sum := sha256.Sum256([]byte(raw))
	fp := hex.EncodeToString(sum[:12])
	prev, seen := a.turn.loop.rememberFingerprint(fp, callID)
	if seen && prev != callID {
		return fmt.Sprintf("duplicate tool result omitted (identical to call_id=%s, fingerprint=%s). Full original remains locally; page it with session:tool_result if needed.", prev, fp)
	}
	return visible
}
