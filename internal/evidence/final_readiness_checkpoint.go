package evidence

import "encoding/json"

// FinalReadinessCheckpoint is the bounded, durable copy of a failed turn's
// host evidence, stored only in provider-excluded local metadata.
type FinalReadinessCheckpoint struct {
	Receipts []Receipt `json:"receipts,omitempty"`
}

// FinalReadinessCheckpoint snapshots the ledger without duplicating writer
// payloads, patches, or delegated prompts.
func (l *Ledger) FinalReadinessCheckpoint() FinalReadinessCheckpoint {
	if l == nil {
		return FinalReadinessCheckpoint{}
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	receipts := make([]Receipt, len(l.receipts))
	for i, receipt := range l.receipts {
		receipts[i] = receipt
		receipts[i].Args = recoveryReceiptArgs(receipt)
	}
	return FinalReadinessCheckpoint{Receipts: receipts}
}

// RestoreFinalReadinessCheckpoint rebuilds the ordered ledger through Record
// so live and restored receipts receive identical normalization.
func (l *Ledger) RestoreFinalReadinessCheckpoint(checkpoint FinalReadinessCheckpoint) bool {
	if l == nil {
		return false
	}
	l.Reset()
	for _, receipt := range checkpoint.Receipts {
		l.Record(receipt)
	}
	return true
}

func recoveryReceiptArgs(r Receipt) json.RawMessage {
	switch r.ToolName {
	case "bash":
		if r.Command == "" {
			return nil
		}
		args, _ := json.Marshal(map[string]string{"command": r.Command})
		return args
	case "complete_step", "review_report", "complete_subtask":
		return append(json.RawMessage(nil), r.Args...)
	default:
		// Derived receipt fields retain readiness facts; raw writer and
		// delegation arguments are intentionally not duplicated on disk.
		return nil
	}
}
