package agent

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"reasonix/internal/evidence"
	"reasonix/internal/provider"
	"reasonix/internal/tool"
)

// citedSourceToken reads the optional host source handle a writer passed. It is
// a handle the host issued on a read result, never a value the model may
// compose: an unrecognized one proves nothing and is rejected.
func citedSourceToken(arguments string) string {
	var p struct {
		SourceToken string `json:"source_token"`
	}
	if json.Unmarshal([]byte(arguments), &p) != nil {
		return ""
	}
	return strings.TrimSpace(p.SourceToken)
}

// checkCitedSourceToken resolves the cited read and decides whether it proves
// the exact content this write replaces. A token that names another file, an
// older version, or an incomplete window is rejected once with the concrete
// recovery — re-read the target — instead of an open invitation to retry.
func (a *Agent) checkCitedSourceToken(call provider.ToolCall, check evidenceCheck, info tool.EvidenceTargetInfo, token string) evidenceCheck {
	canonical := filepath.Clean(info.Path)
	observations := slices.DeleteFunc(a.task.ledger.SourceToken(token), func(o evidence.TextObservation) bool {
		return filepath.Clean(o.Path) != canonical
	})
	if len(observations) == 0 {
		check.Reason = "source_token_unknown"
		check.Recovery = fmt.Sprintf("source_token %q does not name a read of %s in this turn; read the file and cite the source_token it prints", token, info.Path)
		check.Missing = info.Ranges
		return check
	}
	if info.Snapshot != "" {
		for _, o := range observations {
			if o.Snapshot != "" && o.Snapshot != info.Snapshot {
				check.Reason = "stale_or_partial_evidence"
				check.Recovery = fmt.Sprintf("%s changed since source_token %q was issued; re-read it and cite the new source_token", info.Path, token)
				check.Missing = info.Ranges
				return check
			}
		}
	}
	if satisfied, missing := evidenceCoversTarget(observations, info); satisfied {
		check.Satisfied = true
		a.operations().NoteSourceToken(evidence.OperationID(call.Name, json.RawMessage(call.Arguments)), token)
		return check
	} else {
		check.Missing = missing
	}
	check.Reason = "stale_or_partial_evidence"
	check.Recovery = fmt.Sprintf("source_token %q covers only part of what this write replaces; re-read the missing lines of %s and cite the new source_token", token, info.Path)
	return check
}
