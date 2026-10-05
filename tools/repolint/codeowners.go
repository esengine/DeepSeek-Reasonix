// codeowners.go — who answers for a path, read from .github/CODEOWNERS.
// The first owner on a line is the primary and the second the backup; GitHub
// requests review from both, and the documentation gate reads the order.
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

const codeownersPath = ".github/CODEOWNERS"

type ownerRule struct {
	line    int
	pattern string
	owners  []string
	match   *regexp.Regexp
}

func loadOwnerRules(root string) ([]ownerRule, error) {
	data, err := os.ReadFile(filepath.Join(root, codeownersPath))
	if err != nil {
		return nil, err
	}
	var rules []ownerRule
	for i, raw := range strings.Split(string(data), "\n") {
		fields := strings.Fields(raw)
		if len(fields) == 0 || strings.HasPrefix(fields[0], "#") {
			continue
		}
		rules = append(rules, ownerRule{line: i + 1, pattern: fields[0], owners: fields[1:], match: ownerPattern(fields[0])})
	}
	return rules, nil
}

// ownersOf is the last matching rule, which is the one GitHub applies.
func ownersOf(rules []ownerRule, rel string) (ownerRule, bool) {
	for _, r := range slices.Backward(rules) {
		if r.match.MatchString(rel) {
			return r, true
		}
	}
	return ownerRule{}, false
}

// ownerPattern translates the subset of CODEOWNERS syntax this repository
// uses: a leading slash anchors, a trailing slash names a directory, * stays
// within a segment and ** crosses them.
func ownerPattern(pattern string) *regexp.Regexp {
	anchored := strings.HasPrefix(pattern, "/") || strings.Contains(strings.Trim(pattern, "/"), "/")
	dir := strings.HasSuffix(pattern, "/")
	body := strings.Trim(pattern, "/")
	var b strings.Builder
	for i := 0; i < len(body); i++ {
		switch {
		case strings.HasPrefix(body[i:], "**"):
			b.WriteString(".*")
			i++
		case body[i] == '*':
			b.WriteString("[^/]*")
		case body[i] == '?':
			b.WriteString("[^/]")
		default:
			b.WriteString(regexp.QuoteMeta(body[i : i+1]))
		}
	}
	prefix := "^"
	if !anchored {
		prefix = "^(?:.*/)?"
	}
	suffix := "(?:/.*)?$"
	if dir {
		suffix = "/.*$"
	}
	return regexp.MustCompile(prefix + b.String() + suffix)
}

// checkCodeOwners holds every rule to a path that exists and to a primary and
// a backup who are two different people. A rule for a deleted file routes
// nothing, and reading it tells a newcomer the file is still owned.
func checkCodeOwners(root string) []Finding {
	rules, err := loadOwnerRules(root)
	if err != nil {
		return []Finding{{codeownersPath, 1, ruleCodeOwners, "no CODEOWNERS: nothing names who answers for a path", 1}}
	}
	paths, err := repoFiles(root)
	var out []Finding
	for _, r := range rules {
		if len(r.owners) < 2 || r.owners[0] == r.owners[1] {
			out = append(out, Finding{codeownersPath, r.line, ruleCodeOwners,
				fmt.Sprintf("%s names %v; a rule needs a primary and a different backup", r.pattern, r.owners), 1})
		}
		if err == nil && !anyMatch(r.match, paths) {
			out = append(out, Finding{codeownersPath, r.line, ruleCodeOwners,
				fmt.Sprintf("%s matches no file in the tree; remove the rule with the file it owned", r.pattern), 1})
		}
	}
	return out
}

func anyMatch(re *regexp.Regexp, paths []string) bool {
	return slices.ContainsFunc(paths, re.MatchString)
}

// repoFiles is the tree a reviewer would see: tracked files plus new ones git
// does not ignore, minus anything deleted from the working tree. A walk would
// also read local worktrees and caches nobody commits.
func repoFiles(root string) ([]string, error) {
	listed, err := gitPaths(root, "ls-files", "--cached", "--others", "--exclude-standard")
	if err != nil {
		return nil, err
	}
	out := listed[:0]
	for _, rel := range listed {
		if _, err := os.Stat(filepath.Join(root, rel)); err == nil {
			out = append(out, rel)
		}
	}
	return out, nil
}
