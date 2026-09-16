package transcript

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"time"

	"reasonix/internal/eventwire"
)

const FollowProtocolVersion = 2

type Change struct {
	Runtime       *Runtime         `json:"runtime,omitempty"`
	AttemptID     string           `json:"attemptId,omitempty"`
	Index         uint64           `json:"index"`
	ResultSeq     uint64           `json:"resultSeq,omitempty"`
	ResultKind    string           `json:"resultKind,omitempty"`
	Revision      uint64           `json:"revision"`
	CommitSeq     uint64           `json:"commitSeq"`
	DurableSeq    uint64           `json:"durableSeq"`
	FirstSeq      uint64           `json:"firstSeq,omitempty"`
	Records       []Message        `json:"records,omitempty"`
	Event         *eventwire.Event `json:"event,omitempty"`
	ResetRequired bool             `json:"resetRequired,omitempty"`
}

type FollowRequest struct {
	Subscription  string `json:"subscription,omitempty"`
	AfterRevision uint64 `json:"afterRevision,omitempty"`
	Close         bool   `json:"close,omitempty"`
}

type FollowResponse struct {
	ProtocolVersion int       `json:"protocolVersion"`
	Subscription    string    `json:"subscription"`
	Snapshot        *Snapshot `json:"snapshot,omitempty"`
	Changes         []Change  `json:"changes"`
	ResetRequired   bool      `json:"resetRequired"`
}

type follower struct {
	ack   uint64
	queue []Change
	bytes int
	reset bool
	wake  chan struct{}
	seen  time.Time
}

// Follow registers before freezing its initial cut. Subsequent calls long-poll
// this subscription; no network callback is invoked under the owner lock.
func (p *Projection) Follow(ctx context.Context, req FollowRequest) (FollowResponse, error) {
	p.mu.Lock()
	if p.followers == nil {
		p.followers = make(map[string]*follower)
	}
	now := time.Now()
	for id, f := range p.followers {
		if now.Sub(f.seen) > 2*time.Minute {
			f.reset = true
			select {
			case f.wake <- struct{}{}:
			default:
			}
			delete(p.followers, id)
		}
	}
	out := FollowResponse{ProtocolVersion: FollowProtocolVersion, Subscription: req.Subscription, Changes: []Change{}}
	if req.Close {
		if f := p.followers[req.Subscription]; f != nil {
			f.reset = true
			select {
			case f.wake <- struct{}{}:
			default:
			}
		}
		delete(p.followers, req.Subscription)
		p.mu.Unlock()
		return out, nil
	}
	if req.Subscription == "" {
		if len(p.followers) >= 64 {
			p.mu.Unlock()
			return out, errors.New("too many transcript followers")
		}
		out.Subscription = rand.Text()
		f := &follower{wake: make(chan struct{}, 1), seen: now, ack: p.revision}
		p.followers[out.Subscription] = f
		frozen, err := p.freezeLocked("")
		if err != nil {
			delete(p.followers, out.Subscription)
		}
		p.mu.Unlock()
		if err != nil {
			return out, err
		}
		snapshot, err := frozen.snapshotCurrent(PageRequest{Records: 32})
		snapshot.ProtocolVersion = FollowProtocolVersion
		out.Snapshot = &snapshot
		return out, err
	}
	f := p.followers[req.Subscription]
	if f == nil {
		p.mu.Unlock()
		out.ResetRequired = true
		return out, nil
	}
	f.seen = now
	if req.AfterRevision < f.ack || req.AfterRevision > p.revision {
		f.reset = true
	} else {
		f.ack = req.AfterRevision
	}
	p.mu.Unlock()
	timer := time.NewTimer(25 * time.Second)
	defer timer.Stop()
	for {
		p.mu.Lock()
		for len(f.queue) > 0 && f.queue[0].Revision <= req.AfterRevision {
			encoded, _ := json.Marshal(f.queue[0])
			f.bytes -= len(encoded)
			f.queue = f.queue[1:]
		}
		if f.reset || len(f.queue) > 0 {
			out.ResetRequired = f.reset
			out.Changes = append(out.Changes, f.queue...)
			p.mu.Unlock()
			return out, nil
		}
		p.mu.Unlock()
		select {
		case <-ctx.Done():
			return out, ctx.Err()
		case <-timer.C:
			return out, nil
		case <-f.wake:
		}
	}
}

func (p *Projection) CloseFollowers() {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, f := range p.followers {
		f.reset = true
		select {
		case f.wake <- struct{}{}:
		default:
		}
	}
	clear(p.followers)
}

func (p *Projection) publishChangeLocked(change Change) {
	change.Revision, change.CommitSeq, change.DurableSeq = p.revision, p.covered, p.durable
	encoded, err := json.Marshal(change)
	// Detach retained deltas from mutable event payloads and caller-owned rows.
	var owned Change
	if err == nil {
		err = json.Unmarshal(encoded, &owned)
	}
	for _, f := range p.followers {
		if f.reset {
			continue
		}
		if err != nil || len(f.queue) >= 256 || f.bytes+len(encoded) > MaxResponseBytes/2 {
			f.queue, f.bytes, f.reset = nil, 0, true
		} else {
			f.queue = append(f.queue, owned)
			f.bytes += len(encoded)
		}
		select {
		case f.wake <- struct{}{}:
		default:
		}
	}
}

func (p *Projection) SetDurableSequence(sequence uint64) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if sequence > p.durable {
		p.durable = sequence
		p.revision++
		p.publishChangeLocked(Change{})
	}
}
