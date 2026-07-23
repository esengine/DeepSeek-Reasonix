package memory

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"reasonix/internal/config"
	fileencoding "reasonix/internal/fileutil/encoding"
	"reasonix/internal/frontmatter"
)

// Store is the per-project auto-memory: a directory of one-fact-per-file
// Markdown notes with frontmatter, plus a MEMORY.md index of one line per fact.
// The model maintains it through the `remember` tool; the index loads into the
// cached system-prompt prefix at boot so the model always knows what it has
// saved, and reads individual facts on demand with read_file. The whole thing is
// plain files the user can edit by hand.
//
// Memories of type "user" and "feedback" are routed to GlobalDir (shared across
// all projects), while "project" and "reference" stay in the project-specific Dir.
// List() and Index() merge both directories so every session sees the full set.
type Store struct {
	Dir       string // ...reasonix/projects/<slug>/memory
	GlobalDir string // ...reasonix/memory/global (shared across projects)
}

// Type classifies a memory, mirroring the auto-memory taxonomy.
type Type string

const (
	TypeUser      Type = "user"      // who the user is: role, preferences, expertise
	TypeFeedback  Type = "feedback"  // guidance on how to work (with why + how-to-apply)
	TypeProject   Type = "project"   // ongoing work / goals / constraints not in the code
	TypeReference Type = "reference" // pointers to external resources (URLs, tickets)
)

// validTypes is the closed set the `remember` tool accepts; anything else
// normalises to TypeProject.
var validTypes = map[Type]bool{TypeUser: true, TypeFeedback: true, TypeProject: true, TypeReference: true}

// NormalizeType coerces an arbitrary string to a known Type, defaulting to
// TypeProject so a sloppy tool argument never blocks a save.
func NormalizeType(s string) Type {
	t := Type(strings.ToLower(strings.TrimSpace(s)))
	if validTypes[t] {
		return t
	}
	return TypeProject
}

// Memory is one stored fact.
type Memory struct {
	Name        string // kebab-case slug; also the file stem (<name>.md)
	Title       string // human-readable index label; falls back to a de-kebabed Name
	Description string // one-line summary used for the index and recall
	Type        Type
	Body        string // the fact itself (Markdown)
}

// ArchivedMemory is a saved fact that has been removed from active memory but
// kept on disk for traceability.
type ArchivedMemory struct {
	Memory
	Path       string
	ArchivedAt time.Time
}

// StoreFor resolves the auto-memory directory for a project working dir under
// Reasonix home, e.g. ~/.reasonix/projects/-Users-me-proj/memory.
// A "" userDir (config dir unresolvable) yields a zero Store, which all methods
// treat as a disabled no-op.
func StoreFor(userDir, cwd string) Store {
	if userDir == "" {
		return Store{}
	}
	return Store{
		Dir:       filepath.Join(userDir, "projects", config.WorkspaceSlug(absOf(cwd)), "memory"),
		GlobalDir: filepath.Join(userDir, "memory", "global"),
	}
}

// DirFor returns the directory a memory of the given type should be stored in.
// TypeUser and TypeFeedback go to GlobalDir (shared across all projects);
// everything else goes to the project-specific Dir. When GlobalDir is empty,
// all types fall back to Dir.
func (s Store) DirFor(t Type) string {
	if s.GlobalDir != "" && (t == TypeUser || t == TypeFeedback) {
		return s.GlobalDir
	}
	return s.Dir
}

// indexFile is the human-readable index of saved memories.
const indexFile = "MEMORY.md"

// dirs returns the directories to read from, in order: GlobalDir first (shared
// memories), then Dir (project-specific).
func (s Store) dirs() []string {
	if s.GlobalDir != "" && s.GlobalDir != s.Dir {
		return []string{s.GlobalDir, s.Dir}
	}
	return []string{s.Dir}
}

