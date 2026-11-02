package serve

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"reasonix/internal/contract/provider"
	"reasonix/internal/runtime/delegation"
	"reasonix/internal/session/control"
)

type subagentSource struct {
	control.SessionAPI
	runs    []delegation.RunInfo
	page    delegation.RunTranscript
	err     error
	gotRef  string
	gotFrom int
	gotMax  int
}

func (c *subagentSource) SubagentRuns() ([]delegation.RunInfo, error) { return c.runs, c.err }

func (c *subagentSource) SubagentTranscript(ref string, after, limit int) (delegation.RunTranscript, error) {
	c.gotRef, c.gotFrom, c.gotMax = ref, after, limit
	return c.page, c.err
}

func subagentMux(src *subagentSource) *http.ServeMux {
	mux := http.NewServeMux()
	(&Server{ctrl: src}).registerSubagentRoutes(mux)
	return mux
}

func get(mux *http.ServeMux, target string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, target, nil))
	return rec
}

func TestSubagentRunsAreListedWithTheirState(t *testing.T) {
	started := time.UnixMilli(1_760_000_000_000)
	src := &subagentSource{runs: []delegation.RunInfo{{Ref: "sa_1", Name: "audit", Kind: "task", Model: "deepseek/pro", State: delegation.RunRunning, StartedAt: started, UpdatedAt: started, ParentToolCallID: "call_9"}}}
	rec := get(subagentMux(src), "/subagents")
	var got []subagentRunView
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil || rec.Code != 200 {
		t.Fatalf("status %d body %s err %v", rec.Code, rec.Body.String(), err)
	}
	if len(got) != 1 || got[0].Ref != "sa_1" || got[0].State != "running" || got[0].Model != "deepseek/pro" || got[0].StartedAt != 1_760_000_000_000 || got[0].ParentCall != "call_9" {
		t.Fatalf("runs = %+v", got)
	}
	if rec := get(subagentMux(&subagentSource{}), "/subagents"); rec.Body.String() != "[]\n" {
		t.Fatalf("no runs must be an empty list, got %q", rec.Body.String())
	}
}

func TestSubagentTranscriptKeepsIndicesDropsTheSystemPromptAndMasksCredentials(t *testing.T) {
	src := &subagentSource{page: delegation.RunTranscript{
		Info:     delegation.RunInfo{Ref: "sa_1", State: delegation.RunRunning},
		After:    4,
		Next:     7,
		Total:    7,
		Live:     true,
		Messages: []provider.Message{{Role: provider.RoleSystem, Content: "persona"}, {Role: provider.RoleUser, Content: "task"}, {Role: provider.RoleAssistant, Content: "key is sk-abcdefghijklmnop1234"}},
	}}
	rec := get(subagentMux(src), "/subagents/sa_1/transcript?after=4&limit=3")
	var got subagentTranscriptView
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil || rec.Code != 200 {
		t.Fatalf("status %d body %s err %v", rec.Code, rec.Body.String(), err)
	}
	if src.gotRef != "sa_1" || src.gotFrom != 4 || src.gotMax != 3 {
		t.Fatalf("asked for %q from %d max %d", src.gotRef, src.gotFrom, src.gotMax)
	}
	if len(got.Messages) != 2 || got.Messages[0].Role != "user" || got.Messages[0].MsgIndex != 5 || got.Messages[1].MsgIndex != 6 {
		t.Fatalf("messages = %+v, want the user and assistant rows at their real indices 5 and 6", got.Messages)
	}
	if got.Messages[1].Content == "key is sk-abcdefghijklmnop1234" {
		t.Fatal("a credential left the host in a sub-agent transcript")
	}
	if !got.Live || got.Next != 7 || got.Total != 7 || got.After != 4 || got.Run.State != "running" {
		t.Fatalf("page = %+v", got)
	}
}

func TestSubagentRefusalsKeepTheirIdentity(t *testing.T) {
	for _, tc := range []struct {
		err    error
		status int
		code   string
	}{
		{delegation.ErrSubagentNotFound, http.StatusNotFound, "subagent.not_found"},
		{delegation.ErrSubagentNotOwned, http.StatusForbidden, "subagent.not_owned"},
		{errors.New("disk on fire"), http.StatusInternalServerError, "subagent.read_failed"},
	} {
		rec := get(subagentMux(&subagentSource{err: tc.err}), "/subagents/sa_1/transcript")
		var reason Reason
		_ = json.Unmarshal(rec.Body.Bytes(), &reason)
		if rec.Code != tc.status || reason.Code != tc.code {
			t.Fatalf("%v: status %d code %q, want %d %s", tc.err, rec.Code, reason.Code, tc.status, tc.code)
		}
	}
}

func TestSubagentTranscriptPageIsBounded(t *testing.T) {
	src := &subagentSource{}
	get(subagentMux(src), "/subagents/sa_1/transcript?limit=99999999")
	if src.gotMax != subagentPageMax {
		t.Fatalf("limit reached the store as %d, want the named ceiling %d", src.gotMax, subagentPageMax)
	}
	get(subagentMux(src), "/subagents/sa_1/transcript?after=-5&limit=junk")
	if src.gotFrom != 0 || src.gotMax != 0 {
		t.Fatalf("a negative offset and a junk limit reached the store as %d / %d, want 0 / 0 (the store's own default page)", src.gotFrom, src.gotMax)
	}
}
