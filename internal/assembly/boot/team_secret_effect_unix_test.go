//go:build darwin || linux

package boot

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/event"
	"reasonix/internal/session/team"
	"reasonix/internal/state/teamroster"
)

// A secret the user gave an agent reaches its shell through the real assembly
// and the real openai adapter, and no byte sent to the endpoint carries the
// value: not the message the person pasted it in, not what the command printed.
func TestEffectATeamSecretReachesTheShellAndNeverTheWire(t *testing.T) {
	home := statedBootHome(t)
	t.Setenv("REASONIX_HOME", home)
	dir := testenv.TempDir(t)
	store := teamroster.Store{Dir: testenv.TempDir(t)}
	var ada string
	if _, err := store.Update(context.Background(), func(r *teamroster.Roster) error {
		ada = r.AddAgent(teamroster.Agent{Name: "Ada", Main: true, CreatedBy: teamroster.ByUser}).ID
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	team.WatchSecrets(store)
	svc := team.New(team.Options{Store: store})
	const value = "sk-shop-effect-0123456789"
	if err := svc.UserSaveSecret(context.Background(), "SHOP_KEY", value, "the shop", []string{ada}, ""); err != nil {
		t.Fatal(err)
	}

	endpoint := &secretEndpoint{command: `printf 'key=%s' "$REASONIX_SECRET_SHOP_KEY"`}
	chat := httptest.NewServer(endpoint)
	t.Cleanup(chat.Close)
	writeFile(t, dir, "reasonix.toml", `
default_model = "test-model"
tool_approval = "yolo"

[agent]
system_prompt = "BASE"

[codegraph]
enabled = false

[[providers]]
name = "test-model"
kind = "openai"
base_url = "`+chat.URL+`"
model = "x"
api_key = "test-key"
`)
	approveWorkspaceAt(t, home, dir)
	ctrl, err := Build(context.Background(), Options{resolvedShell: pinnedEffectShell(), Home: home, WorkspaceRoot: dir, Sink: event.Discard, TeamSeat: team.Seat{Service: svc, Ref: team.SeatRef{Agent: ada}}})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	defer ctrl.Close()
	if err := ctrl.Run(context.Background(), "my shop key is "+value+", use it"); err != nil {
		t.Fatalf("Run: %v", err)
	}

	bodies := endpoint.captured()
	if len(bodies) < 2 {
		t.Fatalf("the endpoint saw %d requests, want the tool round and the answer", len(bodies))
	}
	for i, b := range bodies {
		if strings.Contains(b, value) {
			t.Fatalf("request %d put the secret on the wire: %s", i, b)
		}
	}
	if last := bodies[len(bodies)-1]; !strings.Contains(last, "key=[secret:SHOP_KEY]") || !strings.Contains(last, "my shop key is [secret:SHOP_KEY]") {
		t.Fatalf("the command did not read the secret, or the request does not say where it was: %s", last)
	}
}

// secretEndpoint asks for one shell command, then answers, the way an
// OpenAI-compatible endpoint streams.
type secretEndpoint struct {
	command string
	mu      sync.Mutex
	bodies  []string
}

func (e *secretEndpoint) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	e.mu.Lock()
	e.bodies = append(e.bodies, string(body))
	e.mu.Unlock()
	w.Header().Set("Content-Type", "text/event-stream")
	delta, finish := `{"content":"done"}`, "stop"
	if bytes.Contains(body, []byte(`"tools"`)) && !bytes.Contains(body, []byte(`"role":"tool"`)) {
		args, _ := json.Marshal(map[string]string{"command": e.command})
		call, _ := json.Marshal(map[string]any{"tool_calls": []any{map[string]any{"index": 0, "id": "c1", "type": "function", "function": map[string]string{"name": "bash", "arguments": string(args)}}}})
		delta, finish = string(call), "tool_calls"
	}
	fmt.Fprintf(w, "data: {\"id\":\"c\",\"object\":\"chat.completion.chunk\",\"choices\":[{\"index\":0,\"delta\":%s,\"finish_reason\":null}]}\n\n", delta)
	fmt.Fprintf(w, "data: {\"id\":\"c\",\"object\":\"chat.completion.chunk\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":%q}],\"usage\":{\"prompt_tokens\":10,\"completion_tokens\":1,\"total_tokens\":11}}\n\n", finish)
	fmt.Fprint(w, "data: [DONE]\n\n")
}

func (e *secretEndpoint) captured() []string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]string(nil), e.bodies...)
}