// Index returns the MEMORY.md contents (the per-line index of saved memories),
// or "" if there are none yet. This is what loads into the cached prefix.
// When both GlobalDir and Dir have indexes, they are merged with deduplication
// (global first).
func (s Store) Index() string {
	managed := map[string]string{}
	for _, dir := range s.dirs() {
		if dir == "" {
			continue
		}
		b, err := fileencoding.ReadFileUTF8(filepath.Join(dir, indexFile))
		if err != nil {
			continue
		}
		for _, line := range strings.Split(string(b), "\n") {
			if mt := indexLineRe.FindStringSubmatch(line); mt != nil {
				if _, exists := managed[mt[1]]; !exists {
					managed[mt[1]] = strings.TrimRight(line, "\r")
				}
			}
		}
	}
	if len(managed) == 0 {
		return ""
	}
	names := make([]string, 0, len(managed))
	for n := range managed {
		names = append(names, n)
	}
	sort.Strings(names)
	var b strings.Builder
	for _, n := range names {
		b.WriteString(managed[n])
		b.WriteString("\n")
	}
	return b.String()
}

// Path returns the absolute file path a memory with the given name lives at.
// It checks GlobalDir first, then Dir, returning the first match. If no file
// exists yet, it returns the path in Dir (the default for project types).
func (s Store) Path(name string) string {
	stem := slug(name) + ".md"
	for _, dir := range s.dirs() {
		if dir == "" {
			continue
		}
		p, err := safeJoin(dir, stem)
		if err != nil {
			continue
		}
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	p, sjErr := safeJoin(s.Dir, stem)
	if sjErr != nil {
		return ""
	}
	return p
}

// Save writes (or overwrites) a memory file and refreshes its MEMORY.md index
// line. It is the single mutation entry point — the `remember` tool, the desktop
// editor, and any future importer all go through here so the index never drifts
// from the files. Returns the path written.
func (s Store) Save(m Memory) (string, error) {
	dir := s.DirFor(m.Type)
	if dir == "" {
		return "", fmt.Errorf("memory store unavailable (no user config dir)")
	}
	if strings.TrimSpace(m.Name) == "" {
		return "", fmt.Errorf("memory needs a name")
	}
	name := slug(m.Name)
	if name == "" {
		return "", fmt.Errorf("memory name needs at least one letter or digit")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	path, err := safeJoin(dir, name+".md")
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(path, []byte(render(m, name)), 0o644); err != nil {
		return "", err
	}
	if err := reindexIn(dir, name, m); err != nil {
		return path, err
	}
	for _, otherDir := range s.dirs() {
		if sameDir(otherDir, dir) {
			continue
		}
		if err := removeActiveMemoryInDir(otherDir, name); err != nil {
			return path, err
		}
	}
	return path, nil
}

// Archive removes a memory from the active store and moves its file under
// .archive/ for traceability. A missing file is not an error; the goal state
// (not active) already holds. It returns the archive path, or "" when no file
// existed to archive.
// When both GlobalDir and Dir exist, it archives from every directory the
// memory appears in (handles migration duplicates).
func (s Store) Archive(name string) (string, error) {
	if s.Dir == "" && s.GlobalDir == "" {
		return "", fmt.Errorf("memory store unavailable (no user config dir)")
	}
	name = slug(name)
	if name == "" {
		return "", fmt.Errorf("memory needs a name")
	}
	var lastPath string
	for _, dir := range s.dirs() {
		if dir == "" {
			continue
		}
		p, err := archiveInDir(dir, name)
		if err != nil {
			return "", err
		}
		if p != "" || indexContainsIn(dir, name) {
			if err := flushIndexIn(dir, indexLinesExceptIn(dir, name)); err != nil {
				return "", err
			}
		}
		if p != "" {
			lastPath = p
		}
	}
	return lastPath, nil
}

func removeActiveMemoryInDir(dir, name string) error {
	if strings.TrimSpace(dir) == "" {
		return nil
	}
	p, err := archiveInDir(dir, name)
	if err != nil {
		return err
	}
	if p != "" || indexContainsIn(dir, name) {
		return flushIndexIn(dir, indexLinesExceptIn(dir, name))
	}
	return nil
}

// Delete removes a memory from the active store and its MEMORY.md line — the
// model's `forget` path and the user's way to prune a stale fact. It archives
// the file instead of permanently deleting it so wrong memories remain
// traceable. A missing file is not an error; the goal state (gone) holds either
// way.
func (s Store) Delete(name string) error {
	_, err := s.Archive(name)
	return err
}

func archiveInDir(dir, name string) (string, error) {
	root, err := os.OpenRoot(dir)
	if os.IsNotExist(err) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	defer root.Close()

	file := name + ".md"
	if _, err := root.Stat(file); err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}
	if err := root.MkdirAll(".archive", 0o755); err != nil {
		return "", err
	}
	dest, err := archivePath(root, name, time.Now().UTC())
	if err != nil {
		return "", err
	}
	if err := renameMemoryFile(root, file, dest); err != nil {
		return "", err
	}
	out, err := safeJoin(dir, dest)
	if err != nil {
		return "", err
	}
	return out, nil
}

func archivePath(root *os.Root, name string, when time.Time) (string, error) {
	stem := when.Format("20060102-150405.000") + "-" + name
	path := filepath.Join(".archive", stem+".md")
	if _, err := root.Stat(path); os.IsNotExist(err) {
		return path, nil
	} else if err != nil {
		return "", err
	}
	for i := 1; ; i++ {
		path = filepath.Join(".archive", fmt.Sprintf("%s-%d.md", stem, i))
		if _, err := root.Stat(path); os.IsNotExist(err) {
			return path, nil
		} else if err != nil {
			return "", err
		}
	}
}

func safeJoin(base, name string) (string, error) {
	if base == "" {
		return "", fmt.Errorf("memory store unavailable (no user config dir)")
	}
	if !filepath.IsLocal(name) {
		return "", fmt.Errorf("memory path escapes store: %s", name)
	}
	baseAbs, err := filepath.Abs(base)
	if err != nil {
		return "", err
	}
	path := filepath.Join(baseAbs, name)
	pathAbs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(baseAbs, pathAbs)
	if err != nil {
		return "", err
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) || filepath.IsAbs(rel) {
		return "", fmt.Errorf("memory path escapes store: %s", name)
	}
	return pathAbs, nil
}

func renameMemoryFile(root *os.Root, path, dest string) error {
	err := root.Rename(path, dest)
	if err == nil || os.IsNotExist(err) {
		return nil
	}
	if !os.IsPermission(err) {
		return err
	}
	repairOwnerWrite(root, path, false)
	repairOwnerWrite(root, filepath.Dir(path), true)
	repairOwnerWrite(root, filepath.Dir(dest), true)
	err = root.Rename(path, dest)
	if err == nil || os.IsNotExist(err) {
		return nil
	}
	return err
}

func repairOwnerWrite(root *os.Root, path string, dir bool) {
	info, err := root.Stat(path)
	if err != nil {
		return
	}
	need := os.FileMode(0o600)
	if dir {
		need = 0o700
	}
	_ = root.Chmod(path, info.Mode().Perm()|need)
}

// memoryFrontmatter is the YAML shape render emits, mirroring the auto-memory
// shape (name / description / metadata.type) so the files are interchangeable
// with that ecosystem and re-readable by loadMemory. Marshaled by yaml.v3 so a
// title or description containing ": ", '#', or quotes is escaped instead of
// corrupting the block — frontmatter.Split returns an EMPTY map for
// unparseable YAML, which would silently drop the memory's name/title/type on
// the next load. Plain values render byte-identically to the previous
// hand-built format.
type memoryFrontmatter struct {
	Name     string `yaml:"name"`
	Title    string `yaml:"title,omitempty"`
	Desc     string `yaml:"description"`
	Metadata struct {
		Type string `yaml:"type"`
	} `yaml:"metadata"`
}

// render serializes a memory to frontmatter + body.
func render(m Memory, name string) string {
	fm := memoryFrontmatter{Name: name, Title: oneLine(m.Title), Desc: oneLine(m.Description)}
	fm.Metadata.Type = string(NormalizeType(string(m.Type)))
	var b strings.Builder
	b.WriteString("---\n")
	enc := yaml.NewEncoder(&b)
	enc.SetIndent(2)
	// Encoding a flat struct of strings cannot fail.
	_ = enc.Encode(fm)
	_ = enc.Close()
	b.WriteString("---\n\n")
	b.WriteString(strings.TrimSpace(m.Body))
	b.WriteString("\n")
	return b.String()
}

// indexLineRe matches a managed index line so reindex/Delete can target the line
// for one memory by its filename without disturbing the rest of a hand-edited
// MEMORY.md.
var indexLineRe = regexp.MustCompile(`(?m)^\s*-\s\[.+?\]\(([^)]+)\.md\)\s*—\s.*$`)

// indexLinesExceptIn returns the managed MEMORY.md lines keyed by filename stem
// in the given directory, dropping the entry for name (a missing index → empty map).
func indexLinesExceptIn(dir, name string) map[string]string {
	existing, _ := fileencoding.ReadFileUTF8(filepath.Join(dir, indexFile))
	keep := map[string]string{}
	for _, line := range strings.Split(string(existing), "\n") {
		if mt := indexLineRe.FindStringSubmatch(line); mt != nil && mt[1] != name {
			keep[mt[1]] = strings.TrimRight(line, "\r")
		}
	}
	return keep
}

func indexContainsIn(dir, name string) bool {
	existing, err := fileencoding.ReadFileUTF8(filepath.Join(dir, indexFile))
	if err != nil {
		return false
	}
	for _, line := range strings.Split(string(existing), "\n") {
		if mt := indexLineRe.FindStringSubmatch(line); mt != nil && mt[1] == name {
			return true
		}
	}
	return false
}

// flushIndexIn rewrites MEMORY.md in the given directory from the managed lines,
// preserving hand-written content. Managed lines are updated or removed, and
// new managed entries are appended in sorted order.
func flushIndexIn(dir string, lines map[string]string) error {
	path := filepath.Join(dir, indexFile)
	existing, _ := fileencoding.ReadFileUTF8(path)
	processed := map[string]bool{}
	var preserved strings.Builder
	preservedEmpty := true
	for _, line := range strings.Split(string(existing), "\n") {
		trimmed := strings.TrimRight(line, "\r")
		if mt := indexLineRe.FindStringSubmatch(trimmed); mt != nil {
			name := mt[1]
			if fresh, ok := lines[name]; ok {
				preserved.WriteString(fresh)
				preserved.WriteString("\n")
				processed[name] = true
				preservedEmpty = false
			}
			continue
		}
		preserved.WriteString(trimmed)
		preserved.WriteString("\n")
		if strings.TrimSpace(trimmed) != "" {
			preservedEmpty = false
		}
	}

	names := make([]string, 0, len(lines))
	for n := range lines {
		if !processed[n] {
			names = append(names, n)
		}
	}
	sort.Strings(names)

	var b strings.Builder
	if preservedEmpty && len(names) > 0 {
		b.WriteString("# Memory\n\n")
	} else {
		b.WriteString(preserved.String())
	}
	for _, n := range names {
		b.WriteString(lines[n])
		b.WriteString("\n")
	}
	result := strings.TrimRight(b.String(), "\n")
	if result == "" {
		return os.WriteFile(path, []byte(""), 0o644)
	}
	return os.WriteFile(path, []byte(result+"\n"), 0o644)
}

// reindexIn rewrites the MEMORY.md line for name in the given directory,
// preserving every other managed line.
func reindexIn(dir, name string, m Memory) error {
	lines := indexLinesExceptIn(dir, name)
	lines[name] = fmt.Sprintf("- [%s](%s.md) — %s", displayTitle(m.Title, name), name, oneLine(m.Description))
	return flushIndexIn(dir, lines)
}

// List returns the saved memories parsed from their files, sorted by name. Used
// by `/memory` and the desktop memory panel. Reads from both GlobalDir and Dir,
// merging results. Files that fail to parse are skipped so one bad file never
// hides the rest.
func (s Store) List() []Memory {
	if s.Dir == "" && s.GlobalDir == "" {
		return nil
	}
	var out []Memory
	seen := map[string]bool{}
	for _, dir := range s.dirs() {
		if dir == "" {
			continue
		}
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if e.IsDir() || e.Name() == indexFile || !strings.HasSuffix(e.Name(), ".md") {
				continue
			}
			if m, ok := loadMemory(filepath.Join(dir, e.Name())); ok {
				if !seen[m.Name] {
					out = append(out, m)
					seen[m.Name] = true
				}
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// ListArchived returns archived memories parsed from .archive/, newest first.
// Archived files stay out of List() and the prompt index, so stale facts remain
// inspectable without being reused as active truth. Reads from both GlobalDir
// and Dir.
func (s Store) ListArchived() []ArchivedMemory {
	if s.Dir == "" && s.GlobalDir == "" {
		return nil
	}
	var out []ArchivedMemory
	for _, base := range s.dirs() {
		if base == "" {
			continue
		}
		dir := filepath.Join(base, ".archive")
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
				continue
			}
			path := filepath.Join(dir, e.Name())
			m, ok := loadMemory(path)
			if !ok {
				continue
			}
			when := archiveTimeFromName(e.Name())
			if when.IsZero() {
				if info, err := e.Info(); err == nil {
					when = info.ModTime()
				}
			}
			out = append(out, ArchivedMemory{Memory: m, Path: path, ArchivedAt: when})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if !out[i].ArchivedAt.Equal(out[j].ArchivedAt) {
			return out[i].ArchivedAt.After(out[j].ArchivedAt)
		}
		if out[i].Name != out[j].Name {
			return out[i].Name < out[j].Name
		}
		return out[i].Path < out[j].Path
	})
	return out
}

func archiveTimeFromName(name string) time.Time {
	const stampLen = len("20060102-150405.000")
	if len(name) <= stampLen || name[stampLen] != '-' {
		return time.Time{}
	}
	when, err := time.ParseInLocation("20060102-150405.000", name[:stampLen], time.UTC)
	if err != nil {
		return time.Time{}
	}
	return when
}

// loadMemory parses one fact file back into a Memory. It tolerates the minimal
// frontmatter render writes; a file without frontmatter still loads with its
// body and a name derived from the filename.
func loadMemory(path string) (Memory, bool) {
	b, err := fileencoding.ReadFileUTF8(path)
	if err != nil {
		return Memory{}, false
	}
	fm, body := splitFrontmatter(string(b))
	m := Memory{
		Name:        fm["name"],
		Title:       fm["title"],
		Description: fm["description"],
		Type:        NormalizeType(fm["type"]),
		Body:        strings.TrimSpace(body),
	}
	if m.Name == "" {
		m.Name = strings.TrimSuffix(filepath.Base(path), ".md")
	}
	return m, true
}

// splitFrontmatter is a thin wrapper; the real parser lives in
// internal/frontmatter.
func splitFrontmatter(s string) (map[string]string, string) {
	return frontmatter.Split(s)
}

// slugRe strips everything but Unicode letters and digits.
var slugRe = regexp.MustCompile(`[^\p{L}\p{N}]+`)

// slug normalises a name into a kebab-case, filesystem-safe stem. The stem is
// bounded so `<stem>.md` stays under the 255-byte filename component limit —
// a name distilled from a long title/description previously failed the write
// with ENAMETOOLONG. Names short enough to have ever been written are
// returned unchanged, so existing files keep resolving.
func slug(s string) string {
	stem := strings.Trim(slugRe.ReplaceAllString(strings.ToLower(strings.TrimSpace(s)), "-"), "-")
	return config.BoundFilenameComponent(stem, 255-len(".md"))
}

// oneLine collapses whitespace so a description can't break the single-line
// index or frontmatter format.
func oneLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// displayTitle is the index link label: the given title, or a de-kebabed name
// when none was supplied, so a bare slug never leaks into the index.
func displayTitle(title, name string) string {
	if t := oneLine(title); t != "" {
		return t
	}
	return strings.ReplaceAll(name, "-", " ")
}
