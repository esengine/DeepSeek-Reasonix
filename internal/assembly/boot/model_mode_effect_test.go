package boot

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sync"
	"testing"

	"reasonix/internal/contract/event"
	"reasonix/internal/session/control"
)

// modeWire records every Responses body the real provider sends.
type modeWire struct {
	mu     sync.Mutex
	bodies []map[string]any
}

func (w *modeWire) serve(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		body := map[string]any{}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode request: %v", err)
		}
		w.mu.Lock()
		w.bodies = append(w.bodies, body)
		w.mu.Unlock()
		rw.Header().Set("Content-Type", "text/event-stream")
		_, _ = rw.Write([]byte(answeredResponsesSSE))
	}))
	t.Cleanup(srv.Close)
	return srv
}

// answeredResponsesSSE is one turn with a visible answer, so a run settles on
// its first request rather than retrying for one.
const answeredResponsesSSE = "data: {\"type\":\"response.output_text.delta\",\"item_id\":\"m1\",\"content_index\":0,\"delta\":\"ok\"}\n\n" +
	"data: {\"type\":\"response.output_text.done\",\"item_id\":\"m1\",\"content_index\":0,\"text\":\"ok\"}\n\n" +
	"data: {\"type\":\"response.completed\",\"response\":{\"id\":\"r1\",\"status\":\"completed\",\"output\":[]}}\n\n"

// loopBodies are the main loop's requests: the ones carrying the tool surface.
func (w *modeWire) loopBodies() []map[string]any {
	w.mu.Lock()
	defer w.mu.Unlock()
	var out []map[string]any
	for _, b := range w.bodies {
		if _, ok := b["tools"]; ok {
			out = append(out, b)
		}
	}
	return out
}

// buildModeSession assembles the real stack for one Responses provider whose
// wire is the recording server, while its base_url stays the vendor's host.
func buildModeSession(t *testing.T, wire *modeWire, baseURL, model string) *control.Controller {
	t.Helper()
	isolateConfigHome(t)
	dir := robustTempDir(t)
	t.Chdir(dir)
	t.Setenv("BOOT_MODE_TEST_KEY", "sk-test")
	srv := wire.serve(t)
	writeFile(t, dir, "reasonix.toml", `
default_model = "oai"

[agent]
system_prompt = "BASE"

[[providers]]
name = "oai"
kind = "responses"
base_url = "`+baseURL+`"
request_url = "`+srv.URL+`"
model = "`+model+`"
responses_mode = "stateless"
api_key_env = "BOOT_MODE_TEST_KEY"
`)
	approveWorkspace(t, dir)
	ctrl, err := Build(context.Background(), Options{Sink: event.Discard})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	t.Cleanup(ctrl.Close)
	return ctrl
}

func reasoningMode(body map[string]any) any {
	reasoning, _ := body["reasoning"].(map[string]any)
	return reasoning["mode"]
}

// samePrefix reports whether later carries earlier's cacheable prefix
// unchanged: the same instructions and tools, and earlier's input as its head.
func samePrefix(earlier, later map[string]any) bool {
	if !reflect.DeepEqual(earlier["instructions"], later["instructions"]) || !reflect.DeepEqual(earlier["tools"], later["tools"]) {
		return false
	}
	head, _ := earlier["input"].([]any)
	tail, _ := later["input"].([]any)
	return len(head) > 0 && len(tail) >= len(head) && reflect.DeepEqual(tail[:len(head)], head)
}

