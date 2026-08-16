package checkpoint

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	fileenc "reasonix/internal/fileutil/encoding"
)

// load arbitrates legacy metadata, expired metadata, and v3 turn directories
// by checkpoint timestamp. Format priority only breaks an exact timestamp tie.
func (s *Store) load() {
	type loadedCheckpoint struct {
		checkpoint *Checkpoint
		priority   int
	}
	loaded := map[int]loadedCheckpoint{}
	choose := func(c *Checkpoint, priority int) {
		if c == nil {
			return
		}
		current, ok := loaded[c.Turn]
		if !ok || c.Time.After(current.checkpoint.Time) ||
			(c.Time.Equal(current.checkpoint.Time) && priority > current.priority) {
			loaded[c.Turn] = loadedCheckpoint{checkpoint: c, priority: priority}
		}
	}
	loadDir := func(dir string, expired bool) {
		ents, err := os.ReadDir(dir)
		if err != nil {
			return
		}
		for _, e := range ents {
			if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
				continue
			}
			var turnNum int
			if _, err := fmt.Sscanf(e.Name(), "turn-%d.json", &turnNum); err != nil {
				continue
			}
			b, err := fileenc.ReadFileUTF8(filepath.Join(dir, e.Name()))
			if err != nil {
				continue
			}
			var c Checkpoint
			if json.Unmarshal(b, &c) != nil {
				continue
			}
			if expired {
				c.ExpiredFilePayload = true
				for i := range c.Files {
					c.Files[i].PayloadExpired = true
					c.Files[i].BlobRef = ""
					c.Files[i].Content = nil
				}
			}
			// Mark v1 as legacy_unverified.
			if c.SchemaVersion == 0 || c.SchemaVersion < SchemaV2 {
				c.SchemaVersion = SchemaV1
				c.Legacy = true
				c.Coverage = CoverageLegacy
				hasLegacyGap := false
				for _, g := range c.CoverageGaps {
					if g.Reason == GapLegacyUnverified {
						hasLegacyGap = true
						break
					}
				}
				if !hasLegacyGap {
					c.CoverageGaps = append(c.CoverageGaps, CoverageGap{Reason: GapLegacyUnverified, Detail: "v1 checkpoint cannot verify later manual edits"})
				}
			}
			priority := 2
			if expired {
				priority = 1
			}
			choose(&c, priority)
		}
	}
	loadDir(s.dir, false)
	loadDir(s.expiredDir(), true)
	// A v3 compatibility marker has the same timestamp as its turn directory,
	// so v3 wins ties. A previous build that wrote a genuinely newer checkpoint
	// with the same turn wins by timestamp instead of being hidden by load order.
	for _, c := range s.loadV3Turns() {
		choose(c, 3)
	}
	s.done = s.done[:0]
	for _, item := range loaded {
		s.done = append(s.done, item.checkpoint)
	}
	sort.Slice(s.done, func(i, j int) bool { return s.done[i].Turn < s.done[j].Turn })
}
