package memory

import (
	"crypto/sha256"
	"os"
	"strings"
)

// FactStamp is the content identity of one fact file: the canonical path it was
// delivered from and a digest of the bytes there. There is deliberately no size
// and no modification time — an external rewrite can hold both still, and
// freshness must never be inferred from a stat.
type FactStamp struct {
	Path   string
	Digest [sha256.Size]byte
}

// StampOfFact digests the one file an active fact lives at, on the path its own
// scope and slug name. It reads that single file and never lists a directory; ok
// is false when no readable regular file is there (deleted, or a stem that does
// not match the name), which leaves the caller the identity read.
func (s Store) StampOfFact(fact Memory) (FactStamp, bool) {
	name := slug(fact.Name)
	if name == "" {
		return FactStamp{}, false
	}
	path, err := safeJoin(s.DirFor(NormalizeFactScope(string(fact.Scope))), name+".md")
	if err != nil {
		return FactStamp{}, false
	}
	return StampOfPath(path)
}

// StampOfPath digests a fact file whose path the caller already holds — the
// write path, which has just written it. The stat only refuses a file kind the
// read could block on (a directory or fifo); what the stamp then compares is the
// digest, so no stat result can ever decide that a fact is unchanged.
func StampOfPath(path string) (FactStamp, bool) {
	if strings.TrimSpace(path) == "" {
		return FactStamp{}, false
	}
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() {
		return FactStamp{}, false
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return FactStamp{}, false
	}
	return FactStamp{Path: path, Digest: sha256.Sum256(raw)}, true
}

// FactAt parses the one fact file at path, the counterpart of StampOfPath for a
// caller that must know whether the bytes there still describe the fact it was
// handed. It reads that single file and never lists a directory; ok is false
// when the path does not hold a readable fact file.
func FactAt(path string) (Memory, bool) {
	if strings.TrimSpace(path) == "" {
		return Memory{}, false
	}
	memory, ok, _ := loadMemory(path)
	return memory, ok
}

// SameContent reports whether the file still holds the exact bytes this stamp
// was taken from — those bytes are the fact's semantic version, because they are
// all the model was handed. It reads that one file and touches no directory.
func (f FactStamp) SameContent() bool {
	now, ok := StampOfPath(f.Path)
	return ok && now.Digest == f.Digest
}
