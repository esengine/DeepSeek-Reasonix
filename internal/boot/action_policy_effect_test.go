package boot

// Effect test for the opt-in model action policy: asserts what actually reaches
// the provider boundary through the real Build stack, and that a build without
// the opt-in keeps a byte-identical system prefix.

import (
	"context"
	"strings"
	"testing"

	"reasonix/internal/config"
	"reasonix/internal/event"
	"reasonix/internal/provider"
)

// actionPolicySystemPrompt builds the real stack with the given provider block
// and returns the system message that reached the provider.
func actionPolicySystemPrompt(t *testing.T, kind, providerBlock string) string {
	t.Helper()
	isolateConfigHome(t)
	dir := robustTempDir(t)
	t.Chdir(dir)

	rec := &effectRecordingProvider{}
	provider.Register(kind, func(provider.Config) (provider.Provider, error) {
		return rec, nil
	})
	writeFile(t, dir, "reasonix.toml", `
default_model = "test-model"

[agent]
system_prompt = "BASE"

[environment]
enabled = false

[[providers]]
name = "test-model"
kind = "`+kind+`"
model = "x"
`+providerBlock+"\n")

	ctrl, err := Build(context.Background(), Options{Sink: event.Discard})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	defer ctrl.Close()
	if err := ctrl.Run(context.Background(), "reply ok"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	reqs := rec.requests()
	if len(reqs) == 0 {
		t.Fatal("no request reached the provider boundary")
	}
	for _, m := range reqs[0].Messages {
		if m.Role == "system" {
			return m.Content
		}
	}
	t.Fatal("no system message reached the provider boundary")
	return ""
}

func TestModelActionPolicyReachesProviderOnlyWhenOptedIn(t *testing.T) {
	for _, tc := range []struct {
		name  string
		kind  string
		block string
		want  bool
	}{
		{name: "off by default", kind: "action-policy-off", want: false},
		{
			name:  "model opt-in",
			kind:  "action-policy-model-on",
			block: `model_overrides = { "x" = { action_policy = true } }`,
			want:  true,
		},
		{
			name:  "explicit off",
			kind:  "action-policy-model-off",
			block: `model_overrides = { "x" = { action_policy = false } }`,
			want:  false,
		},
		{
			name:  "override for another model",
			kind:  "action-policy-other-model",
			block: `model_overrides = { "other" = { action_policy = true } }`,
			want:  false,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := actionPolicySystemPrompt(t, tc.kind, tc.block)
			n := strings.Count(got, config.ModelActionPolicy)
			switch {
			case tc.want && n != 1:
				t.Fatalf("policy reached the provider %d times, want exactly 1", n)
			case !tc.want && n != 0:
				t.Fatalf("policy reached the provider %d times, want 0", n)
			}
		})
	}
}

// The cache-stable prefix is the reason this feature is opt-in: a default build
// must be byte-identical to one that never knew about the policy.
func TestModelActionPolicyDefaultPrefixIsUnchanged(t *testing.T) {
	base := actionPolicySystemPrompt(t, "action-policy-base-a", "")
	again := actionPolicySystemPrompt(t, "action-policy-base-b", "")
	if base != again {
		t.Fatal("default system prefix is not reproducible across builds")
	}
	if strings.Contains(base, config.ModelActionPolicy) {
		t.Fatal("default build leaked the action policy into the prefix")
	}

	opted := actionPolicySystemPrompt(t, "action-policy-opted", `model_overrides = { "x" = { action_policy = true } }`)
	if opted == base {
		t.Fatal("opt-in did not change the prefix")
	}
	if strings.Replace(opted, "\n\n"+config.ModelActionPolicy, "", 1) != base {
		t.Fatal("opt-in changed more than inserting the policy paragraph")
	}
}
