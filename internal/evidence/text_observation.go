package evidence

// TextObservation is a turn-scoped, content-free record of a line window the
// model was shown. Only canonical path, line position, and SHA-256 line
// digests are retained; source text is never stored in the ledger.
type TextObservation struct {
	Sequence   uint64
	Path       string
	StartLine  int
	LineHashes []string
}

// ObservationBoundary freezes the ledger sequence at the start of a provider
// tool-call batch. Later observations are ineligible because the model has not
// seen their result yet.
func (l *Ledger) ObservationBoundary() uint64 {
	if l == nil {
		return 0
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.nextSequence
}

func (l *Ledger) RecordTextObservation(o TextObservation) {
	if l == nil || o.Path == "" || o.StartLine < 1 || len(o.LineHashes) == 0 {
		return
	}
	o.LineHashes = append([]string(nil), o.LineHashes...)
	l.mu.Lock()
	defer l.mu.Unlock()
	l.nextSequence++
	o.Sequence = l.nextSequence
	l.observations = append(l.observations, o)
}

func (l *Ledger) TextObservations() []TextObservation {
	if l == nil {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make([]TextObservation, len(l.observations))
	for i, o := range l.observations {
		out[i], out[i].LineHashes = o, append([]string(nil), o.LineHashes...)
	}
	return out
}

func (l *Ledger) ReceiptSequence(index int) (uint64, bool) {
	if l == nil {
		return 0, false
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if index < 0 || index >= len(l.receipts) {
		return 0, false
	}
	return l.receipts[index].Sequence, true
}
