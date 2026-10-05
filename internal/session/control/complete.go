package control

import (
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"reasonix/internal/base/fileref"
	"reasonix/internal/base/gitignore"
	"reasonix/internal/ext/plugin"
)

// The three menus a composer can be showing. A frontend renders them
// differently but applies all of them the same way: replace [From, To) with the
// chosen item's Insert.
const (
	CompleteSlash    = "slash"     // the command word, while the line is a bare "/word"
	CompleteSlashArg = "slash-arg" // a structured argument of a slash command
	CompleteRef      = "ref"       // an @-reference: a path or an MCP resource
)

const (
	// maxCompletionItems caps one directory's contribution, so a pathologically
	// large folder cannot blow up the menu — one level is read, never a tree.
	maxCompletionItems = 200
	// maxRefSearchItems caps the basename search a bare @token also runs.
	maxRefSearchItems = 20
)

// Completion is one composer menu: the items, and the half-open byte span of
// the token an accepted item replaces. Replacing the span rather than the line
// is what lets a reference be completed mid-sentence — "see @fo|o and more"
// accepts to "see @foobar.md and more", with no dangling suffix.
type Completion struct {
	Kind string `json:"kind"`
	From int    `json:"from"`
	To   int    `json:"to"`
	// Query is what the menu filtered on, so a frontend can show why a row is
	// here rather than leaving a fuzzy hit unexplained.
	Query string      `json:"query,omitempty"`
	Items []SlashItem `json:"items"`
	// Typed is the command the line already spells in full. It stays out of
	// Items, where accepting it would change nothing, yet a terminal whose
	// Enter runs a finished command needs to know that command exists.
	Typed *SlashItem `json:"typed,omitempty"`
}

// CompletionData is everything Complete cannot read for itself. Names is the
// slash catalogue in the caller's own order — the kernel already decides which
// of a command and a skill of the same name wins, and the menu must not re-sort
// that answer.
type CompletionData struct {
	ArgData
	Names         []SlashItem
	WorkspaceRoot string
	// Scoped refuses to complete outside WorkspaceRoot. It mirrors what
	// SubmitHTTP will actually resolve, so a client reached over the network is
	// never offered a path the turn would then refuse.
	Scoped    bool
	Resources []plugin.Resource
}

// Complete is the single source of the composer's completion grammar: which
// token the cursor is in, what may replace it, and what span to replace. Every
// frontend calls this rather than parsing the line itself.
func Complete(line string, cursor int, d CompletionData) Completion {
	// An @-reference under the cursor wins — it can appear mid-line, even inside
	// a slash command's arguments ("/review @file").
	if from, to, query, ok := ActiveRefToken(line, cursor); ok {
		if items := dropTyped(d.refItems(query), line[from:to]); len(items) > 0 {
			_, frag := SplitPathToken(query)
			return Completion{Kind: CompleteRef, From: from, To: to, Query: UnescapeRefPath(frag), Items: items}
		}
	}
	if !strings.HasPrefix(line, "/") {
		return noCompletion()
	}
	// Still naming the command itself. Filtering uses the whole line so a
	// mid-token cursor never rewrites what was already typed.
	if !strings.ContainsAny(line, " \t\n") {
		matches := FuzzySlashNames(d.Names, line)
		typed := typedItem(matches, line)
		if items := dropTyped(matches, line); len(items) > 0 || typed != nil {
			return Completion{Kind: CompleteSlash, To: len(line), Query: line, Items: items, Typed: typed}
		}
		return noCompletion()
	}
	if items, from := SlashArgItems(line, d.ArgData); len(items) > 0 {
		to := RefTokenEnd(line, from)
		return Completion{Kind: CompleteSlashArg, From: from, To: to, Query: line[from:to], Items: items}
	}
	return noCompletion()
}

// dropTyped removes the row that would replace the token with itself. It is the
// line reading itself back, not a suggestion — and left in, a finished @path
// keeps a duplicate row that also swallows the next Enter.
func dropTyped(items []SlashItem, typed string) []SlashItem {
	out := items[:0]
	for _, it := range items {
		if it.Insert != typed {
			out = append(out, it)
		}
	}
	return out
}

// typedItem is the row dropTyped would remove: the token spelled in full.
func typedItem(items []SlashItem, typed string) *SlashItem {
	for _, it := range items {
		if it.Insert == typed {
			return &it
		}
	}
	return nil
}

