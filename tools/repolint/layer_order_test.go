package main

import (
	"strings"
	"testing"
)

func TestLayerOrderAllowsOnlyDownwardAndPeerImports(t *testing.T) {
	imports := map[string][]importRef{
		"internal/runtime/agent/a.go": {
			{path: "reasonix/internal/contract/provider", line: 3},
			{path: "reasonix/internal/safety/sandbox", line: 4},
			{path: "reasonix/internal/session/control", line: 5},
		},
		"internal/tools/builtin/b.go": {
			{path: "reasonix/internal/safety/sandbox", line: 3},
			{path: "fmt", line: 4},
		},
		"internal/contract/event/e.go": {{path: "reasonix/internal/model/billing", line: 7}},
		"internal/stray/x.go":          {{path: "reasonix/internal/base/fileutil", line: 2}},
		"cmd/tool/main.go":             {{path: "reasonix/internal/frontend/cli", line: 2}},
	}
	got := map[string]string{}
	for _, f := range checkLayerOrder(imports) {
		got[f.File] = f.Msg
	}
	if len(got) != 3 {
		t.Fatalf("findings = %v, want the upward import from runtime, the one from contract, and the stray package", got)
	}
	if !strings.Contains(got["internal/runtime/agent/a.go"], "internal/session/control (session)") {
		t.Errorf("runtime → session: %q", got["internal/runtime/agent/a.go"])
	}
	if !strings.Contains(got["internal/contract/event/e.go"], "internal/model/billing (model)") {
		t.Errorf("contract → model: %q", got["internal/contract/event/e.go"])
	}
	if !strings.Contains(got["internal/stray/x.go"], "not in a layer directory") {
		t.Errorf("stray package: %q", got["internal/stray/x.go"])
	}
}

func TestLayerOrderKeepsSameRankDirectoriesIndependent(t *testing.T) {
	for _, tc := range []struct {
		pkg, dep string
		want     bool
	}{
		{"internal/state/store", "internal/tools/builtin", true},
		{"internal/safety/sandbox", "internal/ext/plugin", true},
		{"internal/model/laya", "internal/platform/remote", true},
		{"internal/state/store", "internal/safety/sandbox", true},
		{"internal/tools/builtin", "internal/ext/skill", true},
		{"internal/ext/skill", "internal/tools/builtin", true},
		{"internal/tools/builtin", "internal/platform/browser", false},
		{"internal/platform/remote", "internal/state/store", false},
		{"internal/model/laya", "internal/safety/typesafe", false},
		{"internal/runtime/agent", "internal/runtime/delegation", false},
	} {
		imports := map[string][]importRef{tc.pkg + "/a.go": {{path: modulePrefix + tc.dep, line: 3}}}
		if got := len(checkLayerOrder(imports)) > 0; got != tc.want {
			t.Errorf("%s → %s: violation = %v, want %v", tc.pkg, tc.dep, got, tc.want)
		}
	}
}
