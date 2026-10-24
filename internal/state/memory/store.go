package memory

import (
	"cmp"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	fileencoding "reasonix/internal/base/fileutil/encoding"
	"reasonix/internal/base/frontmatter"
	"reasonix/internal/contract/config"
)

// Store is the scoped auto-memory store: project and global directories of
// one-fact-per-file Markdown notes, each with a MEMORY.md index.
// The model maintains it through the `remember` tool; the index loads into the
// cached system-prompt prefix at boot so the model always knows what it has
// saved, and reads individual facts on demand with the `memory` tool. The whole
// thing is plain files the user can edit by hand.
//
// Scope and type are independent: callers choose whether a fact belongs to the
// current project or every project, while Type only classifies its contents.
// List() and Index() merge both directories so every session sees the full set.
type Store struct {
	Dir               string // ...reasonix/projects/<slug>/memory
	GlobalDir         string // ...reasonix/memory/global (shared across projects)
	PinnedBudgetChars int    // user ceiling on pinned-body runes; 0 = none
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

// FactScope controls where an auto-memory fact is active. It is intentionally
// separate from Type: project feedback should not silently become global merely
// because it is classified as feedback.
type FactScope string

const (
	FactScopeProject FactScope = "project"
	FactScopeGlobal  FactScope = "global"
)

// NormalizeFactScope defaults to the current project. Global memory must be an
// explicit choice because it affects every workspace.
func NormalizeFactScope(s string) FactScope {
	if FactScope(strings.ToLower(strings.TrimSpace(s))) == FactScopeGlobal {
		return FactScopeGlobal
	}
	return FactScopeProject
}

// Memory is one stored fact.
type Memory struct {
	ID             string // immutable identity; Name may change without changing ID
	Revision       int    // monotonic content revision, starting at 1
	CreatedAt      time.Time
	UpdatedAt      time.Time
	Name           string // kebab-case slug; also the file stem (<name>.md)
	Title          string // human-readable index label; falls back to a de-kebabed Name
	Description    string // one-line summary used for the index and recall
	Type           Type
	Scope          FactScope  // project by default; global only when explicitly requested
	Activation     Activation // persisted choice; "" = unset, resolved by ResolveActivation
	Volatility     Volatility // how fast the fact ages; "" = unset, type default applies
	SubjectKey     string     // which question the fact answers (project.package_manager); one active value per scope+subject
	ExpiresAt      time.Time  // hard freshness boundary; zero = never expires
	LastVerifiedAt time.Time  // last explicit confirmation; renews the freshness clock
	Keywords       string     // search aliases (bilingual synonyms, related commands); recall-only, never rendered into the index
	Body           string     // the fact itself (Markdown)
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

// DirFor returns the directory for an explicit fact scope. When GlobalDir is
// unavailable, global writes fall back to Dir rather than being dropped.
func (s Store) DirFor(scope FactScope) string {
	if s.GlobalDir != "" && NormalizeFactScope(string(scope)) == FactScopeGlobal {
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

// Path returns the absolute file path a memory with the given name lives at.
// It checks GlobalDir first, then Dir, returning the first match. If no file
// exists yet, it returns the path in Dir (the default project scope).
func (s Store) Path(name string) (string, error) {
	_, path, ok, err := s.findActive(name)
	if err != nil {
		return "", err
	}
	if ok {
		return path, nil
	}
	ref := parseMemoryReference(name)
	stem := ref.name + ".md"
	if ref.qualified {
		p, err := safeJoin(s.DirFor(ref.scope), stem)
		if err != nil {
			return "", err
		}
		return p, nil
	}
	for _, dir := range s.dirs() {
		if dir == "" {
			continue
		}
		p, err := safeJoin(dir, stem)
		if err != nil {
			continue
		}
		if _, err := os.Stat(p); err == nil {
			return p, nil
		}
	}
	p, sjErr := safeJoin(s.Dir, stem)
	if sjErr != nil {
		return "", sjErr
	}
	return p, nil
}

// Save writes (or overwrites) a memory file and refreshes its MEMORY.md index
// line. It is the single mutation entry point — the `remember` tool, the desktop
// editor, and any future importer all go through here so the index never drifts
// from the files. Returns the path written.
func (s Store) Save(m Memory) (string, error) {
	result, err := s.SaveWithOptions(m, SaveOptions{})
	return result.Path, err
}

// Archive removes a memory from the active store and moves its file under
// .archive/ for traceability. A missing file is not an error; the goal state
// (not active) already holds. It returns the archive path, or "" when no file
// existed to archive.
// When both GlobalDir and Dir exist, it archives from every directory the
// memory appears in (handles migration duplicates).
func (s Store) Archive(name string) (string, error) {
	memoryStoreMutationMu.Lock()
	defer memoryStoreMutationMu.Unlock()
	release, err := s.lockMutations()
	if err != nil {
		return "", err
	}
	defer release()
	return s.archiveLocked(name)
}

func (s Store) archiveLocked(name string) (string, error) {
	if s.Dir == "" && s.GlobalDir == "" {
		return "", fmt.Errorf("memory store unavailable (no user config dir)")
	}
	ref := strings.TrimSpace(name)
	parsed := parseMemoryReference(ref)
	active, path, ok, err := s.findActive(ref)
	if err != nil {
		return "", err
	}
	if ok && ref == active.ID {
		return archiveMemoryInDir(filepath.Dir(path), active.Name)
	} else if ok && parsed.qualified {
		return archiveMemoryInDir(filepath.Dir(path), active.Name)
	} else if ok {
		name = active.Name
	} else if parsed.qualified {
		if parsed.name == "" {
			return "", fmt.Errorf("memory needs a name")
		}
		return archiveMemoryInDir(s.DirFor(parsed.scope), parsed.name)
	} else {
		p, repaired, err := s.repairArchivedRef(ref)
		if err != nil {
			return "", err
		}
		if repaired {
			return p, nil
		}
		name = slug(name)
	}
	if name == "" {
		return "", fmt.Errorf("memory needs a name")
	}
	var lastPath string
	for _, dir := range s.dirs() {
		if dir == "" {
			continue
		}
		lines, contains, err := indexLinesExceptIn(dir, name)
		if err != nil {
			return "", err
		}
		p, err := archiveInDir(dir, name, lines)
		if err != nil {
			return "", err
		}
		if p == "" && contains {
			if err := flushIndexIn(dir, lines); err != nil {
				return "", err
			}
		}
		if p != "" {
			lastPath = p
		}
	}
	return lastPath, nil
}

func archiveMemoryInDir(dir, name string) (string, error) {
	lines, contains, err := indexLinesExceptIn(dir, name)
	if err != nil {
		return "", err
	}
	path, err := archiveInDir(dir, name, lines)
	if err != nil {
		return "", err
	}
	if path == "" && contains {
		if err := flushIndexIn(dir, lines); err != nil {
			return "", err
		}
	}
	return path, nil
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

// repairArchivedRef reconciles a reference whose active file is already gone. It
// repairs only a stable id: a legacy name can match several historical archives,
// and picking the first one would be a guess. It drops the stale index line that
// still claims the fact is active, and only that removal counts as a hand-off.
func (s Store) repairArchivedRef(ref string) (string, bool, error) {
	for _, dir := range s.dirs() {
		if strings.TrimSpace(dir) == "" {
			continue
		}
		entries, err := readMemoryDirectory(filepath.Join(dir, ".archive"))
		if err != nil {
			return "", false, err
		}
		for _, entry := range entries {
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
				continue
			}
			path := filepath.Join(dir, ".archive", entry.Name())
			memory, ok, readErr := loadMemory(path)
			// Only the id can select an archive entry, and an unreadable file's
			// id is unknown; it is skipped here rather than guessed at. The
			// archive's own reads still fail loudly at the directory level.
			if readErr != nil || !ok || memory.ID != ref {
				continue
			}
			lines, contains, err := indexLinesExceptIn(dir, slug(memory.Name))
			if err != nil {
				return "", false, err
			}
			if !contains {
				continue
			}
			if err := flushIndexIn(dir, lines); err != nil {
				return "", false, err
			}
			return path, true, nil
		}
	}
	return "", false, nil
}

func archiveInDir(dir, name string, lines map[string]string) (string, error) {
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
	if err := flushIndexIn(dir, lines); err != nil {
		if back := renameMemoryFile(root, dest, file); back != nil {
			return "", fmt.Errorf("%w (and %q could not be moved back out of the archive: %w)", err, name, back)
		}
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

// indexLineRe matches a managed index line so reindex/Delete can target the line
// for one memory by its filename without disturbing the rest of a hand-edited
// MEMORY.md.
var indexLineRe = regexp.MustCompile(`(?m)^\s*-\s\[.+?\]\(([^)]+)\.md\)\s*—\s.*$`)

func renderIndexLine(name string, m Memory) string {
	marker := ""
	if ResolveActivation(m) == ActivationPinned {
		marker = " pinned" // the body already rides the prefix; no need to read it
	}
	return fmt.Sprintf("- [%s](%s.md) — [%s/%s%s] %s",
		displayTitle(m.Title, name), name,
		NormalizeFactScope(string(m.Scope)), NormalizeType(string(m.Type)), marker, oneLine(m.Description))
}

// List returns the saved memories parsed from their files, sorted by name. Used
// by `/memory` and the desktop memory panel. Reads from both GlobalDir and Dir,
// merging results. Files that fail to parse are skipped so one bad file never
// hides the rest.
func (s Store) List() ([]Memory, error) {
	if s.Dir == "" && s.GlobalDir == "" {
		return nil, nil
	}
	var out []Memory
	seen := map[string]bool{}
	for _, dir := range s.dirs() {
		if dir == "" {
			continue
		}
		entries, err := readMemoryDirectory(dir)
		if err != nil {
			return nil, err
		}
		for _, e := range entries {
			if e.IsDir() || e.Name() == indexFile || !strings.HasSuffix(e.Name(), ".md") {
				continue
			}
			// List keeps the legacy name-based merge and returns facts only, so
			// an unreadable file is skipped here exactly as before; ListActive is
			// what a surface uses to name it.
			if m, ok, readErr := loadMemory(filepath.Join(dir, e.Name())); readErr == nil && ok {
				if m.Scope == "" {
					m.Scope = s.scopeForDir(dir)
				}
				if !seen[m.Name] {
					out = append(out, m)
					seen[m.Name] = true
				}
			}
		}
	}
	slices.SortFunc(out, func(a, b Memory) int { return cmp.Compare(a.Name, b.Name) })
	return out, nil
}

// ListAll returns every readable active fact from both scopes without the
// legacy name-based deduplication performed by List. An unreadable fact file is
// not an error here — one bad file must not hide the rest — so a surface that
// has to name it reads ListActive instead.
func (s Store) ListAll() ([]Memory, error) {
	report, err := s.ListActive()
	return report.Facts, err
}

// PinnedBudgetOff is the unset pinned budget. Pinned bodies do cost prefix
// tokens every session, but how many are worth paying is the user's judgment:
// `/memory` reports the cost, and memory.pinned_budget_chars sets a ceiling
// for anyone who wants one.
const PinnedBudgetOff = 0

// pinnedGuidance snapshots explicitly pinned facts (plus legacy global
// user/feedback, which ResolveActivation keeps pinned for compatibility) for
// the stable session prefix, most recently updated first.
func (s Store) pinnedGuidance() ([]Memory, error) {
	var out []Memory
	for _, dir := range s.dirs() {
		if dir == "" {
			continue
		}
		entries, err := readMemoryDirectory(dir)
		if err != nil {
			return nil, err
		}
		for _, e := range entries {
			if e.IsDir() || e.Name() == indexFile || !strings.HasSuffix(e.Name(), ".md") {
				continue
			}
			// A pinned fact that cannot be read contributes nothing to the
			// stable prefix; omitting it is the only safe option here, since
			// sending its bytes is precisely what failed. /memory lists it.
			m, ok, readErr := loadMemory(filepath.Join(dir, e.Name()))
			if readErr != nil || !ok {
				continue
			}
			if m.Scope == "" {
				m.Scope = s.scopeForDir(dir)
			}
			if ResolveActivation(m) != ActivationPinned || strings.TrimSpace(m.Body) == "" {
				continue
			}
			// An explicit expiry is a hard boundary: the index and automatic
			// recall already drop the fact past it, so the prefix must not keep
			// sending the body.
			if memoryFreshness(m, time.Now()) == FreshnessExpired {
				continue
			}
			out = append(out, m)
		}
	}
	slices.SortFunc(out, func(a, b Memory) int {
		return cmp.Or(b.UpdatedAt.Compare(a.UpdatedAt), cmp.Compare(a.Name, b.Name))
	})
	return out, nil
}

// pinnedGuidanceForProject removes pinned guidance shadowed by an equivalent
// project fact before the stable session prefix is built. This makes the
// documented project-over-global rule deterministic on the first turn instead
// of depending on whether automatic recall happens to match the request.
func (s Store) pinnedGuidanceForProject() ([]Memory, error) {
	guidance, err := s.pinnedGuidance()
	if err != nil {
		return nil, err
	}
	if len(guidance) == 0 || s.Dir == "" {
		return guidance, nil
	}
	now := time.Now()
	projectKeys := map[string]bool{}
	facts, err := s.ListAll()
	if err != nil {
		return nil, err
	}
	for _, fact := range facts {
		if NormalizeFactScope(string(fact.Scope)) != FactScopeProject {
			continue
		}
		// An expired project fact sends nothing, so it cannot shadow the global
		// guidance it used to override.
		if memoryFreshness(fact, now) == FreshnessExpired {
			continue
		}
		for _, key := range recallIdentityKeys(fact) {
			if strings.HasSuffix(key, ":") {
				continue
			}
			projectKeys[key] = true
		}
	}
	if len(projectKeys) == 0 {
		return guidance, nil
	}
	out := guidance[:0]
	for _, fact := range guidance {
		// Project pinned facts always stay: the shadow rule only suppresses a
		// GLOBAL fact that an equivalent project fact overrides.
		shadowed := false
		if NormalizeFactScope(string(fact.Scope)) == FactScopeGlobal {
			for _, key := range recallIdentityKeys(fact) {
				if projectKeys[key] {
					shadowed = true
					break
				}
			}
		}
		if !shadowed {
			out = append(out, fact)
		}
	}
	return out, nil
}

// ListArchived returns archived memories parsed from .archive/, newest first.
// Archived files stay out of List() and the prompt index, so stale facts remain
// inspectable without being reused as active truth. Reads from both GlobalDir
// and Dir.
func (s Store) ListArchived() ([]ArchivedMemory, error) {
	if s.Dir == "" && s.GlobalDir == "" {
		return nil, nil
	}
	var out []ArchivedMemory
	for _, base := range s.dirs() {
		if base == "" {
			continue
		}
		dir := filepath.Join(base, ".archive")
		entries, err := readMemoryDirectory(dir)
		if err != nil {
			return nil, err
		}
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
				continue
			}
			path := filepath.Join(dir, e.Name())
			// An archive entry that cannot be read is skipped from the archive
			// listing; recover() reports it when that entry is the one named.
			m, ok, readErr := loadMemory(path)
			if readErr != nil || !ok {
				continue
			}
			if m.Scope == "" {
				m.Scope = s.scopeForDir(base)
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
	slices.SortFunc(out, func(a, b ArchivedMemory) int {
		return cmp.Or(b.ArchivedAt.Compare(a.ArchivedAt), cmp.Compare(a.Name, b.Name), cmp.Compare(a.Path, b.Path))
	})
	return out, nil
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

// loadMemory parses one fact file back into a Memory, deriving its body and a
// name from the filename when the frontmatter is minimal or absent. A file that
// is present but cannot be read is a FactReadError, never (zero, false): the
// two states have different consequences for a read, a listing, and a write.
// ok is false only for a file that is genuinely gone.
func loadMemory(path string) (Memory, bool, *FactReadError) {
	if err := fault("read-fact", path); err != nil {
		if os.IsNotExist(err) {
			return Memory{}, false, nil
		}
		return Memory{}, false, &FactReadError{Path: path, Err: err}
	}
	b, err := fileencoding.ReadFileUTF8(path)
	if err != nil {
		if os.IsNotExist(err) {
			// Deleted between the directory listing and this read: absence is
			// the truth. Windows also answers this way for a regular file where
			// a directory should be, which readMemoryDirectory already reports.
			return Memory{}, false, nil
		}
		return Memory{}, false, &FactReadError{Path: path, Err: err}
	}
	m := parseMemory(filepath.Base(path), b)
	if m.CreatedAt.IsZero() || m.UpdatedAt.IsZero() {
		if info, err := os.Stat(path); err == nil {
			if m.CreatedAt.IsZero() {
				m.CreatedAt = info.ModTime().UTC()
			}
			if m.UpdatedAt.IsZero() {
				m.UpdatedAt = info.ModTime().UTC()
			}
		}
	}
	return m, true, nil
}

func legacyIdentityScope(m Memory) FactScope {
	if m.Scope != "" {
		return NormalizeFactScope(string(m.Scope))
	}
	if m.Type == TypeUser || m.Type == TypeFeedback {
		return FactScopeGlobal
	}
	return FactScopeProject
}

func parsePositiveInt(value string) int {
	n, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil || n < 1 {
		return 0
	}
	return n
}

func parseMemoryTime(value string) time.Time {
	when, _ := time.Parse(time.RFC3339Nano, strings.TrimSpace(value))
	return when
}

func persistedFactType(fm map[string]string) Type {
	if t := Type(strings.ToLower(strings.TrimSpace(fm["fact_type"]))); validTypes[t] {
		return t
	}
	return NormalizeType(fm["type"])
}

func factScopeFromFrontmatter(s string) FactScope {
	switch FactScope(strings.ToLower(strings.TrimSpace(s))) {
	case FactScopeProject:
		return FactScopeProject
	case FactScopeGlobal:
		return FactScopeGlobal
	default:
		return ""
	}
}

func (s Store) scopeForDir(dir string) FactScope {
	if s.GlobalDir != "" && sameDir(dir, s.GlobalDir) {
		return FactScopeGlobal
	}
	return FactScopeProject
}

func (s Store) scopeForPath(path string) FactScope {
	return s.scopeForDir(filepath.Dir(path))
}

// splitFrontmatter is a thin wrapper; the real parser lives in
// internal/base/frontmatter.
func splitFrontmatter(s string) (map[string]string, string) {
	return frontmatter.SplitLegacy(s)
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
