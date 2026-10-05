// subject.go — what a permission rule matches against: the one string in a
// tool call that names what it will touch, and the glob that tests it.
package permission

import "encoding/json"

// subjectKeys are the JSON argument keys, in priority order, that carry a tool
// call's "subject" — the thing a Subject glob matches against. Generic so tools
// need not implement a permission-specific method: bash exposes command, the
// file tools expose path / file_path, grep & glob expose pattern. planId leads
// because it is a ticket rather than a target — see subjectRequiresHuman.
var subjectKeys = []string{"planId", "command", "file_path", "path", "source_path", "destination_path", "pattern", "origin", "app"}

// Subject extracts the primary matchable subject string from a call's raw JSON
// args, returning "" when none of the known keys is present (such a call only
// matches bare "ToolName" rules). Use Subjects for permission decisions that
// must account for every touched endpoint.
func Subject(args json.RawMessage) string {
	subjects := Subjects(args)
	if len(subjects) > 0 {
		return subjects[0]
	}
	return ""
}

// Subjects extracts every matchable subject from a call's raw JSON args. Most
// tools expose one subject; move_file exposes both source_path and
// destination_path so path-scoped permission rules can protect either endpoint.
func Subjects(args json.RawMessage) []string {
	if len(args) == 0 {
		return nil
	}
	var m map[string]any
	if err := json.Unmarshal(args, &m); err != nil {
		return nil
	}
	src := stringArg(m, "source_path")
	dst := stringArg(m, "destination_path")
	if src != "" && dst != "" {
		out := []string{src}
		if dst != src {
			out = append(out, dst)
		}
		return out
	}
	if origin := stringArg(m, "origin"); BrowserSubjectRequiresExplicitApproval(origin) {
		return browserSubjects(origin)
	}
	if app := stringArg(m, "app"); ComputerSubjectTakesPointer(app) {
		return computerSubjects(app)
	}
	for _, k := range subjectKeys {
		if s := stringArg(m, k); s != "" {
			return []string{s}
		}
	}
	return nil
}

func stringArg(m map[string]any, key string) string {
	if v, ok := m[key]; ok {
		if s, ok := v.(string); ok && s != "" {
			return s
		}
	}
	return ""
}

// matchGlob reports whether name matches pattern, where '*' matches any run of
// characters (including separators) and '?' matches exactly one. Unlike
// path.Match, '*' is not stopped by '/', which is what command-line and path
// prefixes ("rm -rf*", "/etc/*") intuitively expect. Linear time with
// backtracking, byte-oriented.
func matchGlob(pattern, name string) bool {
	var px, nx, starPx, starNx int
	starPx = -1
	for nx < len(name) {
		switch {
		case px < len(pattern) && pattern[px] == '*':
			starPx = px
			starNx = nx
			px++
		case px < len(pattern) && (pattern[px] == '?' || pattern[px] == name[nx]):
			px++
			nx++
		case starPx != -1:
			px = starPx + 1
			starNx++
			nx = starNx
		default:
			return false
		}
	}
	for px < len(pattern) && pattern[px] == '*' {
		px++
	}
	return px == len(pattern)
}
