package memory

import (
	"fmt"
	"path/filepath"
	"strings"

	fileencoding "reasonix/internal/base/fileutil/encoding"
)

// parseMemory turns one fact file's UTF-8 bytes into a Memory, tolerating the
// minimal frontmatter render writes; a file without frontmatter still loads
// with its body and a name derived from the filename.
func parseMemory(name string, b []byte) Memory {
	fm, body := splitFrontmatter(string(b))
	m := Memory{
		ID:             fm["id"],
		Revision:       parsePositiveInt(fm["revision"]),
		CreatedAt:      parseMemoryTime(fm["created_at"]),
		UpdatedAt:      parseMemoryTime(fm["updated_at"]),
		Name:           fm["name"],
		Title:          fm["title"],
		Description:    fm["description"],
		Keywords:       fm["keywords"],
		Galaxy:         NormalizeGalaxy(fm["galaxy"]),
		Importance:     NormalizeImportance(parsePositiveInt(fm["importance"])),
		Activation:     NormalizeActivation(fm["activation"]),
		Volatility:     NormalizeVolatility(fm["volatility"]),
		SubjectKey:     NormalizeSubjectKey(fm["subject_key"]),
		ExpiresAt:      parseMemoryTime(fm["expires_at"]),
		LastVerifiedAt: parseMemoryTime(fm["last_verified_at"]),
		Type:           persistedFactType(fm),
		Scope:          factScopeFromFrontmatter(fm["scope"]),
		Body:           strings.TrimSpace(body),
	}
	if m.Name == "" {
		m.Name = strings.TrimSuffix(filepath.Base(name), ".md")
	}
	if m.ID == "" {
		m.ID = legacyMemoryID(m.Name, legacyIdentityScope(m))
	}
	if m.Revision <= 0 {
		m.Revision = 1
	}
	return m
}

// memoryRevisionConflict is the one spelling of a write refused because the
// revision it expected is not the one on disk.
func memoryRevisionConflict(expected, found int) error {
	return fmt.Errorf("memory revision conflict: expected %d, found %d", expected, found)
}

// snapshotRevisionConflict reports the conflict when raw no longer carries the
// id and revision memory names: another process may have published a newer
// revision over it, or archived the fact and created another under the name.
func snapshotRevisionConflict(path string, raw []byte, memory Memory) error {
	found := parseMemory(filepath.Base(path), fileencoding.DecodeToUTF8(raw))
	if found.ID == memory.ID && found.Revision == memory.Revision {
		return nil
	}
	return memoryRevisionConflict(memory.Revision, found.Revision)
}
