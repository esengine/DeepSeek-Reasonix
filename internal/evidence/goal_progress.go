package evidence

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strings"
)

// SuccessfulProgressSignaturesSince returns stable successful-work identities.
func (l *Ledger) SuccessfulProgressSignaturesSince(index int) []string {
	if l == nil {
		return nil
	}
	if index < 0 {
		index = 0
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	var out []string
	for i := index; i < len(l.receipts); i++ {
		if sig, ok := progressReceiptSignature(l.receipts[i]); ok {
			out = append(out, sig)
		}
	}
	return out
}

func progressReceiptSignature(r Receipt) (string, bool) {
	if !r.Success {
		return "", false
	}
	kind := ""
	switch {
	case r.Mutation || r.Write:
		kind = "mutation"
	case r.Command != "":
		kind = "command"
	case r.ToolName == "todo_write" && len(r.Todos) > 0:
		kind = "todo"
	case r.ToolName == "complete_step" && r.StepProof:
		kind = "signoff"
	case successfulForegroundReviewReceipt(r) || completedStructuredReviewReceipt(r, nil):
		kind = "review"
	case r.Read && r.OutputBytes > 0:
		kind = "read"
	default:
		return "", false
	}
	payload := strings.TrimSpace(string(r.Args))
	var decoded any
	if json.Unmarshal(r.Args, &decoded) == nil {
		if canonical, err := json.Marshal(decoded); err == nil {
			payload = string(canonical)
		}
	}
	if (kind == "read" || kind == "command") && r.OutputDigest != "" {
		payload += "\x00output=" + r.OutputDigest
	}
	sum := sha256.Sum256([]byte(kind + "\x00" + r.ToolName + "\x00" + payload))
	return fmt.Sprintf("%x", sum), true
}
