package session

import (
	"context"
	"encoding/json"
	"slices"

	"reasonix/internal/provider"
	"reasonix/internal/transcript"
	"reasonix/internal/turnevent"
)

// Transcript is owned by the runtime, not a replaceable controller. The
// Session acceptance lock serializes canonical batches and transient frames.
// No subscriber callback, disk read or network write runs under that lock.
func (r *Runtime) Transcript() *transcript.Projection { return r.transcript }

func (r *Runtime) FollowTranscript(ctx context.Context, req transcript.FollowRequest) (transcript.FollowResponse, error) {
	r.session.mu.Lock()
	if r.session.binding != nil {
		durable, _, _ := r.session.binding.progress()
		r.transcript.SetDurableSequence(durable)
	}
	r.session.mu.Unlock()
	return r.transcript.Follow(ctx, req)
}

func (r *Runtime) PublishTranscriptFrame(envelope turnevent.Envelope) error {
	s := r.session
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.binding != nil {
		durable, _, _ := s.binding.progress()
		r.transcript.SetDurableSequence(durable)
	}
	return r.transcript.ApplyFrame(envelope, s.next-1)
}

func (s *Session) acceptTranscriptCommit(commit Commit) {
	if s.transcript == nil {
		return
	}
	var messages []provider.Message
	var removed []string
	rewrite := false
	for _, e := range commit.Events {
		switch e.Kind {
		case "message/complete", "message/upsert":
			var payload struct {
				Message provider.Message `json:"message"`
			}
			if json.Unmarshal(e.Payload, &payload) == nil {
				removed = slices.DeleteFunc(removed, func(id string) bool { return id == payload.Message.ID })
				messages = append(messages, payload.Message)
			}
		case "message/retract":
			ids, err := retractedMessageIDs(e, e.Payload)
			if err == nil {
				removed = append(removed, ids...)
				messages = slices.DeleteFunc(messages, func(message provider.Message) bool { return slices.Contains(ids, message.ID) })
			}
		case "history/replace", "legacy/import":
			var payload struct {
				Messages []provider.Message `json:"messages"`
			}
			if json.Unmarshal(e.Payload, &payload) == nil {
				messages, rewrite = payload.Messages, true
				removed = nil
			}
		}
	}
	finalID := s.projection.CurrentTurnMessageID
	if finalID == "" && len(s.projection.Turns) > 0 {
		last := s.projection.Turns[len(s.projection.Turns)-1]
		if last.TurnID == commit.TurnID || s.projection.TurnID == "" {
			finalID = last.MessageID
		}
	}
	rows := transcript.History(messages, transcript.HistoryOptions{})
	if len(removed) > 0 && !rewrite {
		s.transcript.AcceptRetractions(rows, removed, commit.LastSequence(), commit.TurnID, finalID)
	} else {
		s.transcript.AcceptBusiness(rows, commit.LastSequence(), commit.TurnID, rewrite, finalID)
	}
}
