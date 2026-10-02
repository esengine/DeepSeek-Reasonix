package serve

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"reasonix/internal/provider"
	"reasonix/internal/session"
)

func TestSessionsExposeVisibleResultSequence(t *testing.T) {
	srv, _, service, ref := newExclusiveSessionServe(t)
	runtime, ok := service.Runtime(ref)
	if !ok {
		t.Fatal("runtime missing")
	}
	read := func() map[string]any {
		t.Helper()
		w := httptest.NewRecorder()
		srv.sessions(w, httptest.NewRequest(http.MethodGet, "/sessions", nil))
		var rows []map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &rows); err != nil {
			t.Fatal(err)
		}
		for _, row := range rows {
			if row["sessionId"] == ref.SessionID {
				return row
			}
		}
		t.Fatal("session missing")
		return nil
	}
	if row := read(); row["resultSequence"] != float64(0) || row["metadataReady"] != true {
		t.Fatalf("new session must distinguish a known zero from an old server: %+v", row)
	}
	payload, _ := json.Marshal(map[string]any{"message": provider.Message{ID: "answer", Role: provider.RoleAssistant, Content: "done"}})
	commit, err := runtime.Session().Append(t.Context(), session.Batch{OperationID: "answer", TurnID: "turn", Events: []session.Event{
		{Kind: "turn/start"}, {Kind: "message/complete", Payload: payload},
		{Kind: "turn/end", Payload: json.RawMessage(`{"status":"completed"}`)},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.Session().Flush(t.Context()); err != nil {
		t.Fatal(err)
	}
	want := float64(commit.FirstSequence + uint64(commit.EventCount) - 1)
	if row := read(); row["resultSequence"] != want {
		t.Fatalf("completed result missing: %+v", row)
	}
	if err := service.SetTitle(t.Context(), ref, "renamed"); err != nil {
		t.Fatal(err)
	}
	if row := read(); row["resultSequence"] != want {
		t.Fatalf("rename created unread activity: %+v", row)
	}
}
