package serve

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"reasonix/internal/config"
	"reasonix/internal/control"
	"reasonix/internal/provider"
)

func TestProtocolRecoveryHTTPRejectsMissingAndStaleTokens(t *testing.T) {
	bc := NewBroadcaster()
	got := make(chan string, 1)
	ctrl := control.New(control.Options{Runner: fakeRunner{got: got}, Sink: bc})
	defer ctrl.Close()
	srv := httptest.NewServer(New(ctrl, bc, config.ServeConfig{}).Handler())
	defer srv.Close()
	for _, tc := range []struct {
		body   string
		status int
	}{
		{`{"action":"protocol_recovery"}`, 400},
		{`{"action":"protocol_recovery","recoveryId":"stale","input":"/new"}`, 409},
		{`{"action":"protocol_recovery","recoveryId":"stale","input":"/model other"}`, 409},
		{`{"action":"protocol_recovery","recoveryId":"stale","format":"json_object"}`, 400},
	} {
		resp, err := http.Post(srv.URL+"/submit", "application/json", strings.NewReader(tc.body))
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != tc.status {
			t.Fatalf("body=%s status=%d", tc.body, resp.StatusCode)
		}
	}
	select {
	case input := <-got:
		t.Fatalf("stale action reached runner: %q", input)
	default:
	}
}
func TestProtocolAndSearchHistoryPreserveDisplayWithoutProof(t *testing.T) {
	raw, _ := json.Marshal(provider.ProtocolRecoveryRecord{Version: 1, ID: "one", State: "pending", Fingerprint: "hash", Prefix: 1})
	got := historyMessages([]provider.Message{{LocalOnly: true, ProtocolRecovery: raw}, {Role: provider.RoleAssistant, Content: "summary", ServerSearch: []provider.ServerSearchCall{{ID: "s", SourcesStatus: provider.SourcesNotProvided, Raw: json.RawMessage(`{"opaque":"secret-proof"}`)}}}})
	b, _ := json.Marshal(got)
	if !strings.Contains(string(b), "not_provided") || !strings.Contains(string(b), "protocolRecovery") || strings.Contains(string(b), "secret-proof") {
		t.Fatalf("history=%s", b)
	}
}
