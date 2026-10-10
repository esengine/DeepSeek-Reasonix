package boot

import (
	"context"
	"strings"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/provider"
)

// In auto mode a Chinese turn must carry a response-language block at the tail
// while the system prefix stays byte-stable and an English turn gets none.
func TestEffectAutoResponseLanguageFollowsTheTurn(t *testing.T) {
	t.Parallel()
	home := statedBootHome(t)
	dir := testenv.TempDir(t)
	rec := &effectRecordingProvider{}
	provider.Register("boot-response-language", func(provider.Config) (provider.Provider, error) { return rec, nil })
	writeFile(t, dir, "reasonix.toml", `
default_model = "test-model"

[[providers]]
name = "test-model"
kind = "boot-response-language"
model = "x"
`)
	approveWorkspaceAt(t, home, dir)
	ctrl, err := Build(context.Background(), Options{Home: home, WorkspaceRoot: dir})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	defer ctrl.Close()

	turns := []string{"请帮我解释一下这个函数为什么会超时", "explain why this function times out", "请帮我解释一下这个函数为什么会超时"}
	for _, in := range turns {
		if err := ctrl.Run(context.Background(), in); err != nil {
			t.Fatalf("turn %q: %v", in, err)
		}
	}
	reqs := agentRequests(rec.requests())
	if len(reqs) != 3 {
		t.Fatalf("want 3 requests, got %d", len(reqs))
	}
	zh1, en, zh2 := lastUserOf(reqs[0]), lastUserOf(reqs[1]), lastUserOf(reqs[2])
	if !strings.Contains(zh1, "\n<response-language>\n") || !strings.Contains(zh1, "简体中文") {
		t.Errorf("Chinese turn carries no response-language block:\n%s", zh1)
	}
	if strings.Contains(en, "\n<response-language>\n") {
		t.Errorf("English turn got a response-language block:\n%s", en)
	}
	if responseLanguageBodyOf(zh1) == "" || responseLanguageBodyOf(zh1) != responseLanguageBodyOf(zh2) {
		t.Errorf("same input projected different blocks:\n%s\n---\n%s", responseLanguageBodyOf(zh1), responseLanguageBodyOf(zh2))
	}
	if systemOf(reqs[0]) != systemOf(reqs[1]) || systemOf(reqs[0]) != systemOf(reqs[2]) {
		t.Error("response language moved the cache-stable system prefix")
	}
	if strings.Contains(systemOf(reqs[0]), "\n<response-language>\n") {
		t.Error("response-language block leaked into the system prefix")
	}
}

func responseLanguageBodyOf(msg string) string {
	_, rest, ok := strings.Cut(msg, "\n<response-language>\n")
	if !ok {
		return ""
	}
	body, _, _ := strings.Cut(rest, "</response-language>")
	return body
}