// An empty item list rather than a nil one: this crosses a JSON boundary, and
// `null` there is a second empty value every client would have to handle.
func noCompletion() Completion {
	return Completion{Items: []SlashItem{}}
}

func (d CompletionData) refItems(token string) []SlashItem {
	if i := strings.Index(token, ":"); i > 0 && slices.Contains(d.ServerNames, token[:i]) {
		return RefResourceItems(d.Resources, token[:i], token[i+1:])
	}
	items := RefDirItems(d.WorkspaceRoot, token, d.Scoped)
	// Past the first segment the token is a path and nothing else. At the top
	// level it is still a name, so the whole workspace and the MCP resources
	// sharing the '@' namespace are candidates too.
	if strings.Contains(token, "/") {
		return items
	}
	seen := make(map[string]bool, len(items))
	for _, it := range items {
		seen[it.Insert] = true
	}
	remaining := min(maxCompletionItems-len(items), maxRefSearchItems)
	for _, it := range RefSearchItems(d.WorkspaceRoot, UnescapeRefPath(token), remaining) {
		if seen[it.Insert] {
			continue
		}
		items = append(items, it)
	}
	return append(items, RefResourceItems(d.Resources, "", token)...)
}

// ActiveRefToken finds the @-reference token under the byte offset cursor. The
// '@' must start the line or follow whitespace, so an email never opens a menu.
// [from, to) is the whole token — it extends past the caret to the next
// unescaped whitespace, while query stops at the caret: "@fo|o" filters as "fo"
// and still replaces "foo".
func ActiveRefToken(line string, cursor int) (from, to int, query string, ok bool) {
	if cursor < 0 || cursor > len(line) {
		cursor = len(line)
	}
	for i := cursor - 1; i >= 0; i-- {
		switch line[i] {
		case ' ', '\t':
			if i > 0 && line[i-1] == '\\' {
				i-- // escaped whitespace stays inside the token
				continue
			}
			return 0, 0, "", false
		case '\n':
			return 0, 0, "", false
		case '@':
			if i > 0 && line[i-1] != ' ' && line[i-1] != '\t' && line[i-1] != '\n' {
				return 0, 0, "", false
			}
			to = RefTokenEnd(line, i+1)
			return i, to, line[i+1 : min(max(cursor, i+1), to)], true
		}
	}
	return 0, 0, "", false
}

// RefTokenEnd returns the exclusive byte end of the token starting at from,
// stopping at the first unescaped whitespace.
func RefTokenEnd(line string, from int) int {
	for i := from; i < len(line); i++ {
		switch line[i] {
		case ' ', '\t':
			if i > 0 && line[i-1] == '\\' {
				continue
			}
			return i
		case '\n':
			return i
		}
	}
	return len(line)
}

// SplitPathToken splits a path token into the directory part, trailing slash
// kept, and the segment still being typed.
func SplitPathToken(token string) (dir, frag string) {
	if i := strings.LastIndex(token, "/"); i >= 0 {
		return token[:i+1], token[i+1:]
	}
	return "", token
}

// RefDirItems lists one directory level for a path token: entries of the
// token's directory whose name starts with the fragment. Directories descend,
// files complete. Hidden entries stay hidden unless the fragment asks for them.
func RefDirItems(root, token string, scoped bool) []SlashItem {
	dir, frag := SplitPathToken(token)
	// The typed token carries backslash-escaped spaces (completion inserts them
	// itself), so lookups need the real path while inserts keep the grammar.
	fsFrag := UnescapeRefPath(frag)
	readDir, ok := resolveRefDir(root, UnescapeRefPath(dir), scoped)
	if !ok {
		return nil
	}
	entries, err := os.ReadDir(readDir)
	if err != nil {
		return nil
	}
	// Directories first; ReadDir is already name-sorted.
	sort.SliceStable(entries, func(i, j int) bool { return entries[i].IsDir() && !entries[j].IsDir() })

	// Read from .gitignore, not from a list of names: "node_modules" is one
	// ecosystem's answer and the file is where this repository stated its own.
	ignored := gitignore.At(readDir, gitignore.Options{})
	// Inside a directory the rules already exclude, they exclude everything —
	// which is an empty list, not an answer. A user who typed their way in here
	// has named it, the way grep searches a path named explicitly.
	if ignored.Ignored(readDir, true) {
		ignored = nil
	}
	showHidden := strings.HasPrefix(fsFrag, ".")
	var items []SlashItem
	for _, e := range entries {
		name := e.Name()
		if !strings.HasPrefix(name, fsFrag) || (!showHidden && strings.HasPrefix(name, ".")) {
			continue
		}
		if ignored.Ignored(filepath.Join(readDir, name), e.IsDir()) {
			continue
		}
		if e.IsDir() {
			items = append(items, SlashItem{
				Label: name + "/", Insert: "@" + dir + EscapeRefPath(name) + "/",
				Kind: "dir", Descend: true,
			})
		} else {
			items = append(items, SlashItem{Label: name, Insert: "@" + dir + EscapeRefPath(name), Kind: "file"})
		}
		if len(items) >= maxCompletionItems {
			break
		}
	}
	return items
}

