package boot

import (
	"context"
	"strings"
	"testing"
	"time"

	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/session/control"
)

// A user-typed inline skill invocation must reach the provider in the same
// pinned form run_skill returns, on both the string and the structured submit path.
func TestEffectInlineSkillInvocationReachesProviderPinned(t *testing.T) {
	const pinOpen = "<skill-pin name=\"probe\">\n# Skill: probe"
	cases := []struct {
		name   string
		submit func(*control.Controller)
		tail   string
		raw    string
		other  bool
	}{
		{"slash bare", func(c *control.Controller) { c.Submit("/probe") }, "PROBE BODY\nthen run /other\n</skill-pin>", "/probe", false},
		{"slash with task", func(c *control.Controller) { c.Submit("/probe tidy the notes") }, "PROBE BODY\nthen run /other\n\nArguments: tidy the notes\n</skill-pin>", "/probe tidy the notes", false},
		{"chip bare", func(c *control.Controller) {
			c.SubmitInvocationDisplay("/probe", "", []control.InvocationRequest{{Name: "probe", Kind: "skill"}})
		}, "PROBE BODY\nthen run /other\n</skill-pin>", "", false},
		{"chip with task", func(c *control.Controller) {
			c.SubmitInvocationDisplay("/probe tidy the notes", "tidy the notes", []control.InvocationRequest{{Name: "probe", Kind: "skill"}})
		}, "PROBE BODY\nthen run /other\n\nArguments: tidy the notes\n</skill-pin>", "tidy the notes", false},
		{"chip mid-line, queued", func(c *control.Controller) {
			c.EnsureSessionPath()
			if _, err := c.TryEnqueueFollowup(control.InboxRequest{
				Display: "tidy /probe the notes", Submit: "tidy the notes", Raw: "tidy the notes",
				Invocations: []control.InvocationRequest{{Name: "probe", Kind: "skill", Offset: 5}},
			}); err != nil {
				panic(err)
			}
		}, "PROBE BODY\nthen run /other\n\nArguments: tidy the notes\n</skill-pin>", "tidy the notes", false},
		{"two chips bare", func(c *control.Controller) {
			c.SubmitInvocationDisplay("/probe /other", "", []control.InvocationRequest{{Name: "probe", Kind: "skill"}, {Name: "other", Kind: "skill"}})
		}, "PROBE BODY\nthen run /other\n</skill-pin>", "", true},
		{"two chips with task", func(c *control.Controller) {
			c.SubmitInvocationDisplay("/probe /other tidy the notes", "tidy the notes", []control.InvocationRequest{{Name: "probe", Kind: "skill"}, {Name: "other", Kind: "skill"}})
		}, "PROBE BODY\nthen run /other\n</skill-pin>", "tidy the notes", true},
	}
	var rec *effectRecordingProvider
	provider.Register("boot-effect-skill-invocation", func(provider.Config) (provider.Provider, error) { return rec, nil })
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			isolateConfigHome(t)
			dir := robustTempDir(t)
			t.Chdir(dir)
			rec = &effectRecordingProvider{}
			writeFile(t, dir, "reasonix.toml", `
default_model = "test-model"

[agent]
system_prompt = "BASE"

[environment]
enabled = false

[[providers]]
name = "test-model"
kind = "boot-effect-skill-invocation"
model = "x"
`)
			approveWorkspace(t, dir)
			writeFile(t, dir, ".reasonix/skills/probe/SKILL.md", "---\ndescription: probe skill\ntriggers: probe, tidy\nauto-use: require\n---\nPROBE BODY\nthen run /other")
			writeFile(t, dir, ".reasonix/skills/other/SKILL.md", "---\ndescription: other skill\ntriggers: other\nauto-use: require\n---\nOTHER BODY\nthen run /third")
			writeFile(t, dir, ".reasonix/skills/third/SKILL.md", "---\ndescription: third skill\ntriggers: third\nauto-use: require\n---\nTHIRD BODY")
			ctrl, err := Build(context.Background(), Options{Sink: event.Discard})
			if err != nil {
				t.Fatalf("Build: %v", err)
			}
			defer ctrl.Close()

			tc.submit(ctrl)
			deadline := time.Now().Add(30 * time.Second)
			for ctrl.Running() {
				if time.Now().After(deadline) {
					t.Fatal("turn did not finish")
				}
				time.Sleep(time.Millisecond)
			}
			reqs := rec.requests()
			if len(reqs) == 0 {
				t.Fatal("no request reached the provider boundary")
			}
			req := reqs[len(reqs)-1]
			if raw := rec.rawUserInputs(); len(raw) == 0 || raw[len(raw)-1] != tc.raw {
				t.Fatalf("provider raw user input = %q, want %q", raw, tc.raw)
			}
			var user string
			for _, m := range req.Messages {
				if m.Role == provider.RoleUser {
					user = m.Content
				}
			}
			if !strings.Contains(user, pinOpen) || !strings.Contains(user, tc.tail) {
				t.Fatalf("user message is not the pinned invocation (want tail %q):\n%s", tc.tail, user)
			}
			if got := strings.Count(user, "<skill-pin name=\"probe\">"); got != 1 {
				t.Fatalf("probe pin count = %d, want 1:\n%s", got, user)
			}
			if got := strings.Count(user, "<skill-pin name=\"other\">"); (got == 1) != tc.other {
				t.Fatalf("other pin count = %d, want one = %t:\n%s", got, tc.other, user)
			}
			if strings.Count(user, "tidy the notes") > 1 {
				t.Fatalf("task delivered twice:\n%s", user)
			}
			if strings.Contains(systemMessage(req.Messages), "PROBE BODY") {
				t.Fatal("invocation body leaked into the cache-stable prefix")
			}
			if strings.Contains(user, "<capability-route") {
				t.Fatalf("inline skill body must not create a capability route:\n%s", user)
			}
		})
	}
}
