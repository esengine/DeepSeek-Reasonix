package memory

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"reasonix/internal/frontmatter"
)

// Store is the per-project auto-memory: a directory of one-fact-per-file
// Markdown notes with frontmatter, plus a MEMORY.md index of one line per fact.
// The model maintains it through the `remember` tool; the index loads into the
// cached system-prompt prefix at boot so the model always knows what it has
// saved, and reads individual facts on demand with read_file. The whole thing is
// plain files the user can edit by hand.
type Store struct {
	Dir string // ...reasonix/projects/<slug>/memory
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
// the user config root, e.g. ~/.config/reasonix/projects/-Users-me-proj/memory.
// A "" userDir (config dir unresolvable) yields a zero Store, which all methods
// treat as a disabled no-op.
func StoreFor(userDir, cwd string) Store {
	if userDir == "" {
		return Store{}
	}
	return Store{Dir: filepath.Join(userDir, "projects", slugify(absOf(cwd)), "memory")}
}

// indexFile is the human-readable index of saved memories.
const indexFile = "MEMORY.md"

// slugify turns an absolute project path into a single filesystem-safe segment,
// matching the auto-memory convention (path separators → '-'), e.g.
// "/Users/me/proj" → "-Users-me-proj".
func slugify(absPath string) string {
	r := strings.NewReplacer(string(os.PathSeparator), "-", "/", "-", "\\", "-", ":", "-")
	return r.Replace(absPath)
}

// Index returns the MEMORY.md contents (the per-line index of saved memories),
// or "" if there are none yet. This is what loads into the cached prefix.
func (s Store) Index() string {
	if s.Dir == "" {
		return ""
	}
	b, err := os.ReadFile(filepath.Join(s.Dir, indexFile))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

// Path returns the absolute file path a memory with the given name lives at.
func (s Store) Path(name string) string {
	path, _ := safeJoin(s.Dir, slug(name)+".md")
	return path
}

// Save writes (or overwrites) a memory file and refreshes its MEMORY.md index
// line. It is the single mutation entry point — the `remember` tool, the desktop
// editor, and any future importer all go through here so the index never drifts
// from the files. Returns the path written.
func (s Store) Save(m Memory) (string, error) {
	if s.Dir == "" {
		return "", fmt.Errorf("memory store unavailable (no user config dir)")
	}
	name := slug(m.Name)
	if name == "" {
		return "", fmt.Errorf("memory needs a name")
	}
	if err := os.MkdirAll(s.Dir, 0o755); err != nil {
		return "", err
	}
	path := filepath.Join(s.Dir, name+".md")
	if err := os.WriteFile(path, []byte(render(m, name)), 0o644); err != nil {
		return "", err
	}
	if err := s.reindex(name, m); err != nil {
		return path, err
	}
	return path, nil
}

// Archive removes a memory from the active store and moves its file under
// .archive/ for traceability. A missing file is not an error; the goal state
// (not active) already holds. It returns the archive path, or "" when no file
// existed to archive.
func (s Store) Archive(name string) (string, error) {
	if s.Dir == "" {
		return "", fmt.Errorf("memory store unavailable (no user config dir)")
	}
	name = slug(name)
	if name == "" {
		return "", fmt.Errorf("memory needs a name")
	}
	path, err := s.archiveMemoryFile(name)
	if err != nil {
		return "", err
	}
	return path, s.flushIndex(s.indexLinesExcept(name))
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

func (s Store) archiveMemoryFile(name string) (string, error) {
	if s.Dir == "" {
		return "", fmt.Errorf("memory store unavailable (no user config dir)")
	}
	root, err := os.OpenRoot(s.Dir)
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
	out, err := safeJoin(s.Dir, dest)
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

// render serializes a memory to frontmatter + body. The frontmatter mirrors the
// auto-memory shape (name / description / metadata.type) so the files are
// interchangeable with that ecosystem and re-readable by loadMemory.
func render(m Memory, name string) string {
	var b strings.Builder
	b.WriteString("---\n")
	b.WriteString("name: " + name + "\n")
	if t := oneLine(m.Title); t != "" {
		b.WriteString("title: " + t + "\n")
	}
	b.WriteString("description: " + oneLine(m.Description) + "\n")
	b.WriteString("metadata:\n")
	b.WriteString("  type: " + string(NormalizeType(string(m.Type))) + "\n")
	b.WriteString("---\n\n")
	b.WriteString(strings.TrimSpace(m.Body))
	b.WriteString("\n")
	return b.String()
}

// indexLineRe matches a managed index line so reindex/Delete can target the line
// for one memory by its filename without disturbing the rest of a hand-edited
// MEMORY.md.
var indexLineRe = regexp.MustCompile(`\]\(([^)]+)\.md\)`)

// indexLinesExcept returns the managed MEMORY.md lines keyed by filename stem,
// dropping the entry for name (a missing index → empty map).
func (s Store) indexLinesExcept(name string) map[string]string {
	existing, _ := os.ReadFile(filepath.Join(s.Dir, indexFile))
	keep := map[string]string{}
	for _, line := range strings.Split(string(existing), "\n") {
		if mt := indexLineRe.FindStringSubmatch(line); mt != nil && mt[1] != name {
			keep[mt[1]] = strings.TrimRight(line, "\r")
		}
	}
	return keep
}

// flushIndex rewrites MEMORY.md from the managed lines, sorted by filename.
func (s Store) flushIndex(lines map[string]string) error {
	names := make([]string, 0, len(lines))
	for n := range lines {
		names = append(names, n)
	}
	sort.Strings(names)

	var b strings.Builder
	b.WriteString("# Memory\n\n")
	for _, n := range names {
		b.WriteString(lines[n])
		b.WriteString("\n")
	}
	return os.WriteFile(filepath.Join(s.Dir, indexFile), []byte(b.String()), 0o644)
}

// reindex rewrites the MEMORY.md line for name, preserving every other managed
// line. The line is "- [<title>](<name>.md) — <description>"; title falls back
// to a de-kebabed name so the index reads as a label, never a bare slug.
func (s Store) reindex(name string, m Memory) error {
	lines := s.indexLinesExcept(name)
	lines[name] = fmt.Sprintf("- [%s](%s.md) — %s", displayTitle(m.Title, name), name, oneLine(m.Description))
	return s.flushIndex(lines)
}

// List returns the saved memories parsed from their files, sorted by name. Used
// by `/memory` and the desktop memory panel. Files that fail to parse are
// skipped so one bad file never hides the rest.
func (s Store) List() []Memory {
	if s.Dir == "" {
		return nil
	}
	entries, err := os.ReadDir(s.Dir)
	if err != nil {
		return nil
	}
	var out []Memory
	for _, e := range entries {
		if e.IsDir() || e.Name() == indexFile || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		if m, ok := loadMemory(filepath.Join(s.Dir, e.Name())); ok {
			out = append(out, m)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// ListArchived returns archived memories parsed from .archive/, newest first.
// Archived files stay out of List() and the prompt index, so stale facts remain
// inspectable without being reused as active truth.
func (s Store) ListArchived() []ArchivedMemory {
	if s.Dir == "" {
		return nil
	}
	dir := filepath.Join(s.Dir, ".archive")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []ArchivedMemory
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
	b, err := os.ReadFile(path)
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

// slugRe strips everything but lowercase alphanumerics and dashes.
var slugRe = regexp.MustCompile(`[^a-z0-9]+`)

// slug normalises a name into a kebab-case, filesystem-safe stem.
func slug(s string) string {
	return strings.Trim(slugRe.ReplaceAllString(strings.ToLower(strings.TrimSpace(s)), "-"), "-")
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
