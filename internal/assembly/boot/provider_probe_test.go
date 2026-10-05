package boot

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"reasonix/internal/base/netclient"
	"reasonix/internal/contract/config"
)

func TestProbeProviderConnectionUsesRequestLocalCredentialAndNoTools(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if got := r.Header.Get("Authorization"); got != "Bearer draft-secret" {
			t.Errorf("authorization = %q", got)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		if _, ok := body["tools"]; ok {
			t.Errorf("probe request exposed tools: %+v", body["tools"])
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"OK\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
	}))
	t.Cleanup(server.Close)
	entry := config.ProviderEntry{Name: "draft", Kind: "openai", BaseURL: server.URL + "/v1", Model: "chat", APIKeyEnv: "DRAFT_KEY"}
	if err := ProbeProviderConnection(context.Background(), entry, "draft-secret", netclient.ProxySpec{Mode: netclient.ModeOff}); err != nil {
		t.Fatal(err)
	}
	if requests != 1 {
		t.Fatalf("requests = %d, want 1", requests)
	}
}
