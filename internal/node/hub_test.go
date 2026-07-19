package node

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"reasonix/internal/mobileprotocol"
)

func TestEventRingReplayAndStale(t *testing.T) {
	r := NewEventRing(3)
	for i := 0; i < 5; i++ {
		r.Append([]byte{byte(i)})
	}
	// Ring keeps last 3: seq 3,4,5. lastAck=1 is stale.
	if _, ok := r.ReplaySince(1); ok {
		t.Fatal("expected stale cursor")
	}
	frames, ok := r.ReplaySince(3)
	if !ok {
		t.Fatal("expected replay")
	}
	if len(frames) != 2 || frames[0].Seq != 4 || frames[1].Seq != 5 {
		t.Fatalf("frames=%+v", frames)
	}
}

func TestRequestDedupeIdempotent(t *testing.T) {
	d := NewRequestDedupe(2)
	d.Remember("a", []byte("one"))
	d.Remember("a", []byte("two"))
	e, ok := d.Lookup("a")
	if !ok || string(e.Response) != "one" {
		t.Fatalf("got %+v ok=%v", e, ok)
	}
	d.Remember("b", []byte("b"))
	d.Remember("c", []byte("c"))
	if _, ok := d.Lookup("a"); ok {
		t.Fatal("oldest entry should have been evicted")
	}
}

func TestHubCreateAndSubmitDedupe(t *testing.T) {
	h := NewHub("test-node")
	create := commandEnv("", "req-create", mobileprotocol.CmdCreateSession, mobileprotocol.CreateSessionArgs{
		Runtime: mobileprotocol.RuntimeRemote,
		Title:   "demo",
	})
	resp1 := h.HandleCommand(create)
	if resp1.Type != mobileprotocol.TypeSnapshot || resp1.SessionID == "" {
		t.Fatalf("create resp=%+v", resp1)
	}
	// Create dedupe is session-scoped after the first response assigns a
	// SessionID; submit is the critical write path for requestId retries.
	sid := resp1.SessionID

	submit := commandEnv(sid, "req-submit-1", mobileprotocol.CmdSubmit, mobileprotocol.SubmitArgs{Text: "hello"})
	a := h.HandleCommand(submit)
	b := h.HandleCommand(submit)
	if a.Type != mobileprotocol.TypeAck || b.Type != mobileprotocol.TypeAck {
		t.Fatalf("a=%+v b=%+v", a, b)
	}
	ab, _ := json.Marshal(a)
	bb, _ := json.Marshal(b)
	if !bytes.Equal(ab, bb) {
		t.Fatalf("dedupe mismatch:\n%s\n%s", ab, bb)
	}
	if a.SessionID != sid || b.SessionID != sid {
		t.Fatal("session id must not jump across retries")
	}
}

func TestHubHTTPCommand(t *testing.T) {
	h := NewHub("http-node")
	srv := httptest.NewServer(h.Handler())
	t.Cleanup(srv.Close)

	body, _ := json.Marshal(commandEnv("", "r1", mobileprotocol.CmdCreateSession, mobileprotocol.CreateSessionArgs{
		Title: "via-http",
	}))
	res, err := http.Post(srv.URL+"/mobile/command", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		t.Fatalf("status=%d", res.StatusCode)
	}
	var env mobileprotocol.Envelope
	if err := json.NewDecoder(res.Body).Decode(&env); err != nil {
		t.Fatal(err)
	}
	if env.SessionID == "" || env.Type != mobileprotocol.TypeSnapshot {
		t.Fatalf("env=%+v", env)
	}

	health, err := http.Get(srv.URL + "/healthz")
	if err != nil {
		t.Fatal(err)
	}
	defer health.Body.Close()
	if health.StatusCode != 200 {
		t.Fatalf("health=%d", health.StatusCode)
	}
}

func TestHubRejectsLocalRuntimeOnNode(t *testing.T) {
	h := NewHub("n")
	resp := h.HandleCommand(commandEnv("", "x", mobileprotocol.CmdCreateSession, mobileprotocol.CreateSessionArgs{
		Runtime: mobileprotocol.RuntimeLocal,
	}))
	if resp.Type != mobileprotocol.TypeError {
		t.Fatalf("expected error, got %+v", resp)
	}
}

func commandEnv(sessionID, requestID, name string, args any) mobileprotocol.Envelope {
	raw, _ := json.Marshal(args)
	cmd := mobileprotocol.CommandPayload{Name: name, Args: raw}
	env := mobileprotocol.NewEnvelope(mobileprotocol.TypeCommand)
	env.RequestID = requestID
	env.SessionID = sessionID
	_ = env.MarshalPayload(cmd)
	return env
}
