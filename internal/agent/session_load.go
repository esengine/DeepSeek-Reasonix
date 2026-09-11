package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"reasonix/internal/provider"
	"reasonix/internal/store"
)

// sessionLoadResult is one transcript read from disk: the messages of the
// selected head (or the whole schema-1 log), per-message times where the
// source records them, and the head position for schema-2 sessions.
type sessionLoadResult struct {
	msgs       []provider.Message
	times      []time.Time
	fromEvents bool
	damaged    bool
	dag        bool
	head       HeadRef
	headCount  int
	state      *sessionDAGState
	openTurn   *sessionDAGTurn
	events     []HeadEvent
}

// loadSessionMessages returns the session transcript, preferring the event log
// when the native layer owns it and it holds at least one decodable record.
// Foreign files squatting the log path (legacy import leftovers) are ignored
// in favor of the .jsonl checkpoint. damaged reports that a native log could
// not be replayed to its end (torn tail or corrupt record); callers that write
// should rewrite-and-compact to heal it.
func loadSessionMessages(sessionPath string) (msgs []provider.Message, fromEvents, damaged bool, err error) {
	return loadSessionMessagesWithLimits(sessionPath, defaultSessionReplayLimits, nil)
}

func loadSessionMessagesWithLimits(sessionPath string, limits sessionReplayLimits, hasher *sessionTranscriptHasher) (msgs []provider.Message, fromEvents, damaged bool, err error) {
	return loadSessionMessagesWithContext(context.Background(), sessionPath, limits, hasher)
}

func loadSessionMessagesWithContext(ctx context.Context, sessionPath string, limits sessionReplayLimits, hasher *sessionTranscriptHasher) (msgs []provider.Message, fromEvents, damaged bool, err error) {
	res, err := loadSessionTranscript(ctx, sessionPath, limits, hasher)
	return res.msgs, res.fromEvents, res.damaged, err
}

// loadSessionTranscript dispatches on the log schema: a schema-2 log replays
// the DAG and materializes its selected head, a schema-1 log replays its
// records, and anything else falls back to the .jsonl checkpoint.
func loadSessionTranscript(ctx context.Context, sessionPath string, limits sessionReplayLimits, hasher *sessionTranscriptHasher) (sessionLoadResult, error) {
	if err := ctx.Err(); err != nil {
		return sessionLoadResult{}, err
	}
	probe, err := probeSessionEventLogWithLimits(sessionPath, limits)
	if err != nil {
		return sessionLoadResult{}, err
	}
	if probe.futureSchema {
		return sessionLoadResult{fromEvents: true}, fmt.Errorf("session event log for %s uses schema %d; this build supports up to %d", sessionPath, probe.schemaVersion, sessionDAGSchemaVersion)
	}
	if probe.dag {
		st, err := replaySessionDAG(ctx, store.SessionEventLog(sessionPath), limits)
		if err != nil {
			return sessionLoadResult{fromEvents: true, dag: true}, err
		}
		headID := st.selectedHead()
		msgs, times := st.materialize(headID)
		hasher.addAll(msgs)
		return sessionLoadResult{
			msgs: msgs, times: times, fromEvents: true, damaged: st.damaged, dag: true,
			head:      HeadRef{HeadID: headID, LeafID: st.heads[headID].leaf, LogGeneration: st.generation, LogOffset: st.lastGoodEnd},
			headCount: len(st.heads),
			state:     st,
			openTurn:  st.heads[headID].openTurn,
			events:    loadHeadEvents(st, headID),
		}, nil
	}
	if probe.native && probe.size > 0 {
		replay, replayErr := replaySessionEventLogWithContext(ctx, store.SessionEventLog(sessionPath), limits, hasher)
		if replayErr != nil {
			return sessionLoadResult{fromEvents: true}, replayErr
		}
		if replay.records > 0 {
			return sessionLoadResult{msgs: replay.msgs, times: replay.times, fromEvents: true, damaged: replay.damaged}, nil
		}
		// Defensive: the probe saw a native head but nothing replayed; fall
		// back to the checkpoint and let the next save rebuild the log.
		msgs, err := loadSessionMessagesFromJSONLContext(ctx, sessionPath, hasher)
		return sessionLoadResult{msgs: msgs, damaged: true}, err
	}
	msgs, err := loadSessionMessagesFromJSONLContext(ctx, sessionPath, hasher)
	return sessionLoadResult{msgs: msgs}, err
}

func loadSessionMessagesFromJSONL(path string, hasher *sessionTranscriptHasher) ([]provider.Message, error) {
	return loadSessionMessagesFromJSONLContext(context.Background(), path, hasher)
}

func loadSessionMessagesFromJSONLContext(ctx context.Context, path string, hasher *sessionTranscriptHasher) ([]provider.Message, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var msgs []provider.Message
	dec := json.NewDecoder(&contextReader{ctx: ctx, reader: f})
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		var m provider.Message
		if err := dec.Decode(&m); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return nil, fmt.Errorf("decode %s: %w", path, err)
		}
		msgs = append(msgs, hasher.add(m))
	}
	return msgs, nil
}
