package memory

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"reasonix/internal/fileutil"
)

type SaveOptions struct {
	ExpectedRevision        int
	RequireExpectedRevision bool
	RequireCreate           bool
	ClearExpiry             bool // drop an inherited expires_at instead of preserving it
}

type SaveResult struct {
	Path     string
	Memory   Memory
	Previous *Memory
}

type MigrationReport struct {
	Migrated int
}

var memoryStoreMutationMu sync.Mutex

func (s Store) MigrateV2() (MigrationReport, error) {
	memoryStoreMutationMu.Lock()
	defer memoryStoreMutationMu.Unlock()
	var report MigrationReport
	for _, dir := range s.dirs() {
		if strings.TrimSpace(dir) == "" {
			continue
		}
		info, err := os.Stat(dir)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return report, err
		}
		if !info.IsDir() {
			return report, fmt.Errorf("memory store path %q is not a directory", dir)
		}
		entries, err := os.ReadDir(dir)
		if err != nil {
			return report, err
		}
		for _, entry := range entries {
			if entry.IsDir() || entry.Name() == indexFile || !strings.HasSuffix(entry.Name(), ".md") {
				continue
			}
			path := filepath.Join(dir, entry.Name())
			raw, err := os.ReadFile(path)
			if err != nil {
				return report, err
			}
			frontmatter, _ := splitFrontmatter(string(raw))
			if strings.TrimSpace(frontmatter["id"]) != "" && parsePositiveInt(frontmatter["revision"]) > 0 {
				continue
			}
			memory, ok := loadMemory(path)
			if !ok {
				continue
			}
			memory.Name = slug(memory.Name)
			if memory.Scope == "" {
				memory.Scope = s.scopeForDir(dir)
			}
			if err := writeMemoryAtomic(path, []byte(render(memory, memory.Name)), 0o644); err != nil {
				return report, err
			}
			if err := reindexIn(dir, memory.Name, memory); err != nil {
				return report, err
			}
			report.Migrated++
		}
	}
	return report, nil
}

// inheritOnUpdate keeps the update-omittable fields of an existing revision:
// an update that leaves scope, activation, volatility, expiry, verification,
// or keywords empty preserves them, it does not clear them. ClearExpiry is
// the explicit exception — dropping a boundary must be a stated intent.
func inheritOnUpdate(m Memory, existing Memory, clearExpiry bool) Memory {
	if strings.TrimSpace(string(m.Scope)) == "" {
		m.Scope = existing.Scope
	}
	if NormalizeActivation(string(m.Activation)) == "" {
		m.Activation = existing.Activation
	}
	if NormalizeVolatility(string(m.Volatility)) == "" {
		m.Volatility = existing.Volatility
	}
	if NormalizeSubjectKey(m.SubjectKey) == "" {
		m.SubjectKey = existing.SubjectKey
	}
	if clearExpiry {
		m.ExpiresAt = time.Time{}
	} else if m.ExpiresAt.IsZero() {
		m.ExpiresAt = existing.ExpiresAt
	}
	if m.LastVerifiedAt.IsZero() {
		m.LastVerifiedAt = existing.LastVerifiedAt
	}
	if strings.TrimSpace(m.Keywords) == "" {
		m.Keywords = existing.Keywords
	}
	return m
}

// validateSave runs the cross-fact invariants once identity, scope, and
// inheritance are resolved: the pinned budget and subject uniqueness.
func (s Store) validateSave(m Memory) error {
	if err := s.validatePinnedBudget(m); err != nil {
		return err
	}
	return s.validateSubjectKey(m)
}

