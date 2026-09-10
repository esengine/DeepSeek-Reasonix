package serve

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"reasonix/internal/agent"
	"reasonix/internal/config"
	"reasonix/internal/control"
	"reasonix/internal/tool"
)

func recoveryHTTPFixture(t *testing.T) (*httptest.Server, control.ToolRecoverySnapshot) {
	t.Helper()
	bc := NewBroadcaster()
	a := agent.New(nil, tool.NewRegistry(), agent.NewSession("sys"), agent.Options{}, bc)
	c := control.New(control.Options{Executor: a, Sink: bc})
	t.Cleanup(c.Close)
	s := httptest.NewServer(New(c, bc, config.ServeConfig{}).Handler())
	t.Cleanup(s.Close)
	return s, c.ToolRecoverySnapshot()
}

func TestToolRecoveryHTTPRoutesAndFences(t *testing.T) {
	s, v := recoveryHTTPFixture(t)
	response, err := http.Get(s.URL + "/tool-recovery")
	if err != nil {
		t.Fatal(err)
	}
	var got control.ToolRecoverySnapshot
	if err := json.NewDecoder(response.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusOK || got.Revision != v.Revision || got.Calls == nil {
		t.Fatalf("snapshot=%+v status=%d", got, response.StatusCode)
	}
	valid := control.ToolRecoveryRequest{SessionPath: v.SessionPath, RuntimeEpoch: v.RuntimeEpoch, Revision: v.Revision, Action: "unsupported"}
	raw, _ := json.Marshal(valid)
	for _, tc := range []struct {
		name   string
		body   []byte
		path   string
		status int
	}{
		{"unsupported action", raw, "", 409},
		{"unknown field", []byte(`{"action":"inspect","unexpected":true}`), "", 400},
		{"stale session", raw, "/different/session.jsonl", 409},
		{"stale revision", []byte(`{"action":"confirm","revision":"stale"}`), "", 409},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req, _ := http.NewRequest(http.MethodPost, s.URL+"/tool-recovery", bytes.NewReader(tc.body))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set(expectedSessionPathHeader, tc.path)
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != tc.status {
				t.Fatalf("status=%d want=%d", resp.StatusCode, tc.status)
			}
		})
	}
}
