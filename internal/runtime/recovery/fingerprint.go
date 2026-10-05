package recovery

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
)

// CallFingerprint builds a one-shot authorization fingerprint from the
// tool name, resolved subject/target, arguments, and preview summary.
// A continue decision only authorizes a later call whose fingerprint matches.
func CallFingerprint(tool, subject, preview string, args json.RawMessage) string {
	tool = strings.TrimSpace(tool)
	subject = strings.TrimSpace(subject)
	preview = strings.TrimSpace(preview)
	normArgs := normalizeArgs(args)
	h := sha256.New()
	_, _ = fmt.Fprintf(h, "tool=%s\n", tool)
	_, _ = fmt.Fprintf(h, "subject=%s\n", subject)
	_, _ = fmt.Fprintf(h, "preview=%s\n", preview)
	_, _ = fmt.Fprintf(h, "args=%s\n", normArgs)
	return hex.EncodeToString(h.Sum(nil))
}

func normalizeArgs(args json.RawMessage) string {
	if len(args) == 0 {
		return ""
	}
	var v any
	if err := json.Unmarshal(args, &v); err != nil {
		return string(args)
	}
	b, err := json.Marshal(v)
	if err != nil {
		return string(args)
	}
	return string(b)
}

// ArgsSummary produces a short display string for diagnostics.
func ArgsSummary(args json.RawMessage, max int) string {
	if max <= 0 {
		max = 200
	}
	s := strings.TrimSpace(string(args))
	if s == "" {
		return ""
	}
	var compact map[string]any
	if err := json.Unmarshal(args, &compact); err == nil {
		if cmd, ok := compact["command"].(string); ok && strings.TrimSpace(cmd) != "" {
			s = strings.TrimSpace(cmd)
		} else if path, ok := compact["path"].(string); ok && strings.TrimSpace(path) != "" {
			s = strings.TrimSpace(path)
		} else if p, ok := compact["file_path"].(string); ok && strings.TrimSpace(p) != "" {
			s = strings.TrimSpace(p)
		}
	}
	s = strings.Join(strings.Fields(s), " ")
	if len(s) > max {
		return s[:max] + "…"
	}
	return s
}
