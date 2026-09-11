package main

import (
	"encoding/json"
	"reasonix/internal/provider"
	"strings"
	"testing"
)

func TestProtocolRecoveryHistoryVersionAndConsumption(t *testing.T) {
	for _, tc := range []struct {
		version int
		state   string
		visible bool
	}{{1, "pending", true}, {1, "consumed", false}, {99, "pending", false}} {
		raw, _ := json.Marshal(provider.ProtocolRecoveryRecord{Version: tc.version, State: tc.state, ID: "fault", Prefix: 2, Fingerprint: "hash"})
		got := historyMessages([]provider.Message{{LocalOnly: true, ProtocolRecovery: raw}}, func(s string) string { return s })
		if (len(got) > 0) != tc.visible {
			t.Fatalf("state=%+v history=%+v", tc, got)
		}
		if tc.visible && (got[0].ProtocolRecovery == nil || got[0].ProtocolRecovery.ID != "fault") {
			t.Fatal("lost action token")
		}
	}
	got := historyMessages([]provider.Message{{Role: provider.RoleAssistant, Content: "summary", ServerSearch: []provider.ServerSearchCall{{ID: "s", SourcesStatus: provider.SourcesNotProvided, Raw: json.RawMessage(`{"opaque":"private"}`)}}}}, func(s string) string { return s })
	raw, _ := json.Marshal(got)
	if !strings.Contains(string(raw), "not_provided") || strings.Contains(string(raw), "private") {
		t.Fatalf("search history=%s", raw)
	}
}
