package checkpoint

import (
	"fmt"
	"path/filepath"
)

// CopyConversationPrefixTo writes independent conversation checkpoints for the
// kept messages. File payloads, prepared plans and undo transactions stay here.
func (s *Store) CopyConversationPrefixTo(dir, sessionID string, messageCount int) error {
	if s == nil || dir == "" {
		return fmt.Errorf("conversation checkpoint destination unavailable")
	}
	if filepath.Clean(dir) == filepath.Clean(s.dir) {
		return fmt.Errorf("conversation checkpoint destination is the source")
	}
	s.mu.Lock()
	var checkpoints []*Checkpoint
	for _, original := range s.all() {
		if original.MsgIndex < 0 || original.MsgIndex >= messageCount {
			continue
		}
		checkpoints = append(checkpoints, &Checkpoint{
			SchemaVersion: SchemaV2, Turn: original.Turn, Time: original.Time,
			Prompt: original.Prompt, MsgIndex: original.MsgIndex, SessionID: sessionID,
			Files: []FileSnap{}, Coverage: CoverageNone, ForkCopied: true,
		})
	}
	s.mu.Unlock()
	destination := &Store{dir: dir}
	for _, checkpoint := range checkpoints {
		if err := destination.persist(checkpoint); err != nil {
			return fmt.Errorf("copy conversation checkpoint %d: %w", checkpoint.Turn, err)
		}
	}
	return nil
}
