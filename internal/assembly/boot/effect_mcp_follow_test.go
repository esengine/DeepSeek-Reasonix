package boot

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/session/control"
)

// A server the user follows has its changed definitions reach the model without
// a click, the operator is told which ones, and unfollowing puts the next change
// back behind the user.
func TestEffectFollowingAServerTakesItsToolChangesAndAnnouncesThem(t *testing.T) {
	var rec *capabilityCallProvider
	provider.Register("boot-mcp-follow", func(provider.Config) (provider.Provider, error) { return rec, nil })
	home := isolateConfigHome(t)
	reasonixHome := filepath.Join(home, ".reasonix")
	t.Setenv("REASONIX_HOME", reasonixHome)
	workspace := robustTempDir(t)
	t.Chdir(workspace)
	server := newDriftingMCPServer(t)
	writeFile(t, workspace, "reasonix.toml", fmt.Sprintf(`
default_model = "test-model"
[agent]
system_prompt = "BASE"
[codegraph]
enabled = false
[[providers]]
name = "test-model"
kind = "boot-mcp-follow"
model = "x"
[[plugins]]
name = "drift"
type = "http"
url = %q
load = "always"
`, server.URL))
	approveWorkspace(t, workspace)
	approveProjectServer(t, workspace, "drift")

	session := func(sink event.Sink, direct string) (*control.Controller, *capabilityCallProvider) {
		rec = &capabilityCallProvider{direct: direct}
		ctrl, err := Build(t.Context(), Options{Sink: sink, Home: reasonixHome, WorkspaceRoot: workspace})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(ctrl.Close)
		if _, err := ctrl.ConnectMCPServer(ctrl.ConfiguredMCPServers()[0].Entry); err != nil {
			t.Fatal(err)
		}
		return ctrl, rec
	}
	noticeFor := func(sink *collectSink, code string) *event.Event {
		sink.mu.Lock()
		defer sink.mu.Unlock()
		for i := range sink.ev {
			if sink.ev[i].Code == code {
				return &sink.ev[i]
			}
		}
		return nil
	}

	first, _ := session(event.Discard, "mcp__drift__write")
	if _, err := first.SetMCPFollowToolChanges("drift", true); err != nil {
		t.Fatal(err)
	}
	if !first.ConfiguredMCPServers()[0].FollowsToolChanges {
		t.Fatal("the server row does not say it is followed")
	}
	first.Close()

	server.set(rewrittenDescription, true)
	sink := &collectSink{}
	second, rec2 := session(sink, "mcp__drift__extra")
	if held := second.ConfiguredMCPServers()[0].Held; len(held) != 0 {
		t.Fatalf("a followed server held tools: %+v", held)
	}
	if err := second.Run(t.Context(), "use the tool"); err != nil {
		t.Fatal(err)
	}
	if results := effectToolResults(rec2.last()); len(results) != 1 || !strings.Contains(results[0], "ran extra") || !server.called("extra") {
		t.Fatalf("a tool a followed server added did not run: %q", results)
	}
	if n := noticeFor(sink, event.NoticeCodeMCPToolsHeld); n != nil {
		t.Fatalf("a followed change was reported as held: %+v", n)
	}
	n := noticeFor(sink, event.NoticeCodeMCPToolsFollowed)
	if n == nil || !strings.Contains(n.Detail, "extra") || !strings.Contains(n.Detail, "write") {
		t.Fatalf("the change was taken silently: %+v", n)
	}

	if _, err := second.SetMCPFollowToolChanges("drift", false); err != nil {
		t.Fatal(err)
	}
	second.Close()
	server.set("THIRD_REWRITE_MARKER", true)
	third, rec3 := session(event.Discard, "mcp__drift__write")
	if held := third.ConfiguredMCPServers()[0].Held; len(held) != 1 || held[0].RawName != "write" {
		t.Fatalf("after unfollowing, held = %+v, want the changed write tool", held)
	}
	if err := third.Run(t.Context(), "use the tool"); err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(rec3.requests()[0].Tools)
	if strings.Contains(string(body), "THIRD_REWRITE_MARKER") {
		t.Fatal("an unfollowed change reached the provider request")
	}
	if results := effectToolResults(rec3.last()); len(results) != 1 || !strings.Contains(results[0], "mcp_tool_held") && !strings.Contains(results[0], "refusal") {
		t.Fatalf("calling the held tool = %q, want a refusal", results)
	}
	if server.callCount("write") != 0 {
		t.Fatalf("the held tool ran on the server: %d calls", server.callCount("write"))
	}
}
