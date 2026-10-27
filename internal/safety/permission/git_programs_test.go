package permission

import (
	"context"
	"encoding/json"
	"testing"
)

func TestGitReadInARepositoryThatRunsProgramsIsAskedAbout(t *testing.T) {
	probe := func(carried map[string][]string) func(context.Context, string) []string {
		return func(_ context.Context, dir string) []string { return carried[dir] }
	}
	for _, c := range []struct {
		name    string
		repos   map[string][]string
		command string
		asked   bool
	}{
		{"plain repository", nil, "git diff", false},
		{"repository with a text conversion", map[string][]string{"": {"diff.x.textconv"}}, "git diff", true},
		{"a ref-only read is unaffected", map[string][]string{"": {"core.fsmonitor"}}, "git rev-parse HEAD", false},
		{"the named repository is the one inspected", map[string][]string{"sub": {"core.fsmonitor"}}, "git -C sub status", true},
		{"another repository's keys do not count", map[string][]string{"sub": {"core.fsmonitor"}}, "git status", false},
		{"a directory change hides the repository", nil, "cd /elsewhere && git diff", true},
	} {
		approver := &stubApprover{allow: true}
		gate := NewGate(Policy{Mode: Ask, RepoPrograms: probe(c.repos)}, approver)
		args, _ := json.Marshal(map[string]string{"command": c.command})
		v, err := gate.Verdict(context.Background(), "bash", args, true)
		if err != nil || !v.Allow {
			t.Fatalf("%s: verdict %+v, %v", c.name, v, err)
		}
		if asked := approver.calls > 0; asked != c.asked {
			t.Errorf("%s: asked=%v, want %v", c.name, asked, c.asked)
		}
	}
}
