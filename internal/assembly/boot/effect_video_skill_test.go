package boot

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
)

// The video-production body reaches the model only when invoked; its catalog
// line rides the turn tail and the cache-stable prefix never moves.
func TestEffectVideoProductionSkillLoadsOnInvocationOnly(t *testing.T) {
	home := isolateConfigHome(t)
	t.Setenv("REASONIX_HOME", filepath.Join(home, ".reasonix"))
	workspace := robustTempDir(t)
	t.Chdir(workspace)
	writeFile(t, workspace, "reasonix.toml", listChangedConfig("boot-video-skill", ""))
	approveWorkspace(t, workspace)
	rec := &scriptedCallProvider{calls: []scriptedCall{
		{"use_capability", `{"action":"call","capability_id":"skill:video-production","arguments":{"arguments":"a ten second teaser"}}`},
	}}
	provider.Register("boot-video-skill", func(provider.Config) (provider.Provider, error) { return rec, nil })
	ctrl, err := Build(t.Context(), Options{Sink: event.Discard})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(ctrl.Close)
	if err := ctrl.Run(t.Context(), "make me a ten second teaser"); err != nil {
		t.Fatal(err)
	}
	if got := rec.resultOf(0); !strings.Contains(got, "Never claim a capability") || !strings.Contains(got, "submission_unknown") {
		t.Fatalf("invocation did not return the skill body: %.300s", got)
	}
	rec.mu.Lock()
	reqs := append([]provider.Request(nil), rec.reqs...)
	rec.mu.Unlock()
	listed := false
	first, _ := json.Marshal(reqs[0].Tools)
	for _, req := range reqs {
		system := systemMessage(req.Messages)
		if strings.Contains(system, "Never claim a capability") {
			t.Fatal("skill body entered the cached system prefix")
		}
		if system != systemMessage(reqs[0].Messages) {
			t.Fatal("the system prefix changed across the turn")
		}
		if current, _ := json.Marshal(req.Tools); !bytes.Equal(first, current) {
			t.Fatal("the tool prefix changed across the turn")
		}
		for _, m := range req.Messages[1:] {
			if m.Role != provider.RoleTool && strings.Contains(m.Content, "video-production") {
				listed = true
			}
		}
	}
	if !listed {
		t.Fatal("video-production was not listed at the turn tail")
	}
}
