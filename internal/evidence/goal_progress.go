package evidence

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"slices"
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

// SuccessfulProgressFingerprint returns a stable set fingerprint for successful
// host-observed work in the current ledger. Repeating an identical call with an
// identical result does not change it, while a novel read result, command
// result, mutation, todo update, review, or sign-off does.
func (l *Ledger) SuccessfulProgressFingerprint() string {
	signatures := l.SuccessfulProgressSignaturesSince(0)
	unique := make(map[string]struct{}, len(signatures))
	for _, signature := range signatures {
		unique[signature] = struct{}{}
	}
	signatures = signatures[:0]
	for signature := range unique {
		signatures = append(signatures, signature)
	}
	slices.Sort(signatures)
	sum := sha256.Sum256([]byte(strings.Join(signatures, "\x00")))
	return fmt.Sprintf("%x", sum)
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
	case r.ToolName == "todo_write":
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
