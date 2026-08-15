package checkpoint

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	"reasonix/internal/fileutil"
	fileenc "reasonix/internal/fileutil/encoding"
)

func (s *Store) turnsDir() string {
	return filepath.Join(s.dir, "turns")
}

func (s *Store) turnDir(turn int) string {
	return filepath.Join(s.turnsDir(), strconv.Itoa(turn))
}

func (s *Store) v3MetaPath(turn int) string {
	return filepath.Join(s.turnDir(turn), "meta.json")
}

func (s *Store) v3BeforePath(turn, index int) string {
	return filepath.Join(s.turnDir(turn), "files", fmt.Sprintf("%04d.before", index))
}

func v3PayloadBytes(f FileSnap) []byte {
	if f.rawContent != nil {
		return f.rawContent
	}
	if f.Content == nil {
		return nil
	}
	if f.Encoding != nil {
		return fileenc.Encode(*f.Content, *f.Encoding)
	}
	return []byte(*f.Content)
}

func (s *Store) persistV3(c *Checkpoint) error {
	// Previous builds only inspect turn-N.json. Keep a payload-free v2 marker
	// so their NextTurn remains monotonic across a downgrade; write it first so
	// a crash can leave reduced rewind visibility, never an invisible turn.
	marker := *c
	marker.SchemaVersion = SchemaV2
	marker.Files = []FileSnap{}
	marker.Coverage = CoverageNone
	marker.CoverageGaps = nil
	marker.ActiveWriters = nil
	marker.ExpiredFilePayload = true
	markerBytes, err := json.Marshal(&marker)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(s.dir, 0o755); err != nil {
		return err
	}
	markerPath := filepath.Join(s.dir, fmt.Sprintf("turn-%d.json", c.Turn))
	if err := fileutil.AtomicWriteFileStrict(markerPath, markerBytes, 0o644); err != nil {
		return err
	}

	turnDir := s.turnDir(c.Turn)
	if err := os.MkdirAll(filepath.Join(turnDir, "files"), 0o755); err != nil {
		return err
	}
	wire := *c
	wire.SchemaVersion = SchemaV3
	wire.Files = make([]FileSnap, len(c.Files))
	for i, f := range c.Files {
		snap := f
		snap.BlobRef = ""
		payloadPath := s.v3BeforePath(c.Turn, i)
		if f.Content != nil && !f.PayloadExpired {
			if err := fileutil.AtomicWriteFile(payloadPath, v3PayloadBytes(f), 0o644); err != nil {
				return err
			}
			snap.Content = nil
			snap.Encoding = nil
		} else if err := os.Remove(payloadPath); err != nil && !os.IsNotExist(err) {
			return err
		}
		wire.Files[i] = snap
	}
	b, err := json.Marshal(&wire)
	if err != nil {
		return err
	}
	return fileutil.AtomicWriteFileStrict(s.v3MetaPath(c.Turn), b, 0o644)
}

func (s *Store) removeTurnArtifacts(turns map[int]bool) error {
	if s.dir == "" {
		return nil
	}
	for turn := range turns {
		// The compatibility marker is the cross-version liveness record. Remove
		// payloads first so a crash can leave a visible payload-free turn, never
		// a markerless directory that a newer reader resurrects after downgrade.
		if err := os.RemoveAll(s.turnDir(turn)); err != nil {
			return fmt.Errorf("remove checkpoint turn %d: %w", turn, err)
		}
		for _, path := range []string{
			filepath.Join(s.dir, fmt.Sprintf("turn-%d.json", turn)),
			filepath.Join(s.expiredDir(), fmt.Sprintf("turn-%d.json", turn)),
		} {
			if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
				return fmt.Errorf("remove checkpoint turn %d: %w", turn, err)
			}
		}
	}
	return nil
}

