package boot

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

func TestCompositionIsStableAtOpenAIHTTPBoundary(t *testing.T) {
	var mu sync.Mutex
	var bodies []map[string]json.RawMessage
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]json.RawMessage
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		mu.Lock()
		bodies = append(bodies, body)
		mu.Unlock()
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintln(w, `data: {"id":"fixture","model":"x","choices":[{"index":0,"delta":{"content":"done"},"finish_reason":null}]}`)
		fmt.Fprintln(w)
		fmt.Fprintln(w, `data: {"id":"fixture","model":"x","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`)
		fmt.Fprintln(w)
		fmt.Fprintln(w, "data: [DONE]")
		fmt.Fprintln(w)
	}))
	defer server.Close()
	home, root := compositionTestWorkspace(t, "openai")
	writeFile(t, root, "reasonix.toml", fmt.Sprintf("default_model = \"test-model\"\n[[providers]]\nname=\"test-model\"\nkind=\"openai\"\nbase_url=%q\nmodel=\"x\"\n", server.URL+"/v1"))
	approveWorkspaceAt(t, home, root)
	saveReadOnlyComposition(t, home, root)
	built, err := BuildRuntime(t.Context(), Options{Home: home, WorkspaceRoot: root, Model: "test-model", AgentComposition: "user:review", resolvedShell: pinnedEffectShell()})
	if err != nil {
		t.Fatal(err)
	}
	defer built.Controller.Close()
	built.Controller.EnsureSessionPath()
	for _, prompt := range []string{"Read the available information.", "Summarize it briefly."} {
		if err := built.Controller.RunTurn(t.Context(), prompt); err != nil {
			t.Fatal(err)
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if len(bodies) < 2 {
		t.Fatalf("HTTP requests=%d", len(bodies))
	}
	var prefix, schemas string
	for _, body := range bodies {
		var messages []struct{ Role, Content string }
		if err := json.Unmarshal(body["messages"], &messages); err != nil {
			t.Fatal(err)
		}
		if len(messages) == 0 || messages[0].Role != "system" || !strings.Contains(messages[0].Content, "rigorous code reviewer") {
			t.Fatal("persona absent at wire boundary")
		}
		if prefix != "" && prefix != messages[0].Content {
			t.Fatal("same binding changed system bytes")
		}
		prefix = messages[0].Content
		if schemas != "" && schemas != string(body["tools"]) {
			t.Fatal("same binding changed tool schema bytes")
		}
		schemas = string(body["tools"])
		raw, _ := json.Marshal(body)
		if strings.Contains(string(raw), "agent_composition") || strings.Contains(string(raw), "user:review") {
			t.Fatal("binding metadata entered request")
		}
		for _, name := range []string{"bash", "task", "write_file", "use_capability"} {
			if strings.Contains(schemas, `"name":"`+name+`"`) {
				t.Fatalf("forbidden wire tool %s", name)
			}
		}
	}
}
