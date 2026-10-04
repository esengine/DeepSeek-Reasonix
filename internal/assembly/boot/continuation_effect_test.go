package boot

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"reasonix/internal/contract/event"
)

func TestEffectResponsesContinuationRecovery(t *testing.T) {
	home := isolateConfigHome(t)
	t.Setenv("REASONIX_HOME", home)
	dir := robustTempDir(t)
	t.Chdir(dir)
	t.Setenv("BOOT_CONTINUATION_TEST_KEY", "sk-test")
	var mu sync.Mutex
	var bodies []map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		mu.Lock()
		bodies = append(bodies, body)
		mu.Unlock()
		if body["previous_response_id"] != nil {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = io.WriteString(w, "previous_response_id is not available for this user")
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, answeredResponsesSSE)
	}))
	t.Cleanup(srv.Close)
	writeUserConfig(t, `
default_model = "relay"
[agent]
system_prompt = "BASE"
[[providers]]
name = "relay"
kind = "responses"
base_url = "`+srv.URL+`"
model = "gpt-6.1-sol"
api_key_env = "BOOT_CONTINUATION_TEST_KEY"
`)
	ctrl, err := Build(context.Background(), Options{Sink: event.Discard, Home: home, WorkspaceRoot: dir, Model: "relay"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(ctrl.Close)
	for _, prompt := range []string{"reply ok", "reply ok again", "reply ok once more"} {
		if err := ctrl.Run(context.Background(), prompt); err != nil {
			t.Fatalf("Run: %v", err)
		}
	}
	mu.Lock()
	defer mu.Unlock()
	var loop []map[string]any
	for _, body := range bodies {
		if body["tools"] != nil {
			loop = append(loop, body)
		}
	}
	if len(loop) != 4 || loop[1]["previous_response_id"] == nil || loop[2]["previous_response_id"] != nil || loop[3]["previous_response_id"] != nil {
		t.Fatalf("want initial, rejected continuation, recovery, stateless turn: %v", loop)
	}
	if !samePrefix(loop[0], loop[2]) || !samePrefix(loop[2], loop[3]) {
		t.Fatal("full-history recovery moved the cacheable prefix")
	}
}
