package transcript

import (
	"encoding/json"
	"errors"
	"os"
	"reflect"

	"reasonix/internal/eventwire"
	"reasonix/internal/fileutil"
	"reasonix/internal/store"
)

// Checkpoint is the durable display state, separate from provider messages.
// Its digest binds it to the terminal transcript that produced its coverage.
type Checkpoint struct {
	Version           int                          `json:"version"`
	Identity          Identity                     `json:"identity"`
	CoveredThroughSeq uint64                       `json:"coveredThroughSeq"`
	TranscriptDigest  string                       `json:"transcriptDigest"`
	ProviderCount     int                          `json:"providerCount"`
	Records           []Message                    `json:"records"`
	Runtime           Runtime                      `json:"runtime"`
	ActiveAttempts    []ActiveAttempt              `json:"activeAttempts"`
	Completion        *eventwire.CompletionSummary `json:"completion,omitempty"`
}

func (p *Projection) Checkpoint(digest string) (Checkpoint, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	runtime, attempts := p.runtimeLocked()
	state := Checkpoint{Version: ProtocolVersion, Identity: p.identity, CoveredThroughSeq: p.covered,
		TranscriptDigest: digest, Records: p.buffer.Messages(), Runtime: runtime, ActiveAttempts: attempts, Completion: p.buffer.completion}
	// Detach mutable metadata while sharing immutable strings. Encoding and
	// decoding the full transcript here duplicates large bodies under p.mu;
	// SaveCheckpoint already owns the required encoding outside that lock.
	owned := mapContentStrings(reflect.ValueOf(state), nil, func(text string, _ []string) string { return text }).Interface().(Checkpoint)
	return owned, nil
}

func RestoreCheckpoint(state Checkpoint, identity Identity) (*Projection, error) {
	if state.Version != ProtocolVersion || state.Identity.SessionID != identity.SessionID ||
		state.Identity.HeadID != identity.HeadID || state.Identity.RewriteEpoch != identity.RewriteEpoch {
		return nil, errors.New("transcript checkpoint identity mismatch")
	}
	p, err := NewProjection(identity, state.Records, state.CoveredThroughSeq)
	if err != nil {
		return nil, err
	}
	b, err := json.Marshal(state)
	if err != nil {
		return nil, err
	}
	var owned Checkpoint
	if err = json.Unmarshal(b, &owned); err != nil {
		return nil, err
	}
	p.runtime = owned.Runtime
	if p.runtime.StartedAt > 0 {
		p.startedTurnID = p.runtime.TurnID
	}
	for _, attempt := range owned.ActiveAttempts {
		p.attempts[attempt.ID] = attempt
	}
	for _, prompt := range owned.Runtime.PendingEvents {
		id := prompt.PromptID
		if id != "" {
			p.prompts[id] = prompt
		}
	}
	p.buffer.completion = owned.Completion
	return p, nil
}

func SaveCheckpoint(sessionPath string, state Checkpoint) error {
	path := store.SessionTranscriptProjection(sessionPath)
	if path == "" {
		return nil
	}
	b, err := json.Marshal(state)
	if err != nil {
		return err
	}
	return fileutil.AtomicWriteFile(path, b, 0o600)
}

func LoadCheckpoint(sessionPath string) (Checkpoint, bool, error) {
	path := store.SessionTranscriptProjection(sessionPath)
	if path == "" {
		return Checkpoint{}, false, nil
	}
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return Checkpoint{}, false, nil
	}
	if err != nil {
		return Checkpoint{}, false, err
	}
	var state Checkpoint
	if err = json.Unmarshal(b, &state); err != nil {
		return Checkpoint{}, false, err
	}
	if state.Version != ProtocolVersion {
		return Checkpoint{}, false, errors.New("unsupported transcript checkpoint version")
	}
	return state, true, nil
}