// validatePinnedBudget rejects a save that would push the total pinned-body
// runes over PinnedGuidanceBudgetChars. Legacy virtually-pinned guidance
// counts — it occupies the same prefix — so an over-budget store forces
// curation before anything new can be pinned.
func (s Store) validatePinnedBudget(m Memory) error {
	if ResolveActivation(m) != ActivationPinned {
		return nil
	}
	total := utf8.RuneCountInString(strings.TrimSpace(m.Body))
	for _, pinned := range s.pinnedGuidance() {
		if pinned.ID == m.ID || (m.ID == "" && pinned.Name == m.Name) {
			continue
		}
		total += utf8.RuneCountInString(strings.TrimSpace(pinned.Body))
	}
	if total <= PinnedGuidanceBudgetChars {
		return nil
	}
	return fmt.Errorf("pinning this fact would put pinned guidance at %d chars, over the %d budget: rules that must always hold belong in REASONIX.md/AGENTS.md instructions; unpin or consolidate existing pinned facts first", total, PinnedGuidanceBudgetChars)
}

func (s Store) SaveWithOptions(m Memory, opts SaveOptions) (SaveResult, error) {
	memoryStoreMutationMu.Lock()
	defer memoryStoreMutationMu.Unlock()

	inputID := strings.TrimSpace(m.ID)
	inputRef := parseMemoryReference(m.Name)
	if inputID == "" && inputRef.qualified && strings.TrimSpace(string(m.Scope)) != "" &&
		NormalizeFactScope(string(m.Scope)) != inputRef.scope {
		return SaveResult{}, fmt.Errorf("memory reference scope %q conflicts with explicit scope %q", inputRef.scope, m.Scope)
	}
	var existing Memory
	var existingPath string
	var exists bool
	if inputID != "" {
		existing, existingPath, exists = s.findActive(inputID)
		if !exists {
			return SaveResult{}, fmt.Errorf("memory id %q not found", m.ID)
		}
	} else if inputRef.raw != "" {
		existing, existingPath, exists = s.findActive(m.Name)
	}
	if opts.RequireExpectedRevision {
		actual := 0
		if exists {
			actual = existing.Revision
		}
		if actual != opts.ExpectedRevision {
			return SaveResult{}, fmt.Errorf("memory revision conflict: expected %d, found %d", opts.ExpectedRevision, actual)
		}
	}
	if opts.RequireCreate && exists {
		return SaveResult{}, fmt.Errorf("memory %q already exists; automatic writes are create-only", existing.Name)
	}

	if inputRef.raw == "" {
		if !exists {
			return SaveResult{}, fmt.Errorf("memory needs a name")
		}
		m.Name = existing.Name
	} else if exists && inputID == "" {
		// Name-based references identify an existing fact; renames require its
		// stable ID. This also prevents display references such as foo.md or
		// project/foo.md from becoming new slugs during an update.
		m.Name = existing.Name
	} else {
		m.Name = inputRef.name
	}
	m.Name = slug(m.Name)
	if m.Name == "" {
		return SaveResult{}, fmt.Errorf("memory name needs at least one letter or digit")
	}
	now := time.Now().UTC()
	if exists {
		m.ID, m.Revision, m.CreatedAt = existing.ID, existing.Revision+1, existing.CreatedAt
		m = inheritOnUpdate(m, existing, opts.ClearExpiry)
	} else {
		m.ID = newMemoryID(m.Name, now)
		m.Revision = 1
		m.CreatedAt = now
	}
	if m.CreatedAt.IsZero() {
		m.CreatedAt = now
	}
	m.UpdatedAt = now
	m.Type = NormalizeType(string(m.Type))
	if strings.TrimSpace(string(m.Scope)) == "" {
		if inputRef.qualified {
			m.Scope = inputRef.scope
		} else {
			m.Scope = FactScopeProject
		}
	} else {
		m.Scope = NormalizeFactScope(string(m.Scope))
	}
	if err := s.validateSave(m); err != nil {
		return SaveResult{}, err
	}

	dir := s.DirFor(m.Scope)
	if dir == "" {
		return SaveResult{}, fmt.Errorf("memory store unavailable (no user config dir)")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return SaveResult{}, err
	}
	if collision, _, ok := s.findActiveInDir(dir, m.Name); ok && (!exists || collision.ID != existing.ID) {
		return SaveResult{}, fmt.Errorf("memory name %q is already used by id %q", m.Name, collision.ID)
	}
	path, err := safeJoin(dir, m.Name+".md")
	if err != nil {
		return SaveResult{}, err
	}
	if exists {
		if err := snapshotMemoryRevision(existingPath, existing); err != nil {
			return SaveResult{}, err
		}
	}
	if err := writeMemoryAtomic(path, []byte(render(m, m.Name)), 0o644); err != nil {
		return SaveResult{}, err
	}
	if exists && cleanMemoryPath(existingPath) != cleanMemoryPath(path) {
		if err := os.Remove(existingPath); err != nil && !os.IsNotExist(err) {
			return SaveResult{}, err
		}
		oldDir := filepath.Dir(existingPath)
		if err := flushIndexIn(oldDir, indexLinesExceptIn(oldDir, existing.Name)); err != nil {
			return SaveResult{}, err
		}
	}
	if err := reindexIn(dir, m.Name, m); err != nil {
		return SaveResult{Path: path, Memory: m}, err
	}
	// Legacy unqualified name updates keep the previous single-active-copy
	// behavior. Stable IDs and scope-qualified references select one identity
	// exactly, so they must not remove a same-named fact in the other scope.
	if inputID == "" && !inputRef.qualified {
		for _, otherDir := range s.dirs() {
			if sameDir(otherDir, dir) {
				continue
			}
			if duplicate, _, ok := s.findActiveInDir(otherDir, m.Name); ok && duplicate.ID != m.ID {
				if _, err := archiveInDir(otherDir, duplicate.Name); err != nil {
					return SaveResult{}, err
				}
				if err := flushIndexIn(otherDir, indexLinesExceptIn(otherDir, duplicate.Name)); err != nil {
					return SaveResult{}, err
				}
			}
		}
	}

	result := SaveResult{Path: path, Memory: m}
	if exists {
		previous := existing
		result.Previous = &previous
	}
	return result, nil
}

