package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"reasonix/internal/diff"
	"reasonix/internal/tool"
)

func init() { tool.RegisterBuiltin(regexReplace{}) }

// regexReplace performs regex-based find-and-replace in a single file. roots
// confines the target to the workspace when non-empty (see writeFile); workDir,
// when non-empty, is the directory a relative path resolves against (see
// resolveIn). The regex engine is Go RE2 (linear-time, no catastrophic
// backtracking).
type regexReplace struct {
	roots   []string
	workDir string
}

func (regexReplace) Name() string { return "regex_replace" }

func (regexReplace) Description() string {
	return "Replace text in a file using a Go RE2 regex pattern. Supports capture groups ($1, ${name}) in the replacement. Use flags for case-insensitive (i), multiline (m), dotall (s), or ungreedy (U) matching. Prefer this over edit_file when you need pattern matching instead of exact strings."
}

func (regexReplace) Schema() json.RawMessage {
	return json.RawMessage(`{
"type":"object",
"properties":{
  "path":{"type":"string","description":"File path"},
  "pattern":{"type":"string","description":"Go RE2 regular expression to match"},
  "replacement":{"type":"string","description":"Replacement text. Supports capture group references: $1, $2, ${name}. Use $$ for a literal $."},
  "flags":{"type":"string","description":"Regex flags (combine any): i=case-insensitive, m=multiline (^/$ match line boundaries), s=dotall (. matches \\n), U=ungreedy (non-greedy by default). Default: empty (no flags)."},
  "all":{"type":"boolean","description":"Replace all matches (default true). Set false to replace only the first match."}
},
"required":["path","pattern","replacement"]
}`)
}

func (regexReplace) ReadOnly() bool { return false }

func (r regexReplace) Execute(_ context.Context, args json.RawMessage) (string, error) {
	change, err := r.preview(args)
	if err != nil {
		return "", err
	}
	// Re-detect the file's encoding so the rewrite preserves it (GBK/UTF-16/BOM)
	// rather than forcing UTF-8 and corrupting a non-UTF-8 file.
	_, enc, err := readFileEncoded(change.Path)
	if err != nil {
		return "", fmt.Errorf("read %s: %w", change.Path, err)
	}
	if err := writeFileEncoded(change.Path, change.NewText, enc); err != nil {
		return "", fmt.Errorf("write %s: %w", change.Path, err)
	}
	return change.Diff, nil
}

func (r regexReplace) Preview(args json.RawMessage) (diff.Change, error) {
	return r.preview(args)
}

func (r regexReplace) preview(args json.RawMessage) (diff.Change, error) {
	var p struct {
		Path        string `json:"path"`
		Pattern     string `json:"pattern"`
		Replacement string `json:"replacement"`
		Flags       string `json:"flags"`
		All         *bool  `json:"all"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return diff.Change{}, fmt.Errorf("invalid args: %w", err)
	}
	if p.Path == "" {
		return diff.Change{}, fmt.Errorf("path is required")
	}
	if p.Pattern == "" {
		return diff.Change{}, fmt.Errorf("pattern is required")
	}

	all := true
	if p.All != nil {
		all = *p.All
	}

	prefix, err := flagsToPrefix(p.Flags)
	if err != nil {
		return diff.Change{}, err
	}

	re, err := regexp.Compile(prefix + p.Pattern)
	if err != nil {
		return diff.Change{}, fmt.Errorf("invalid regex pattern: %w", err)
	}

	p.Path = resolveIn(r.workDir, p.Path)
	if err := confine(r.roots, p.Path); err != nil {
		return diff.Change{}, err
	}

	original, _, err := readFileEncoded(p.Path)
	if err != nil {
		return diff.Change{}, fmt.Errorf("read %s: %w", p.Path, err)
	}

	var updated string
	var count int

	if all {
		matches := re.FindAllStringIndex(original, -1)
		if len(matches) == 0 {
			return diff.Change{}, fmt.Errorf("pattern did not match anything in %s", p.Path)
		}
		updated = re.ReplaceAllString(original, p.Replacement)
		count = len(matches)
	} else {
		match := re.FindStringSubmatchIndex(original)
		if match == nil {
			return diff.Change{}, fmt.Errorf("pattern did not match anything in %s", p.Path)
		}
		expanded := re.ExpandString(nil, p.Replacement, original, match)
		updated = original[:match[0]] + string(expanded) + original[match[1]:]
		count = 1
	}

	if updated == original {
		return diff.Change{}, fmt.Errorf("pattern matched %d time(s) in %s but replacement produced no change (replacement is identical to matched text)", count, p.Path)
	}

	return diff.Build(p.Path, original, updated, diff.Modify), nil
}

// flagsToPrefix converts a flags string (e.g. "ims") into a Go regex inline
// flag prefix like "(?ims)". Valid flags: i, m, s, U. An empty flags string
// yields an empty prefix (no flags).
func flagsToPrefix(flags string) (string, error) {
	flags = strings.TrimSpace(flags)
	if flags == "" {
		return "", nil
	}
	var b strings.Builder
	b.WriteString("(?")
	seen := map[rune]bool{}
	for _, c := range flags {
		switch c {
		case 'i', 'm', 's', 'U':
			if seen[c] {
				continue // deduplicate; Go regex rejects duplicate inline flags
			}
			seen[c] = true
			b.WriteRune(c)
		default:
			return "", fmt.Errorf("invalid regex flag %q — valid flags: i (case-insensitive), m (multiline), s (dotall), U (ungreedy)", string(c))
		}
	}
	b.WriteString(")")
	return b.String(), nil
}
