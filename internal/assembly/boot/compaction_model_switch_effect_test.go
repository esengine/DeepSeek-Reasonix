package boot

import (
	"context"
	"strings"
	"testing"

	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
)

func digestRequests(reqs []provider.Request) (summarizer, carrying int) {
	for _, req := range reqs {
		if len(req.Messages) > 0 && strings.Contains(req.Messages[0].Content, "compacting the earlier part") {
			summarizer++
			continue
		}
		for _, m := range req.Messages {
			if strings.Contains(m.Content, "<compaction-summary>") {
				carrying++
				break
			}
		}
	}
	return summarizer, carrying
}

// A model switch rebuilds the controller on the same transcript. The projection
// the old model earned is content, not model state: the next request must still
// carry it, and no summarization call may be spent re-deriving it.
func TestEffectModelSwitchKeepsTheCompactionProjection(t *testing.T) {
	isolateConfigHome(t)
	dir := robustTempDir(t)
	t.Chdir(dir)

	rec := &compactionEffectProvider{bulk: strings.Repeat("work output line with detail. ", 400)}
	provider.Register("boot-compaction-switch", func(provider.Config) (provider.Provider, error) {
		return rec, nil
	})
	writeFile(t, dir, "reasonix.toml", `
default_model = "model-a"

[agent]
system_prompt = "BASE"
compact_ratio = 0.5
recent_keep = 2

[[providers]]
name = "model-a"
kind = "boot-compaction-switch"
model = "a"
context_window = 32000

[[providers]]
name = "model-b"
kind = "boot-compaction-switch"
model = "b"
context_window = 32000
`)
	approveWorkspace(t, dir)

	from, err := Build(context.Background(), Options{Sink: event.Discard})
	if err != nil {
		t.Fatalf("Build a: %v", err)
	}
	defer from.Close()
	from.EnsureSessionPath()
	for _, prompt := range []string{"start the task", "second", "keep going", "keep going", "keep going", "keep going", "keep going"} {
		if err := from.Run(context.Background(), prompt); err != nil {
			t.Fatalf("Run(%q): %v", prompt, err)
		}
	}
	if _, carrying := digestRequests(rec.requests()); carrying == 0 {
		t.Fatal("fixture never compacted")
	}
	if err := from.Snapshot(); err != nil {
		t.Fatalf("Snapshot: %v", err)
	}

	to, err := Build(context.Background(), Options{Model: "model-b", Sink: event.Discard})
	if err != nil {
		t.Fatalf("Build b: %v", err)
	}
	defer to.Close()
	if err := ApplyRuntimeMigration(to, from, CaptureRuntimeMigration(from)); err != nil {
		t.Fatalf("ApplyRuntimeMigration: %v", err)
	}
	before := len(rec.requests())
	summBefore, _ := digestRequests(rec.requests())
	if err := to.Run(context.Background(), "after the switch"); err != nil {
		t.Fatalf("Run after switch: %v", err)
	}
	after := rec.requests()[before:]
	summ, carrying := digestRequests(after)
	if summ != 0 {
		t.Fatalf("switch spent %d summarization call(s) (had %d before)", summ, summBefore)
	}
	if carrying == 0 {
		t.Fatalf("request after the switch dropped the projection; messages=%s", messageDigest(after[len(after)-1].Messages))
	}
}

// The covered-prefix hash includes the system message, and a switch splices the
// rebuilt model's system prompt over the carried one. Every provider kind must
// therefore assemble the same prompt, or a switch would invalidate the projection.
func TestBuildAssemblesTheSameSystemPromptForEveryProviderKind(t *testing.T) {
	isolateConfigHome(t)
	dir := robustTempDir(t)
	t.Chdir(dir)
	t.Setenv("BOOT_KIND_TEST_KEY", "sk-test")
	writeFile(t, dir, "reasonix.toml", `
default_model = "oai"

[agent]
system_prompt = "BASE"

[[providers]]
name = "oai"
kind = "openai"
base_url = "https://api.openai.com/v1"
model = "gpt-5"
api_key_env = "BOOT_KIND_TEST_KEY"

[[providers]]
name = "ant"
kind = "anthropic"
base_url = "https://api.anthropic.com"
model = "claude-opus-4"
api_key_env = "BOOT_KIND_TEST_KEY"

[[providers]]
name = "resp"
kind = "responses"
base_url = "https://api.openai.com/v1"
model = "gpt-5.6-sol"
api_key_env = "BOOT_KIND_TEST_KEY"
`)
	approveWorkspace(t, dir)

	var want string
	for _, name := range []string{"oai", "ant", "resp"} {
		ctrl, err := Build(context.Background(), Options{Model: name, Sink: event.Discard})
		if err != nil {
			t.Fatalf("Build %s: %v", name, err)
		}
		history := ctrl.History()
		ctrl.Close()
		if len(history) == 0 || history[0].Role != provider.RoleSystem {
			t.Fatalf("%s: no system message", name)
		}
		if want == "" {
			want = history[0].Content
		} else if history[0].Content != want {
			t.Fatalf("%s assembled a different system prompt than the first kind", name)
		}
	}
}