// Pro reaches the Responses body as reasoning.mode through the real assembly,
// and switching it on mid-conversation leaves the cacheable prefix — the
// instructions, the tools, the input already sent — byte-for-byte as it was.
func TestEffectProModeReachesTheResponsesRequest(t *testing.T) {
	wire := &modeWire{}
	ctrl := buildModeSession(t, wire, "https://api.openai.com/v1", "gpt-5.6-sol")
	modes := ctrl.ModelModes()
	if len(modes) != 1 || modes[0].ID != "pro" || !modes[0].Costlier || modes[0].Active {
		t.Fatalf("gpt-5.6-sol on the Responses API lists %+v, want one inactive costlier pro", modes)
	}
	if err := ctrl.Run(context.Background(), "reply ok"); err != nil {
		t.Fatalf("Run off: %v", err)
	}
	if err := ctrl.SetModelMode("pro"); err != nil {
		t.Fatalf("SetModelMode(pro): %v", err)
	}
	if !ctrl.ModelModes()[0].Active {
		t.Fatal("pro is not reported active after it was set")
	}
	if err := ctrl.Run(context.Background(), "reply ok again"); err != nil {
		t.Fatalf("Run on: %v", err)
	}
	bodies := wire.loopBodies()
	if len(bodies) < 2 {
		t.Fatalf("recorded %d loop requests, want one per run", len(bodies))
	}
	off, on := bodies[0], bodies[len(bodies)-1]
	if reasoningMode(off) != nil {
		t.Fatalf("mode off sent reasoning=%v", off["reasoning"])
	}
	if reasoningMode(on) != "pro" {
		t.Fatalf("mode on sent reasoning=%v, want mode pro", on["reasoning"])
	}
	if !samePrefix(off, on) {
		t.Fatalf("turning pro on moved the cacheable prefix:\noff=%v\non=%v", off, on)
	}
}

// A model or wire nobody declared pro for refuses it at the controller with a
// typed error, and the request never carries it even when the executor is
// told to directly: the adapter sends only what the endpoint declared.
func TestEffectUndeclaredModeNeverReachesTheRequest(t *testing.T) {
	for _, tc := range []struct{ name, baseURL, model string }{
		{"a relay serving gpt-6", "https://relay.example.com/v1", "gpt-6-sol"},
		{"a model with no modes", "https://api.openai.com/v1", "gpt-4.1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			wire := &modeWire{}
			ctrl := buildModeSession(t, wire, tc.baseURL, tc.model)
			if modes := ctrl.ModelModes(); modes != nil {
				t.Fatalf("lists %+v, want no modes", modes)
			}
			if err := ctrl.SetModelMode("pro"); !errors.Is(err, control.ErrModelModeUnsupported) {
				t.Fatalf("SetModelMode(pro) = %v, want ErrModelModeUnsupported", err)
			}
			ctrl.Executor().SetRequestMode("pro")
			if err := ctrl.Run(context.Background(), "reply ok"); err != nil {
				t.Fatalf("Run: %v", err)
			}
			if len(wire.loopBodies()) == 0 {
				t.Fatal("no loop request reached the wire")
			}
			for _, body := range wire.loopBodies() {
				if mode := reasoningMode(body); mode != nil {
					t.Fatalf("sent reasoning.mode=%v to an endpoint that declared none", mode)
				}
			}
		})
	}
}

// A rebuild carries the mode onto a model that declares it and drops it onto
// one that does not, so a model switch is what turns pro off.
func TestEffectModelSwitchCarriesOnlyADeclaredMode(t *testing.T) {
	from := buildModeSession(t, &modeWire{}, "https://api.openai.com/v1", "gpt-5.6-sol")
	if err := from.SetModelMode("pro"); err != nil {
		t.Fatalf("SetModelMode(pro): %v", err)
	}
	for _, tc := range []struct {
		model, want string
	}{{"gpt-6-sol", "pro"}, {"gpt-4.1", ""}} {
		to := buildModeSession(t, &modeWire{}, "https://api.openai.com/v1", tc.model)
		if err := ApplyRuntimeMigration(to, from, CaptureRuntimeMigration(from)); err != nil {
			t.Fatalf("ApplyRuntimeMigration to %s: %v", tc.model, err)
		}
		if got := to.ModelMode(); got != tc.want {
			t.Fatalf("switching to %s left mode %q, want %q", tc.model, got, tc.want)
		}
	}
}
