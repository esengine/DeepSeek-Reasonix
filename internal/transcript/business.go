package transcript

import (
	"slices"

	"reasonix/internal/event"
	"reasonix/internal/eventwire"
)

func (p *Projection) PersistenceFailed() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.runtime.Status = event.TurnRecoveryRequired
	p.revision++
	p.publishChangeLocked(Change{Event: &eventwire.Event{Kind: "turn_status", Status: string(event.TurnRecoveryRequired)}})
	p.revision++
	p.publishChangeLocked(Change{Event: &eventwire.Event{Kind: "notice", Level: "warn", Code: "transcript_save_failed", Text: "Session output could not be saved. Displayed output is retained; recovery is required."}})
}

func (p *Projection) RestoreRuntime(runtime Runtime, durable uint64) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.runtime, p.durable = runtime, durable
}

// AcceptBusiness advances coverage for every accepted batch, including batches
// that have no visible records. Rows come from the canonical message reader,
// never from a wire-event ledger. Mutable stream owners are retained separately
// from the bounded settled tail.
func (p *Projection) AcceptBusiness(rows []Message, covered uint64, turnID string, rewrite bool, finalMessageID ...string) {
	p.acceptBusiness(rows, nil, covered, turnID, rewrite, finalMessageID...)
}

// AcceptRetractions invalidates the reading cut while retaining unrelated
// active output. The canonical reader supplies the new visible history.
func (p *Projection) AcceptRetractions(rows []Message, removed []string, covered uint64, turnID, finalMessageID string) {
	p.acceptBusiness(rows, removed, covered, turnID, false, finalMessageID)
}

func (p *Projection) acceptBusiness(rows []Message, removed []string, covered uint64, turnID string, rewrite bool, finalMessageID ...string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if covered <= p.covered {
		return
	}
	if rewrite {
		p.buffer.Reset()
	}
	for _, id := range removed {
		for index, row := range slices.Backward(p.buffer.messages) {
			if row.message.MessageID == id {
				if row.message.Role == "user" {
					p.buffer.userTurns--
				}
				p.buffer.messages = append(p.buffer.messages[:index], p.buffer.messages[index+1:]...)
			}
		}
		delete(p.buffer.byMessageID, id)
		delete(p.results, id)
		for key, attempt := range p.attempts {
			if attempt.MessageID == id {
				delete(p.attempts, key)
			}
		}
	}
	reset := rewrite || len(removed) > 0
	if reset {
		p.identity.RewriteEpoch++
		clear(p.snapshots)
		p.snapshotOrder = nil
		p.snapshotBytes = 0
	}
	if p.buffer.byMessageID == nil {
		p.buffer.byMessageID = make(map[string]*bufferedMessage)
	}
	if p.results == nil {
		p.results = make(map[string]uint64)
	}
	published := make([]Message, 0, len(rows))
	for _, message := range rows {
		p.ensureRecordIdentity(&message)
		published = append(published, message)
		if message.TurnID == "" {
			message.TurnID = turnID
		}
		var row *bufferedMessage
		for _, existing := range p.buffer.messages {
			if existing.message.RecordID == message.RecordID {
				row = existing
				break
			}
		}
		if row == nil {
			row = &bufferedMessage{}
			p.buffer.messages = append(p.buffer.messages, row)
			if message.Role == "user" {
				p.buffer.userTurns++
			}
		}
		row.message = message
		if message.Role == "assistant" {
			if len(finalMessageID) == 0 {
				p.runtime.FinalMessageID = message.MessageID
			}
			row.content.replace(message.Content)
			row.reasoning.replace(message.Reasoning)
			row.message.Content, row.message.Reasoning = "", ""
		}
		if message.MessageID != "" && (message.Role == "assistant" || message.Role == "user") {
			p.buffer.byMessageID[message.MessageID] = row
			p.results[message.MessageID] = covered
		}
	}
	first := p.covered + 1
	if len(finalMessageID) > 0 {
		p.runtime.FinalMessageID = finalMessageID[0]
	}
	p.covered = covered
	p.revision++
	p.trimSettledLocked()
	p.publishChangeLocked(Change{FirstSeq: first, Records: published, ResetRequired: reset})
}

func (p *Projection) trimSettledLocked() {
	const retainedRecords = 96
	if len(p.buffer.messages) <= retainedRecords {
		return
	}
	keep := make([]*bufferedMessage, 0, retainedRecords+len(p.attempts))
	for i, row := range p.buffer.messages {
		active := row.message.Pending
		for _, attempt := range p.attempts {
			active = active || attempt.MessageID == row.message.MessageID
		}
		if active || i >= len(p.buffer.messages)-retainedRecords {
			keep = append(keep, row)
		} else if p.buffer.byMessageID[row.message.MessageID] == row {
			delete(p.buffer.byMessageID, row.message.MessageID)
			delete(p.results, row.message.MessageID)
		}
	}
	p.buffer.messages = keep
}
