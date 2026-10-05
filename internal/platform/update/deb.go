package update

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
)

// Linux privileged-install error classes. A caller maps these into its own
// anonymous metrics buckets; authorization_cancelled is a user decision rather
// than a failure, so it is named separately from the ones that are.
var (
	ErrDebAuthCancelled = errors.New("update: authorization cancelled")
	ErrDebAuthFailed    = errors.New("update: authorization failed")
	ErrDebPkgBusy       = errors.New("update: package manager busy")
	ErrDebPkgInstall    = errors.New("update: package install failed")
	ErrDebPkgVerify     = errors.New("update: package verify failed")
)

// debHelperPhasePrefix must match cmd/update-helper's phasePrefix. The helper
// writes it to stderr so a caller can leave "authorizing" once Polkit has
// launched it and validation finished, before apt-get starts.
const debHelperPhasePrefix = "REASONIX_UPDATE_PHASE="

// DebAuthCancelled reports whether an install ended because the user dismissed
// the Polkit prompt, which is a decision and not an error to report as one.
func DebAuthCancelled(err error) bool { return errors.Is(err, ErrDebAuthCancelled) }

func parseHelperPhaseLine(line string) (phase string, ok bool) {
	line = strings.TrimSpace(line)
	if !strings.HasPrefix(line, debHelperPhasePrefix) {
		return "", false
	}
	phase = strings.TrimSpace(strings.TrimPrefix(line, debHelperPhasePrefix))
	return phase, phase != ""
}

func parseHelperFailure(stdout []byte) (msg, code string) {
	var result struct {
		OK    bool   `json:"ok"`
		Error string `json:"error"`
		Code  string `json:"code"`
	}
	if err := json.Unmarshal(bytes.TrimSpace(stdout), &result); err != nil {
		return "", ""
	}
	if result.OK {
		return "", ""
	}
	return strings.TrimSpace(result.Error), strings.TrimSpace(result.Code)
}

// helperErrorMessage reduces the helper's output to one UI-safe line. Absolute
// paths are replaced: an install error is shown to a user and must not disclose
// where anything lives on disk.
func helperErrorMessage(stdout, stderr []byte) string {
	if msg, _ := parseHelperFailure(stdout); msg != "" {
		return msg
	}
	var kept []string
	for line := range strings.SplitSeq(string(stderr), "\n") {
		if _, ok := parseHelperPhaseLine(line); ok {
			continue
		}
		if line = strings.TrimSpace(line); line != "" {
			kept = append(kept, line)
		}
	}
	s := strings.TrimSpace(strings.Join(kept, " "))
	if s == "" {
		s = strings.TrimSpace(string(stdout))
	}
	if s == "" {
		return "install failed"
	}
	fields := strings.Fields(s)
	for i, f := range fields {
		if strings.HasPrefix(f, "/") {
			fields[i] = "<path>"
		}
	}
	out := strings.Join(fields, " ")
	if len(out) > 240 {
		out = out[:240]
	}
	return out
}
