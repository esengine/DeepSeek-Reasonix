package turnevent

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"testing"

	"reasonix/internal/event"
)

func TestEnvelopesCarryHeadReferenceAcrossCompaction(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	l, err := Open(path, "session")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	turnID, err := l.Begin()
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	l.SetTranscriptSnapshot(3, "digest")
	l.SetTranscriptHead("main", "01LEAF")
	if _, ok, err := l.Append(event.Event{Kind: event.TurnStarted}, event.TurnInProgress); err != nil || !ok {
		t.Fatalf("started: ok=%v err=%v", ok, err)
	}
	done, ok, err := l.Append(event.Event{Kind: event.TurnDone}, event.TurnCompleted)
	if err != nil || !ok {
		t.Fatalf("done: ok=%v err=%v", ok, err)
	}
	recs, err := l.EventsAfter(0)
	if err != nil || len(recs) != 2 {
		t.Fatalf("EventsAfter: %v (%d records)", err, len(recs))
	}
	if rec := recs[1]; rec.HeadID != "main" || rec.LeafMessageID != "01LEAF" || rec.TranscriptRevision != 3 {
		t.Fatalf("terminal envelope = %+v, want head reference beside the schema-1 identity", rec)
	}
	view, err := l.Replay(0)
	if err != nil || view.HeadID != "main" || view.LeafMessageID != "01LEAF" {
		t.Fatalf("replay view = %+v err=%v", view, err)
	}
	if err := l.Compact(); err != nil {
		t.Fatalf("Compact: %v", err)
	}
	if err := l.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	reopened, err := Open(path, "session")
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	summaries := reopened.summaries
	if len(summaries) != 1 || summaries[0].TurnID != turnID || summaries[0].HeadID != "main" || summaries[0].LeafMessageID != "01LEAF" {
		t.Fatalf("summaries after checkpoint = %+v", summaries)
	}
	view, err = reopened.Replay(done.Sequence)
	if err != nil || view.HeadID != "main" || view.LeafMessageID != "01LEAF" {
		t.Fatalf("replay after reopen = %+v err=%v", view, err)
	}
	// A schema-1 session clears the head without touching the revision pair.
	reopened.SetTranscriptHead("", "")
	if _, err := reopened.Begin(); err != nil {
		t.Fatal(err)
	}
	next, ok, err := reopened.Append(event.Event{Kind: event.TurnDone}, event.TurnCompleted)
	if err != nil || !ok {
		t.Fatalf("schema-1 append ok=%v err=%v", ok, err)
	}
	recs, err = reopened.EventsAfter(next.Sequence - 1)
	if err != nil || len(recs) != 1 || recs[0].HeadID != "" || recs[0].LeafMessageID != "" {
		t.Fatalf("schema-1 envelope = %+v err=%v", recs, err)
	}
}

func TestEnvelopeWithoutHeadFieldsDecodesEmpty(t *testing.T) {
	var rec Envelope
	if err := json.Unmarshal([]byte(`{"schemaVersion":2,"sessionId":"s","turnId":"t","seq":1,"kind":"turn_done","status":"completed","transcriptRevision":4,"transcriptDigest":"d","createdAt":1,"event":{}}`), &rec); err != nil {
		t.Fatal(err)
	}
	if rec.HeadID != "" || rec.LeafMessageID != "" || rec.TranscriptRevision != 4 {
		t.Fatalf("legacy envelope = %+v", rec)
	}
	b, err := json.Marshal(rec)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(b, []byte("headId")) || bytes.Contains(b, []byte("leafMessageId")) {
		t.Fatalf("empty head fields must stay omitted: %s", b)
	}
}