func (s *Store) loadV3Turns() []*Checkpoint {
	ents, err := os.ReadDir(s.turnsDir())
	if err != nil {
		return nil
	}
	var turns []*Checkpoint
	for _, e := range ents {
		if !e.IsDir() {
			continue
		}
		turn, err := strconv.Atoi(e.Name())
		if err != nil {
			continue
		}
		// Old readers truncate only turn-N.json. Treat marker absence as a
		// tombstone so reopening with a newer build cannot revive those turns.
		if _, err := os.Stat(filepath.Join(s.dir, fmt.Sprintf("turn-%d.json", turn))); err != nil {
			continue
		}
		b, err := fileenc.ReadFileUTF8(s.v3MetaPath(turn))
		if err != nil {
			continue
		}
		var c Checkpoint
		if json.Unmarshal(b, &c) != nil {
			continue
		}
		c.Turn = turn
		c.SchemaVersion = SchemaV3
		for i := range c.Files {
			if c.Files[i].PayloadExpired {
				continue
			}
			raw, err := os.ReadFile(s.v3BeforePath(turn, i))
			if err != nil {
				continue
			}
			if want := c.Files[i].SHA256; want != "" && Digest(raw) != want {
				continue
			}
			enc, detected := fileenc.Detect(raw)
			text := string(fileenc.Decode(detected, enc))
			c.Files[i].Content = &text
			c.Files[i].Encoding = &enc
			c.Files[i].BlobRef = ""
			c.Files[i].rawContent = append([]byte(nil), raw...)
		}
		turns = append(turns, &c)
	}
	return turns
}

func (s *Store) v3PayloadSize(turn int) (int64, error) {
	root := filepath.Join(s.turnDir(turn), "files")
	var total int64
	err := filepath.WalkDir(root, func(_ string, d os.DirEntry, err error) error {
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		if d.IsDir() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if info.Mode().IsRegular() {
			total += info.Size()
		}
		return nil
	})
	if os.IsNotExist(err) {
		return 0, nil
	}
	return total, err
}

// pruneV3TurnsLocked applies both count retention and the legacy 1 GiB soft
// payload budget to complete v3 turn directories. The current and protected
// turns may temporarily exceed the budget; the next unprotected turn prunes
// whole oldest directories. Older v1/v2 metadata remains on its legacy path.
func (s *Store) pruneV3TurnsLocked() {
	if s.dir == "" || s.retainN <= 0 {
		return
	}
	var turns []*Checkpoint
	for _, c := range s.all() {
		if c.SchemaVersion >= SchemaV3 {
			turns = append(turns, c)
		}
	}
	excess := len(turns) - s.retainN
	sizes := make(map[*Checkpoint]int64, len(turns))
	var totalSize int64
	sizeKnown := true
	for _, c := range turns {
		size, err := s.v3PayloadSize(c.Turn)
		if err != nil {
			sizeKnown = false
			break
		}
		sizes[c] = size
		totalSize += size
	}
	quotaExceeded := func() bool {
		return sizeKnown && s.blobQuota > 0 && totalSize > s.blobQuota
	}
	if excess <= 0 && !quotaExceeded() {
		return
	}
	removed := make(map[*Checkpoint]bool)
	for _, c := range turns {
		if excess <= 0 && !quotaExceeded() {
			break
		}
		if c == s.cur || s.protectTurns[c.Turn] {
			continue
		}
		if err := s.removeTurnArtifacts(map[int]bool{c.Turn: true}); err != nil {
			continue
		}
		removed[c] = true
		if excess > 0 {
			excess--
		}
		totalSize -= sizes[c]
	}
	if len(removed) == 0 {
		return
	}
	kept := s.done[:0]
	for _, c := range s.done {
		if !removed[c] {
			kept = append(kept, c)
		}
	}
	s.done = kept
	// Pre-v3 builds briefly wrote both a turn directory and a blob. Once such a
	// turn ages out, the legacy mark-and-sweep can reclaim its orphaned blob.
	s.pruneBlobsLocked()
}