func (s Store) Read(ref string) (Memory, bool) {
	memory, _, ok := s.findActive(ref)
	return memory, ok
}

func (s Store) findActive(ref string) (Memory, string, bool) {
	parsed := parseMemoryReference(ref)
	if parsed.raw == "" {
		return Memory{}, "", false
	}
	if parsed.qualified {
		return s.findActiveInDir(s.DirFor(parsed.scope), parsed.raw)
	}
	for _, v := range slices.Backward(s.dirs()) {
		dir := v
		if memory, path, ok := s.findActiveInDir(dir, parsed.raw); ok {
			return memory, path, true
		}
	}
	return Memory{}, "", false
}

func (s Store) findActiveInDir(dir, ref string) (Memory, string, bool) {
	if strings.TrimSpace(dir) == "" {
		return Memory{}, "", false
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return Memory{}, "", false
	}
	parsed := parseMemoryReference(ref)
	wantName := parsed.name
	for _, entry := range entries {
		if entry.IsDir() || entry.Name() == indexFile || !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		memory, ok := loadMemory(path)
		if !ok {
			continue
		}
		if memory.Scope == "" {
			memory.Scope = s.scopeForDir(dir)
		}
		if memory.ID == parsed.raw || slug(memory.Name) == wantName {
			memory.Name = slug(memory.Name)
			return memory, path, true
		}
	}
	return Memory{}, "", false
}

type memoryReference struct {
	raw       string
	name      string
	scope     FactScope
	qualified bool
}

