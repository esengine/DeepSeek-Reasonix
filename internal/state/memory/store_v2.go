package memory

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"reasonix/internal/base/fileutil"
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
	// Unreadable names each fact the pass could not read, by basename, sorted.
	// Those facts were skipped, not deleted.
	Unreadable []string
}

// memoryStoreMutationMu serializes mutations inside this process only, and is
// always taken before the cross-process lock (lockMutations), never after it;
// other processes are excluded by that lock, so both layers share one order.
var memoryStoreMutationMu sync.Mutex

func (s Store) MigrateV2() (MigrationReport, error) {
	return s.MigrateV2WithVersion("dev")
}

func (s Store) MigrateV2WithVersion(version string) (report MigrationReport, err error) {
	memoryStoreMutationMu.Lock()
	defer memoryStoreMutationMu.Unlock()
	release, err := s.lockMutations()
	if err != nil {
		return MigrationReport{}, err
	}
	defer release()
	// The caller receives this report even when a later directory fails, so the
	// names stay sorted on every return path.
	defer func() { slices.Sort(report.Unreadable) }()
	for _, dir := range s.dirs() {
		if strings.TrimSpace(dir) == "" {
			continue
		}
		entries, err := readMemoryDirectory(dir)
		if err != nil {
			return report, err
		}
		// A migration rewrites facts and then the index. A deterministic read
		// fault is republished from the facts instead; a transient one still
		// returns, because the existing bytes may still be good.
		if _, err := readIndexIn(dir); err != nil {
			if err := s.repairIndexIn(dir, entries, err); err != nil {
				return report, err
			}
		}
		for _, entry := range entries {
			if entry.IsDir() || entry.Name() == indexFile || !strings.HasSuffix(entry.Name(), ".md") {
				continue
			}
			path := filepath.Join(dir, entry.Name())
			raw, err := os.ReadFile(path)
			if err != nil {
				// A fact this pass cannot read is skipped, not fatal: the rest
				// of the store still migrates, and the caller learns the name.
				report.Unreadable = append(report.Unreadable, entry.Name())
				continue
			}
			frontmatter, _ := splitFrontmatter(string(raw))
			if strings.TrimSpace(frontmatter["id"]) != "" && parsePositiveInt(frontmatter["revision"]) > 0 {
				// A previous run can have rewritten the fact and then failed to
				// publish the index, which leaves a migrated fact with a stale
				// line. Repairing it is what makes the retry converge.
				if migrated, ok, readErr := loadMemory(path); readErr == nil && ok {
					migrated.Name = slug(migrated.Name)
					if err := reindexIn(dir, migrated.Name, migrated); err != nil {
						return report, err
					}
				}
				continue
			}
			// Unreadable after a successful raw read is a race; skip the rewrite
			// and let the next migration pass see the file again.
			memory, ok, readErr := loadMemory(path)
			if readErr != nil || !ok {
				continue
			}
			memory.Name = slug(memory.Name)
			if memory.Scope == "" {
				memory.Scope = s.scopeForDir(dir)
			}
			if err := backupLegacyMemory(dir, entry.Name(), raw, version); err != nil {
				return report, err
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

// ErrPinnedBudget is the identity of a save refused because pinned guidance would
// exceed the configured budget. Its text is the setting the message already names,
// so wrapping it leaves the user-visible message byte-identical.
var ErrPinnedBudget = errors.New("memory.pinned_budget_chars")

// ErrNameActiveInTwoScopes is the identity of a save refused because an
// unqualified name resolves to a fact in more than one scope, so there is no
// single identity to update.
var ErrNameActiveInTwoScopes = errors.New("memory.name_active_in_two_scopes")

// validatePinnedBudget rejects a save that would push the total pinned-body
// runes over the user's configured ceiling. Legacy virtually-pinned guidance
// counts — it occupies the same prefix. Unset (the default) means no ceiling.
func (s Store) validatePinnedBudget(m Memory) error {
	if s.PinnedBudgetChars <= PinnedBudgetOff || ResolveActivation(m) != ActivationPinned {
		return nil
	}
	total := utf8.RuneCountInString(strings.TrimSpace(m.Body))
	guidance, err := s.pinnedGuidance()
	if err != nil {
		return err
	}
	for _, pinned := range guidance {
		if pinned.ID == m.ID || (m.ID == "" && pinned.Name == m.Name) {
			continue
		}
		total += utf8.RuneCountInString(strings.TrimSpace(pinned.Body))
	}
	if total <= s.PinnedBudgetChars {
		return nil
	}
	return fmt.Errorf("pinning this fact would put pinned guidance at %d chars, over the %d budget you configured (%w): unpin or consolidate existing pinned facts, raise the budget, or move rules that must always hold into REASONIX.md/AGENTS.md instructions", total, s.PinnedBudgetChars, ErrPinnedBudget)
}

func (s Store) SaveWithOptions(m Memory, opts SaveOptions) (SaveResult, error) {
	memoryStoreMutationMu.Lock()
	defer memoryStoreMutationMu.Unlock()
	release, err := s.lockMutations()
	if err != nil {
		return SaveResult{}, err
	}
	defer release()

	inputID := strings.TrimSpace(m.ID)
	inputRef := parseMemoryReference(m.Name)
	if inputID == "" && inputRef.qualified && strings.TrimSpace(string(m.Scope)) != "" &&
		NormalizeFactScope(string(m.Scope)) != inputRef.scope {
		return SaveResult{}, fmt.Errorf("memory reference scope %q conflicts with explicit scope %q", inputRef.scope, m.Scope)
	}
	existing, existingPath, exists, err := s.findSaveTarget(inputID, m.Name)
	if err != nil {
		return SaveResult{}, err
	}
	if opts.RequireExpectedRevision {
		actual := 0
		if exists {
			actual = existing.Revision
		}
		if actual != opts.ExpectedRevision {
			return SaveResult{}, memoryRevisionConflict(opts.ExpectedRevision, actual)
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
	if collision, _, ok, err := s.findActiveInDir(dir, m.Name); err != nil {
		return SaveResult{}, err
	} else if ok && (!exists || collision.ID != existing.ID) {
		return SaveResult{}, fmt.Errorf("memory name %q is already used by id %q", m.Name, collision.ID)
	}
	// An unqualified name that is active in another scope has no single identity
	// to update: archiving the other copy would remove a fact the caller never
	// named, so the collision is reported instead.
	if inputID == "" && !inputRef.qualified {
		for _, otherDir := range s.dirs() {
			if sameDir(otherDir, dir) {
				continue
			}
			duplicate, _, ok, err := s.findActiveInDir(otherDir, m.Name)
			if err != nil {
				return SaveResult{}, err
			}
			if !ok || duplicate.ID == m.ID {
				continue
			}
			scope := NormalizeFactScope(string(m.Scope))
			return SaveResult{}, fmt.Errorf("memory name %q is active in two scopes: id %q (scope %s) and id %q (scope %s) (%w); update the fact you meant by its stable id, or save with a scope-qualified name such as %q",
				m.Name, m.ID, scope, duplicate.ID, NormalizeFactScope(string(duplicate.Scope)), ErrNameActiveInTwoScopes,
				string(scope)+"/"+m.Name)
		}
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
	if err := s.publishRename(dir, path, existingPath, existing, m, exists); err != nil {
		return SaveResult{}, err
	}
	result := SaveResult{Path: path, Memory: m}
	if exists {
		previous := existing
		result.Previous = &previous
	}
	return result, nil
}

// undoPlainWrite puts a fact file back the way it was before a save whose index
// publication failed: a new fact is removed, an update is rewritten from the
// bytes and mode that were on disk, so nothing the user wrote is normalised.
func undoPlainWrite(path string, preimage []byte, mode os.FileMode, exists bool) error {
	if !exists {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	return writeMemoryAtomic(path, preimage, mode)
}

// publishRename retires the old name before publishing the new one, so no
// failure can leave one id with two active names; steps before publication are
// undone from the retired copy.
func (s Store) publishRename(dir, path, existingPath string, existing, m Memory, exists bool) error {
	if !exists || cleanMemoryPath(existingPath) == cleanMemoryPath(path) {
		var preimage []byte
		mode := os.FileMode(0o644)
		if exists {
			raw, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			preimage = raw
			if info, err := os.Stat(path); err == nil {
				mode = info.Mode().Perm()
			}
		}
		if err := writeMemoryAtomic(path, []byte(render(m, m.Name)), 0o644); err != nil {
			return err
		}
		if err := reindexIn(dir, m.Name, m); err != nil {
			if undone := undoPlainWrite(path, preimage, mode, exists); undone != nil {
				return fmt.Errorf("%w (and the saved %q could not be withdrawn: %w)", err, m.Name, undone)
			}
			return err
		}
		return nil
	}
	srcDir := filepath.Dir(existingPath)
	srcRoot, err := os.OpenRoot(srcDir)
	if err != nil {
		return err
	}
	defer srcRoot.Close()
	dstRoot, err := os.OpenRoot(dir)
	if err != nil {
		return err
	}
	defer dstRoot.Close()
	stamp := time.Now().UnixNano()
	stash := fmt.Sprintf(".retired-%s-%d", existing.Name, stamp)
	staged := fmt.Sprintf(".staged-%s-%d", m.Name, stamp)
	if err := writeMemoryAtomic(filepath.Join(dir, staged), []byte(render(m, m.Name)), 0o644); err != nil {
		return err
	}
	existingRel, pathRel := filepath.Base(existingPath), filepath.Base(path)
	undo := func(cause error) error {
		if err := renameMemoryFile(srcRoot, stash, existingRel); err != nil {
			return fmt.Errorf("%w (and the retired %q could not be put back: %w)", cause, existing.Name, err)
		}
		if err := reindexIn(srcDir, existing.Name, existing); err != nil {
			return fmt.Errorf("%w (and %q could not be restored in the index: %w)", cause, existing.Name, err)
		}
		_ = dstRoot.Remove(staged)
		return cause
	}
	if err := renameMemoryFile(srcRoot, existingRel, stash); err != nil {
		_ = dstRoot.Remove(staged)
		return err
	}
	lines, _, err := indexLinesExceptIn(srcDir, existing.Name)
	if err != nil {
		return undo(err)
	}
	if err := flushIndexIn(srcDir, lines); err != nil {
		return undo(err)
	}
	if err := renameMemoryFile(dstRoot, staged, pathRel); err != nil {
		return undo(err)
	}
	if err := reindexIn(dir, m.Name, m); err != nil {
		rmErr := fault("remove", pathRel)
		if rmErr == nil {
			rmErr = dstRoot.Remove(pathRel)
		}
		if rmErr != nil {
			// The new file cannot be withdrawn either, so finish forwards:
			// leaving one id with two live identities is a state the caller
			// cannot act on.
			if again := reindexIn(dir, m.Name, m); again != nil {
				return fmt.Errorf("%w (and %q could not be withdrawn: %w; retrying the index also failed: %w)", err, m.Name, rmErr, again)
			}
			return nil
		}
		return undo(err)
	}
	// Past this point the save is committed: the retired copy is a hidden,
	// non-.md name, so failing to remove it must not turn a committed save into
	// an error the caller would retry.
	if err := fault("remove", stash); err == nil {
		_ = srcRoot.Remove(stash)
	}
	return nil
}

func (s Store) Restore(ref string, revision int) (SaveResult, error) {
	active, path, ok, err := s.findActive(ref)
	if err != nil {
		return SaveResult{}, err
	}
	if !ok {
		return SaveResult{}, fmt.Errorf("memory %q not found", ref)
	}
	if revision == active.Revision {
		return SaveResult{Path: path, Memory: active}, nil
	}
	var target Memory
	found := false
	revisions, err := s.Revisions(active.ID)
	if err != nil {
		return SaveResult{}, err
	}
	for _, candidate := range revisions {
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
	release, err := s.lockMutations()
	if err != nil {
		return SaveResult{}, err
	}
	defer release()

	// A restore killed after it staged its entry leaves the entry hidden. The
	// sweep resolves it by id before this call stages anything: an active id
	// means the dead attempt published, an absent one means it never did.
	if err := s.sweepRestoreStaging(); err != nil {
		return SaveResult{}, err
	}
	archivePath = cleanMemoryPath(strings.TrimSpace(archivePath))
	archived, base, ok, err := s.findArchivedByPath(archivePath)
	if err != nil {
		return SaveResult{}, err
	}
	if !ok {
		return SaveResult{}, fmt.Errorf("archived memory not found")
	}
	if active, _, exists, err := s.findActive(archived.ID); err != nil {
		return SaveResult{}, err
	} else if exists {
		return SaveResult{}, fmt.Errorf("memory id %q is already active as %q", archived.ID, active.Name)
	}
	if active, _, exists, err := s.findActive(archived.Name); err != nil {
		return SaveResult{}, err
	} else if exists {
		return SaveResult{}, fmt.Errorf("memory name %q is already active as id %q", archived.Name, active.ID)
	}

	restored := archived
	restored.Scope = s.scopeForDir(base)
	// Recovery is a write: a subject key another active fact now holds, or a
	// pinned body over the budget, is refused here exactly as the save path
	// refuses it, so recovery cannot leave the store in a state save rejects.
	if err := s.validateSave(restored); err != nil {
		return SaveResult{}, err
	}
	// The active file must not be written while the index is unreadable: a
	// restore that dies mid-way cannot be retried — the identity guard above
	// would then report the half-written file as an already-active collision.
	if _, err := readIndexIn(base); err != nil {
		return SaveResult{}, err
	}
	maxRevision, err := s.maxKnownRevision(archived.ID)
	if err != nil {
		return SaveResult{}, err
	}
	if err := snapshotMemoryRevisionInDir(base, archivePath, archived); err != nil {
		return SaveResult{}, err
	}
	now := time.Now().UTC()
	restored.Revision = maxRevision + 1
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
	// The archive entry is hidden before the active file exists, so a crash
	// between the two can never leave the id listed as active and archived.
	stagedPath, err := stageArchivedEntry(base, archivePath, archived.ID)
	if err != nil {
		return SaveResult{}, err
	}
	if err := publishRestored(base, archivePath, path, stagedPath, restored); err != nil {
		return SaveResult{}, err
	}
	return SaveResult{Path: path, Memory: restored}, nil
}

func snapshotMemoryRevision(path string, memory Memory) error {
	return snapshotMemoryRevisionInDir(filepath.Dir(path), path, memory)
}

func snapshotMemoryRevisionInDir(base, path string, memory Memory) error {
	if memory.ID == "" || memory.Revision < 1 {
		return nil
	}
	if err := fault("read-file", path); err != nil {
		return err
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	// The bytes must still be this id at this revision — another process may
	// have published over them since the caller read the fact.
	if err := snapshotRevisionConflict(path, b, memory); err != nil {
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

func renameMemoryFile(root *os.Root, path, dest string) error {
	if err := fault("rename", path); err != nil {
		return err
	}
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