// resolveRefDir turns the token's directory part into a filesystem path.
// Scoped completion answers only for paths under root: it is the same boundary
// SubmitHTTP resolves references within, and offering more would complete a
// path the turn then refuses.
func resolveRefDir(root, dir string, scoped bool) (string, bool) {
	if root == "" {
		if scoped {
			return "", false
		}
		if dir == "" {
			return ".", true
		}
		return dir, true
	}
	if dir == "" {
		return root, true
	}
	if filepath.IsAbs(dir) {
		if scoped {
			return "", false
		}
		return dir, true
	}
	full := filepath.Join(root, filepath.FromSlash(dir))
	if !scoped {
		return full, true
	}
	rel, err := filepath.Rel(root, full)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", false
	}
	return full, true
}

// RefSearchItems matches a bare fragment against the whole workspace, so a file
// is reachable by name without typing the path down to it.
func RefSearchItems(root, frag string, limit int) []SlashItem {
	if root == "" {
		root = "."
	}
	if limit <= 0 {
		return nil
	}
	results := fileref.Search(root, frag, limit)
	items := make([]SlashItem, 0, len(results))
	for _, r := range results {
		escaped := EscapeRefPath(r.Path)
		if r.IsDir {
			items = append(items, SlashItem{Label: r.Path + "/", Insert: "@" + escaped + "/", Kind: "dir", Descend: true})
			continue
		}
		items = append(items, SlashItem{Label: r.Path, Insert: "@" + escaped, Kind: "file"})
	}
	return items
}

// RefResourceItems lists MCP resources as @server:uri completions. An empty
// server matches on the whole "server:uri" prefix, which is what a token that
// has not reached its colon yet is still choosing between.
func RefResourceItems(resources []plugin.Resource, server, frag string) []SlashItem {
	var items []SlashItem
	for _, r := range resources {
		ref := r.Server + ":" + r.URI
		switch {
		case server == "":
			if !strings.HasPrefix(ref, frag) {
				continue
			}
		case r.Server == server:
			if !strings.HasPrefix(r.URI, frag) {
				continue
			}
		default:
			continue
		}
		hint := r.Name
		if hint == "" {
			hint = r.Server
		}
		items = append(items, SlashItem{Label: "@" + ref, Insert: "@" + ref, Hint: hint, Kind: "resource"})
	}
	return items
}

// FuzzySlashNames returns the catalogue entries matching query as a
// case-insensitive subsequence of their label, prefix hits first and the
// caller's order preserved inside each group. Typing "/modl" still finds
// "/model". An empty query matches everything.
func FuzzySlashNames(items []SlashItem, query string) []SlashItem {
	if query == "" {
		return slices.Clone(items)
	}
	lq := strings.ToLower(query)
	var prefix, rest []SlashItem
	for _, it := range items {
		l := strings.ToLower(it.Label)
		switch {
		case strings.HasPrefix(l, lq):
			prefix = append(prefix, it)
		case SubsequenceMatch(l, lq):
			rest = append(rest, it)
		}
	}
	return append(prefix, rest...)
}

// SubsequenceMatch reports whether query appears in target as a subsequence —
// each rune in order, not necessarily contiguous. Callers pass already
// case-folded strings; an empty query matches every target.
func SubsequenceMatch(target, query string) bool {
	if query == "" {
		return true
	}
	qr := []rune(query)
	ti := 0
	for _, r := range target {
		if r == qr[ti] {
			ti++
			if ti == len(qr) {
				return true
			}
		}
	}
	return false
}
