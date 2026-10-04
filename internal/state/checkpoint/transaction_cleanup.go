package checkpoint

import (
	"log/slog"
	"os"
)

func (s *Store) cleanupPublishTemps(targets []TransactionTarget) {
	for _, target := range targets {
		if target.PublishTmp != "" {
			_ = secureRemove(s.root, target.PublishTmp)
		}
	}
}

// Backups protect an incomplete transaction; undo uses the saved forward payload.
func (s *Store) cleanupCommittedBackups(targets []TransactionTarget) {
	for _, target := range targets {
		if target.BackupPath == "" {
			continue
		}
		if err := secureRemove(s.root, target.BackupPath); err != nil && !os.IsNotExist(err) {
			slog.Warn("checkpoint: remove committed transaction backup", "path", target.BackupPath, "err", err)
		}
	}
}
