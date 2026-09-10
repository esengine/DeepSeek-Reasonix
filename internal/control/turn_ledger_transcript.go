package control

import (
	"log/slog"
	"reasonix/internal/turnevent"
)

func (c *Controller) updateTurnLedgerTranscript(ledger *turnevent.Ledger) {
	if c.executor != nil && c.executor.Session() != nil {
		session := c.executor.Session()
		digest, digestErr := session.ContentDigest()
		if digestErr != nil {
			slog.Warn("controller: compute terminal transcript digest", "err", digestErr)
		} else {
			ledger.SetTranscriptSnapshot(int64(session.TranscriptVersion()), digest)
		}
		if ref, ok := session.Head(); ok {
			ledger.SetTranscriptHead(ref.HeadID, session.LeafID())
		} else {
			ledger.SetTranscriptHead("", "")
		}
	}
}
