package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"unicode"

	"github.com/spf13/pflag"

	"reasonix/internal/base/i18n"
	"reasonix/internal/state/sessionstore"
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

func resolveSessionQuery(dir, query string) (string, error) {
	query = strings.TrimSpace(query)
	if query == "" || query == resumePickerSentinel {
		return "", nil
	}
	if info, err := os.Stat(query); err == nil && !info.IsDir() {
		abs, absErr := filepath.Abs(query)
		if absErr != nil {
			return "", absErr
		}
		return abs, nil
	}
	sessions, err := sessionstore.ListSessions(dir)
	if err != nil {
		return "", fmt.Errorf("list sessions: %w", err)
	}
	// Opaque machine session IDs (session_<hex>) are what --events-jsonl and
	// `session show --json` expose. Match them before preview/partial search so
	// one-shot `run --resume` can resume without scanning private paths (#7429).
	if looksLikeMachineSessionID(query) {
		key, keyErr := loadMachineIdentityKey()
		if keyErr != nil {
			return "", fmt.Errorf("machine identity is unavailable: %w", keyErr)
		}
		for _, session := range sessions {
			if machineSessionIDWithKey(sessionstore.BranchID(session.Path), key) == query {
				return session.Path, nil
			}
		}
		return "", fmt.Errorf("no session matches %q", query)
	}
	lower := strings.ToLower(query)
	var exact []sessionstore.SessionInfo
	var partial []sessionstore.SessionInfo
	for _, session := range sessions {
		id := sessionstore.BranchID(session.Path)
		base := filepath.Base(session.Path)
		if query == id || query == base || query == session.Path {
			exact = append(exact, session)
			continue
		}
		haystack := strings.ToLower(strings.Join([]string{id, base, session.CustomTitle, session.TopicTitle, session.Preview}, "\n"))
		if strings.Contains(haystack, lower) {
			partial = append(partial, session)
		}
	}
	matches := exact
	if len(matches) == 0 {
		matches = partial
	}
	switch len(matches) {
	case 0:
		return "", fmt.Errorf("no session matches %q", query)
	case 1:
		return matches[0].Path, nil
	default:
		return "", &ambiguousSessionQueryError{Query: query, Matches: matches}
	}
}

// ambiguousSessionQueryError is a --resume query more than one session
// matches; Matches lets the caller offer them instead of only refusing.
type ambiguousSessionQueryError struct {
	Query   string
	Matches []sessionstore.SessionInfo
}

func (e *ambiguousSessionQueryError) Error() string {
	return fmt.Sprintf("session query %q is ambiguous (%d matches)", e.Query, len(e.Matches))
}

// reportResumeQueryError prints why a --resume query resolved to nothing and,
// when several sessions matched, each one by the id --resume accepts.
func reportResumeQueryError(w io.Writer, err error) {
	fmt.Fprintln(w, i18n.M.ErrorPrefix, err)
	var ambiguous *ambiguousSessionQueryError
	if !errors.As(err, &ambiguous) {
		return
	}
	for _, s := range ambiguous.Matches {
		fmt.Fprintf(w, "  %s  %s  %s\n", s.LastActivityAt.Local().Format("2006-01-02 15:04"), sessionstore.BranchID(s.Path), sessionPickerLabel(s))
	}
	fmt.Fprintln(w, i18n.M.AmbiguousResumeHint)
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

// registerRunApprovalFlags gives run the approval spellings a bare reasonix
// takes, so a flag keeps its meaning on either side of the verb.
func registerRunApprovalFlags(fs *pflag.FlagSet) (auto, yolo *bool) {
	auto = fs.BoolP("auto", "y", false, "explicitly auto-approve ordinary writer fallbacks (alias for --permission-mode auto)")
	yolo = fs.Bool("yolo", false, "skip approval prompts; the sandbox, network policy and deny rules still apply (alias for --permission-mode bypassPermissions)")
	fs.BoolVar(yolo, "dangerously-skip-permissions", false, "alias for --yolo")
	return auto, yolo
}

func resolveRunPermissionMode(value string, auto, yolo, modeExplicit bool) (string, error) {
	switch {
	case auto && yolo:
		return "", errors.New("--auto/-y cannot be combined with --yolo")
	case auto && modeExplicit:
		return "", errors.New("--auto/-y cannot be combined with --permission-mode")
	case yolo && modeExplicit:
		return "", errors.New("--yolo cannot be combined with --permission-mode")
	case auto:
		return "auto", nil
	case yolo:
		return "bypassPermissions", nil
	}
	return value, nil
}

// startsWithSessionFlag reports whether argv opens with a flag rather than a
// verb; -y and -p belong to run alone but may still be written first.
func startsWithSessionFlag(arg string) bool {
	switch arg {
	case "-y", "--auto", "-p", "--print":
		return true
	}
	return isDefaultInteractiveFlag(arg) || strings.HasPrefix(arg, "--auto=")
}

// splitAtRunVerb finds the first positional with the union of the terminal
// UI's flags and run's, so a value of either is never taken for the verb.
// lead is resume-normalized, as runTUI would read it.
func splitAtRunVerb(args []string) (lead, rest []string, verb bool) {
	norm := normalizeOptionalResumeArg(args)
	fs := quietFlagSet(newTUIFlags().fs)
	newRunFlags().fs.VisitAll(func(f *pflag.Flag) {
		if fs.Lookup(f.Name) == nil && (f.Shorthand == "" || fs.ShorthandLookup(f.Shorthand) == nil) {
			fs.AddFlag(f)
		}
	})
	if fs.Parse(norm) != nil {
		return nil, nil, false
	}
	rest = fs.Args()
	lead = norm[:len(norm)-len(rest)]
	return lead, rest, len(rest) > 0 && rest[0] == "run" && !slices.Contains(lead, "--")
}

// leadingFlagsIntoRun moves the flags written before `run` to after it, but
// only when the session flags (the terminal UI's, plus -y and -p) and run's
// read every one of them identically; otherwise the line stays the terminal
// UI's, which reports a flag it does not take.
func leadingFlagsIntoRun(args []string) ([]string, bool) {
	if len(args) == 0 || !startsWithSessionFlag(args[0]) {
		return nil, false
	}
	lead, rest, verb := splitAtRunVerb(args)
	if !verb {
		return nil, false
	}
	session := quietFlagSet(newTUIFlags().fs)
	session.BoolP("auto", "y", false, "")
	session.BoolP("print", "p", false, "")
	run := quietFlagSet(newRunFlags().fs)
	if session.Parse(lead) != nil || run.Parse(lead) != nil || session.NArg() != 0 || run.NArg() != 0 ||
		!slices.Equal(setFlagValues(session), setFlagValues(run)) {
		return nil, false
	}
	return append(append([]string{"run"}, lead...), rest[1:]...), true
}

// flagsBeforeVerb is where a leading -p may be written: a -p after `run`
// belongs to run itself.
func flagsBeforeVerb(args []string) []string {
	if lead, _, verb := splitAtRunVerb(args); verb {
		return lead
	}
	return args
}

func quietFlagSet(fs *pflag.FlagSet) *pflag.FlagSet {
	fs.SetInterspersed(false)
	fs.SetOutput(io.Discard)
	return fs
}

// setFlagValues is each set flag with its value: two sets that name the same
// flags can still disagree on what a token meant, such as a value-optional
// --resume against one that consumes the next argument.
func setFlagValues(fs *pflag.FlagSet) []string {
	var set []string
	fs.Visit(func(f *pflag.Flag) { set = append(set, f.Name+"="+f.Value.String()) })
	return set
}