// parseMemoryReference understands provider-visible references without ever
// treating them as filesystem paths. Memory facts are flat files, so only one
// fixed scope component plus one filename is accepted as a qualified form.
func parseMemoryReference(ref string) memoryReference {
	raw := strings.TrimSpace(ref)
	parsed := memoryReference{raw: raw, name: slug(strings.TrimSuffix(raw, ".md"))}
	for _, candidate := range []FactScope{FactScopeProject, FactScopeGlobal} {
		prefix := string(candidate) + "/"
		if !strings.HasPrefix(raw, prefix) {
			continue
		}
		name := strings.TrimPrefix(raw, prefix)
		if name == "" || strings.ContainsAny(name, `/\\`) {
			return parsed
		}
		parsed.name = slug(strings.TrimSuffix(name, ".md"))
		parsed.scope = candidate
		parsed.qualified = true
		return parsed
	}
	return parsed
}

func (s Store) Revisions(ref string) []Memory {
	active, _, ok := s.findActive(ref)
	if !ok {
		return nil
	}
	seen := map[int]bool{}
	var revisions []Memory
	for _, dir := range s.dirs() {
		revisionDir := filepath.Join(dir, ".revisions", active.ID)
		entries, err := os.ReadDir(revisionDir)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
				continue
			}
			memory, ok := loadMemory(filepath.Join(revisionDir, entry.Name()))
			if !ok || memory.ID != active.ID || seen[memory.Revision] {
				continue
			}
			seen[memory.Revision] = true
			revisions = append(revisions, memory)
		}
	}
	sort.Slice(revisions, func(i, j int) bool { return revisions[i].Revision > revisions[j].Revision })
	return revisions
}

func (s Store) Restore(ref string, revision int) (SaveResult, error) {
	active, ok := s.Read(ref)
	if !ok {
		return SaveResult{}, fmt.Errorf("memory %q not found", ref)
	}
	if revision == active.Revision {
		return SaveResult{Path: s.Path(active.Name), Memory: active}, nil
	}
	var target Memory
	found := false
	for _, candidate := range s.Revisions(active.ID) {
		if candidate.Revision == revision {
			target = candidate
			found = true
			break
		}
	}
	if !found {
		return SaveResult{}, fmt.Errorf("memory %q revision %d not found", active.ID, revision)
	}
	target.ID = active.ID
	return s.SaveWithOptions(target, SaveOptions{ExpectedRevision: active.Revision, RequireExpectedRevision: true})
}

// RestoreArchived recovers one archive entry as a new active revision. The
// archive path must be an entry currently owned by this Store. Recovery never
// overwrites an active identity or slug, and the archived state becomes an
// immutable revision snapshot before the new active file is created.
func (s Store) RestoreArchived(archivePath string) (SaveResult, error) {
	memoryStoreMutationMu.Lock()
	defer memoryStoreMutationMu.Unlock()

	archivePath = cleanMemoryPath(strings.TrimSpace(archivePath))
	archived, base, ok := s.findArchivedByPath(archivePath)
	if !ok {
		return SaveResult{}, fmt.Errorf("archived memory not found")
	}
	if active, _, exists := s.findActive(archived.ID); exists {
		return SaveResult{}, fmt.Errorf("memory id %q is already active as %q", archived.ID, active.Name)
	}
	if active, _, exists := s.findActive(archived.Name); exists {
		return SaveResult{}, fmt.Errorf("memory name %q is already active as id %q", archived.Name, active.ID)
	}

	if err := snapshotMemoryRevisionInDir(base, archivePath, archived); err != nil {
		return SaveResult{}, err
	}
	now := time.Now().UTC()
	restored := archived
	restored.Scope = s.scopeForDir(base)
	restored.Revision = s.maxKnownRevision(archived.ID) + 1
	if restored.Revision <= archived.Revision {
		restored.Revision = archived.Revision + 1
	}
	if restored.CreatedAt.IsZero() {
		restored.CreatedAt = now
	}
	restored.UpdatedAt = now
	path, err := safeJoin(base, restored.Name+".md")
	if err != nil {
		return SaveResult{}, err
	}
	if err := writeMemoryCreate(path, []byte(render(restored, restored.Name)), 0o644); err != nil {
		if os.IsExist(err) {
			return SaveResult{}, fmt.Errorf("memory name %q is already active", restored.Name)
		}
		return SaveResult{}, err
	}
	if err := reindexIn(base, restored.Name, restored); err != nil {
		return SaveResult{Path: path, Memory: restored}, err
	}
	if err := os.Remove(archivePath); err != nil && !os.IsNotExist(err) {
		return SaveResult{Path: path, Memory: restored}, err
	}
	return SaveResult{Path: path, Memory: restored}, nil
}

