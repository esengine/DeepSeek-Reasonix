package instruction

import (
	"context"
	"strings"
)

// VerifyCheck is a host-observable project check extracted from structured
// project memory. It is runtime-only and is not serialized into prompts.
type VerifyCheck struct {
	Command    string
	SourcePath string
	Line       int
}

type contextKey struct{}

func WithChecks(ctx context.Context, checks []VerifyCheck) context.Context {
	if len(checks) == 0 {
		return ctx
	}
	cp := append([]VerifyCheck(nil), checks...)
	return context.WithValue(ctx, contextKey{}, cp)
}

func FromContext(ctx context.Context) []VerifyCheck {
	checks, ok := ctx.Value(contextKey{}).([]VerifyCheck)
	if !ok || len(checks) == 0 {
		return nil
	}
	return append([]VerifyCheck(nil), checks...)
}

// HostChecksHeading is the one section a project's checks are read from. It is
// exported because the host quotes it back when its own classifier cannot
// recognise what a project runs, and a name told to the model in one place and
// parsed in another drifts.
const HostChecksHeading = "Reasonix host checks"

// ExtractHostChecks reads only the HostChecksHeading section. Ordinary project
// instructions remain guidance and do not become hard gates.
func ExtractHostChecks(docs []Document) []VerifyCheck {
	seen := map[string]bool{}
	var checks []VerifyCheck
	forEachHostCheckLine(docs, func(line string, doc Document, i int) {
		command, ok := verifyBullet(line)
		if !ok || seen[command] {
			return
		}
		seen[command] = true
		checks = append(checks, VerifyCheck{Command: command, SourcePath: doc.Path, Line: i + 1})
	})
	return checks
}

// forEachHostCheckLine visits every line inside the HostChecksHeading section,
// so the bullet kinds a project may declare there stay one parser.
func forEachHostCheckLine(docs []Document, visit func(line string, doc Document, index int)) {
	for _, doc := range docs {
		inSection := false
		for i, raw := range strings.Split(doc.Body, "\n") {
			line := strings.TrimRight(raw, "\r")
			if heading, ok := markdownHeading(line); ok {
				inSection = strings.EqualFold(heading, HostChecksHeading)
				continue
			}
			if inSection {
				visit(line, doc, i)
			}
		}
	}
}

func markdownHeading(line string) (string, bool) {
	line = strings.TrimSpace(line)
	if !strings.HasPrefix(line, "#") {
		return "", false
	}
	i := 0
	for i < len(line) && line[i] == '#' {
		i++
	}
	if i == 0 || i >= len(line) || line[i] != ' ' {
		return "", false
	}
	heading := strings.TrimSpace(line[i+1:])
	heading = strings.TrimSpace(strings.TrimRight(heading, "#"))
	return heading, heading != ""
}

func verifyBullet(line string) (string, bool) {
	return prefixedBullet(line, "verify:")
}

func prefixedBullet(line, prefix string) (string, bool) {
	line = strings.TrimSpace(line)
	if len(line) < 2 || (line[:2] != "- " && line[:2] != "* ") {
		return "", false
	}
	body := strings.TrimSpace(line[2:])
	if len(body) < len(prefix) || !strings.EqualFold(body[:len(prefix)], prefix) {
		return "", false
	}
	value := strings.TrimSpace(body[len(prefix):])
	return value, value != ""
}

// ExtractSensitivePaths reads the `sensitive:` bullets of the same section.
// A project names the paths whose changes it wants reviewed hardest; the host
// does not try to infer that from path spelling, which cannot distinguish
// `internal/auth` from `session_write_authority.go`.
func ExtractSensitivePaths(docs []Document) []string {
	seen := map[string]bool{}
	var globs []string
	forEachHostCheckLine(docs, func(line string, _ Document, _ int) {
		glob, ok := prefixedBullet(line, "sensitive:")
		if !ok || seen[glob] {
			return
		}
		seen[glob] = true
		globs = append(globs, glob)
	})
	return globs
}
