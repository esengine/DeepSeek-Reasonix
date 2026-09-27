package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode"

	"reasonix/internal/agent"
)

const resumePickerSentinel = "__reasonix_resume_picker__"

func splitAllowedToolRules(values []string) ([]string, error) {
	var rules []string
	for _, value := range values {
		start := -1
		depth := 0
		flush := func(end int) {
			if start < 0 {
				return
			}
			if rule := strings.TrimSpace(value[start:end]); rule != "" {
				rules = append(rules, rule)
			}
			start = -1
		}
		for i, r := range value {
			switch r {
			case '(':
				if start < 0 {
					start = i
				}
				depth++
			case ')':
				if depth == 0 {
					return nil, fmt.Errorf("invalid --allowed-tools value %q: unexpected ')'", value)
				}
				depth--
			default:
				if depth == 0 && (r == ',' || unicode.IsSpace(r)) {
					flush(i)
					continue
				}
				if start < 0 {
					start = i
				}
			}
		}
		if depth != 0 {
			return nil, fmt.Errorf("invalid --allowed-tools value %q: unclosed '('", value)
		}
		flush(len(value))
	}
	return uniqueStrings(rules), nil
}

func uniqueStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

// hasLeadingPrintFlag reports whether a standalone -p/--print token appears in
// the top-level flag run, i.e. before any "--" terminator. reasonix has no
// interactive -p, so its presence means the user wants one-shot print mode even
// when it trails other flags (`reasonix --model X -p "task"`).
func hasLeadingPrintFlag(args []string) bool {
	for _, arg := range args {
		if arg == "--" {
			return false
		}
		if arg == "-p" || arg == "--print" {
			return true
		}
	}
	return false
}

// stripLeadingPrintFlag drops the first standalone -p/--print token before any
// "--" terminator, leaving the rest (including everything after "--") untouched.
// Used when re-routing a top-level invocation to `run --print` so the print flag
// is not duplicated.
func stripLeadingPrintFlag(args []string) []string {
	out := make([]string, 0, len(args))
	dropped := false
	for i, arg := range args {
		if arg == "--" {
			out = append(out, args[i:]...)
			break
		}
		if !dropped && (arg == "-p" || arg == "--print") {
			dropped = true
			continue
		}
		out = append(out, arg)
	}
	return out
}

// normalizeOptionalResumeArg gives pflag the optional-value behavior Claude's
// --resume [value] exposes. Interactive sessions have no positional arguments,
// so a following non-flag token is unambiguously the resume query.
func normalizeOptionalResumeArg(args []string) []string {
	out := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if (arg == "--resume" || arg == "-r") && i+1 < len(args) && !strings.HasPrefix(args[i+1], "-") {
			out = append(out, arg+"="+args[i+1])
			i++
			continue
		}
		out = append(out, arg)
	}
	return out
}

func resolveSessionQuery(dir, query string) (cliResumeTarget, error) {
	query = strings.TrimSpace(query)
	if query == "" || query == resumePickerSentinel {
		return cliResumeTarget{}, nil
	}
	if info, err := os.Stat(query); err == nil && !info.IsDir() {
		abs, absErr := filepath.Abs(query)
		if absErr != nil {
			return cliResumeTarget{}, absErr
		}
		return cliResumeTarget{path: abs}, nil
	}
	sessions, err := agent.ListSessions(dir)
	if err != nil {
		return cliResumeTarget{}, fmt.Errorf("list sessions: %w", err)
	}
	// Opaque machine session IDs (session_<hex>) are what --events-jsonl and
	// `session show --json` expose. Match them before preview/partial search so
	// one-shot `run --resume` can resume without scanning private paths (#7429).
	if looksLikeMachineSessionID(query) {
		key, keyErr := loadMachineIdentityKey()
		if keyErr != nil {
			return cliResumeTarget{}, fmt.Errorf("machine identity is unavailable: %w", keyErr)
		}
		for _, session := range sessions {
			if machineSessionIDWithKey(agent.BranchID(session.Path), key) == query {
				return cliResumeTarget{path: session.Path}, nil
			}
		}
		return cliResumeTarget{}, fmt.Errorf("no session matches %q", query)
	}
	// Final-format sessions are the same universe the picker offers: engine
	// mirrors folded and migrated sources hidden, so a transcript and the
	// identity it was imported under never compete as two matches.
	scan := scanWorkspaceResume(context.Background(), dir)
	var exact []resumeEntry
	for _, session := range sessions {
		id := agent.BranchID(session.Path)
		base := filepath.Base(session.Path)
		if query == id || query == base || query == session.Path {
			exact = append(exact, resumeEntry{session: session, target: cliResumeTarget{path: session.Path}})
		}
	}
	for _, entry := range scan.canonical {
		if id := entry.target.ref.SessionID; query == id || query == cliCanonicalRoute(id) {
			exact = append(exact, entry)
		}
	}
	matches := exact
	if len(matches) == 0 {
		lower := strings.ToLower(query)
		for _, session := range scan.legacy {
			haystack := strings.ToLower(strings.Join([]string{
				agent.BranchID(session.Path), filepath.Base(session.Path), session.CustomTitle, session.TopicTitle, session.Preview,
			}, "\n"))
			if strings.Contains(haystack, lower) {
				matches = append(matches, resumeEntry{session: session, target: cliResumeTarget{path: session.Path}})
			}
		}
		for _, entry := range scan.canonical {
			haystack := strings.ToLower(strings.Join([]string{
				entry.target.ref.SessionID, entry.session.CustomTitle, entry.session.Preview,
			}, "\n"))
			if strings.Contains(haystack, lower) {
				matches = append(matches, entry)
			}
		}
	}
	switch len(matches) {
	case 0:
		return cliResumeTarget{}, fmt.Errorf("no session matches %q", query)
	case 1:
		return matches[0].target, nil
	default:
		return cliResumeTarget{}, &ambiguousSessionQueryError{query: query, matches: matches}
	}
}

// ambiguousSessionQueryError carries the conversations a --resume query
// matched, so the caller can list them or offer them in the picker.
type ambiguousSessionQueryError struct {
	query   string
	matches []resumeEntry
}

func (e *ambiguousSessionQueryError) Error() string {
	return fmt.Sprintf("session query %q is ambiguous (%d matches)", e.query, len(e.matches))
}

// looksLikeMachineSessionID reports whether query is the opaque HMAC form
// emitted by machineSessionIDWithKey (`session_` + 32 lowercase hex chars).
func looksLikeMachineSessionID(query string) bool {
	const prefix = "session_"
	if !strings.HasPrefix(query, prefix) {
		return false
	}
	hexPart := query[len(prefix):]
	if len(hexPart) != 32 {
		return false
	}
	for i := range len(hexPart) {
		c := hexPart[i]
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

func isDefaultInteractiveFlag(arg string) bool {
	switch arg {
	case "--model", "--max-steps", "--continue", "-c", "--resume", "-r", "--copy", "--dangerously-skip-permissions", "--yolo", "--permission-mode", "--effort", "--dir", "--append-system-prompt-file", "--add-dir", "--allowed-tools", "--allowedTools", "--profile", "--preset":
		return true
	}
	if name, _, ok := strings.Cut(arg, "="); ok && isDefaultInteractiveFlag(name) {
		return true
	}
	return false
}

func isDoctorRepairCommand(args []string) bool {
	return len(args) > 1 && args[0] == "doctor" && args[1] == "repair"
}