func (s Store) findArchivedByPath(want string) (Memory, string, bool) {
	for _, base := range s.dirs() {
		if strings.TrimSpace(base) == "" {
			continue
		}
		dir := filepath.Join(base, ".archive")
		info, err := os.Lstat(dir)
		if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			continue
		}
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
				continue
			}
			path, err := safeJoin(dir, entry.Name())
			if err != nil || cleanMemoryPath(path) != want {
				continue
			}
			info, err := os.Lstat(path)
			if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
				return Memory{}, "", false
			}
			archived, ok := loadMemory(path)
			if !ok {
				return Memory{}, "", false
			}
			if archived.Scope == "" {
				archived.Scope = s.scopeForDir(base)
			}
			archived.Name = slug(archived.Name)
			return archived, base, true
		}
	}
	return Memory{}, "", false
}

func (s Store) maxKnownRevision(id string) int {
	maxRevision := 0
	for _, base := range s.dirs() {
		if strings.TrimSpace(base) == "" {
			continue
		}
		for _, dir := range []string{filepath.Join(base, ".archive"), filepath.Join(base, ".revisions", id)} {
			entries, err := os.ReadDir(dir)
			if err != nil {
				continue
			}
			for _, entry := range entries {
				if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
					continue
				}
				path := filepath.Join(dir, entry.Name())
				info, err := os.Lstat(path)
				if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
					continue
				}
				candidate, ok := loadMemory(path)
				if ok && candidate.ID == id && candidate.Revision > maxRevision {
					maxRevision = candidate.Revision
				}
			}
		}
	}
	return maxRevision
}

func snapshotMemoryRevision(path string, memory Memory) error {
	return snapshotMemoryRevisionInDir(filepath.Dir(path), path, memory)
}

func snapshotMemoryRevisionInDir(base, path string, memory Memory) error {
	if memory.ID == "" || memory.Revision < 1 {
		return nil
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	dir := filepath.Join(base, ".revisions", memory.ID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	name := fmt.Sprintf("%09d.md", memory.Revision)
	return writeMemoryAtomic(filepath.Join(dir, name), b, 0o644)
}

// writeMemoryAtomic publishes a fact file through the shared crash-safe
// writer (temp + fsync + replace), creating the parent directory on demand.
func writeMemoryAtomic(path string, data []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return fileutil.AtomicWriteFile(path, data, mode)
}

// writeMemoryCreate publishes a fact file only when path is still absent; a
// concurrent creator wins. The shared writer stages a complete temp file, so
// a crash can never leave a partial fact where active truth lives.
func writeMemoryCreate(path string, data []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return fileutil.AtomicCreateFile(path, data, mode)
}

func newMemoryID(name string, now time.Time) string {
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err == nil {
		return "mem-" + hex.EncodeToString(raw)
	}
	sum := sha256.Sum256([]byte(name + "\x00" + strconv.FormatInt(now.UnixNano(), 10)))
	return "mem-" + hex.EncodeToString(sum[:16])
}

func legacyMemoryID(name string, scope FactScope) string {
	sum := sha256.Sum256([]byte("reasonix-memory-v2\x00" + string(NormalizeFactScope(string(scope))) + "\x00" + slug(name)))
	return "legacy-" + hex.EncodeToString(sum[:12])
}

func cleanMemoryPath(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		return filepath.Clean(path)
	}
	return filepath.Clean(abs)
}
