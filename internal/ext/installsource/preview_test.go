package installsource

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"unicode"
	"unicode/utf8"

	"reasonix/internal/base/testenv"
	"reasonix/internal/base/textutil"
	"reasonix/internal/ext/pluginpkg"
)

func hostileText(t *testing.T, s string) {
	t.Helper()
	if !utf8.ValidString(s) {
		t.Fatalf("invalid UTF-8: %q", s)
	}
	for _, r := range s {
		if r < 0x20 || r == 0x7f || (r >= 0x80 && r <= 0x9f) || unicode.Is(unicode.Cf, r) {
			t.Fatalf("hidden character %U in %q", r, s)
		}
	}
}

func TestPreviewActionBoundsAndMarksEveryClass(t *testing.T) {
	long := strings.Repeat("长", 5000)
	args := make([]string, 200)
	for i := range args {
		args[i] = "arg"
	}
	a := previewAction(action{
		Kind: "plugin", Name: "demo", Source: "/src/" + long, Command: "run\x1b[2J‮",
		Args: args, Env: map[string]string{"K​": "v\n" + long},
		Skills: []string{"ok", "bad\x1b[1m"}, Agents: make([]string, 500),
		RiskReasons: []string{strings.Repeat("a\n", 100)}, Warnings: []string{"w\x1b[31m"},
		Error: "e" + long, Next: "n‮",
		SkippedCapabilities: []pluginpkg.CompatibilityIssue{{Capability: "hook", Path: "p\x00", Reason: "r\x1b]0;x\x07" + long}},
		Runtime:             &RuntimePlanInfo{Command: "c\x1b", Args: []string{"x‮"}, FullTrust: true},
	})
	if !a.PreviewTruncated {
		t.Fatal("PreviewTruncated not set")
	}
	raw, _ := json.Marshal(a)
	var back map[string]any
	_ = json.Unmarshal(raw, &back)
	var walk func(v any)
	walk = func(v any) {
		switch x := v.(type) {
		case string:
			hostileText(t, strings.ReplaceAll(x, "\n", " "))
			if utf8.RuneCountInString(x) > 1100 {
				t.Fatalf("unbounded field of %d runes", utf8.RuneCountInString(x))
			}
		case []any:
			if len(x) > 100 {
				t.Fatalf("unbounded list of %d", len(x))
			}
			for _, e := range x {
				walk(e)
			}
		case map[string]any:
			for k, e := range x {
				walk(k)
				walk(e)
			}
		}
	}
	walk(back)
	if a.Command != `run\u{1b}[2J\u{202e}` || a.Skills[1] != `bad\u{1b}[1m` {
		t.Fatalf("executable fields must show hidden characters, got %q %q", a.Command, a.Skills[1])
	}
	if a.Warnings[0] != "w" || a.Next != "n" {
		t.Fatalf("prose not stripped: %q %q", a.Warnings[0], a.Next)
	}
	if len(a.Args) != maxLocatorItems || a.Runtime.Args[0] != `x\u{202e}` || !a.Runtime.FullTrust {
		t.Fatalf("args=%d runtime=%+v", len(a.Args), a.Runtime)
	}
}

func TestPreviewActionOrdinaryPlanIsUnchanged(t *testing.T) {
	in := action{
		Kind: "plugin", Action: "install_plugin_package", Status: "planned", Name: "审查工具", Version: "1.2.0",
		Source: "https://github.com/acme/tool", Target: `/home/用户/.reasonix/plugins/tool`,
		Command: "npx", Args: []string{"-y", "@acme/mcp", "--lang=中文"}, Env: map[string]string{"TOKEN": "${TOKEN}"},
		Skills: []string{"skills/代码审查"}, Agents: []string{"reviewer"}, RiskLevel: RiskHigh,
		RiskReasons: []string{"registers shell hooks that execute during Reasonix sessions"},
		Warnings:    []string{"first line\nsecond line 🚀"}, Next: "Review the plan.",
		SkippedCapabilities: []pluginpkg.CompatibilityIssue{{Capability: "lsp", Path: "a/b", Reason: "not supported 暂不支持"}},
		Runtime:             &RuntimePlanInfo{Command: "bin/x", Args: []string{"--serve"}, FullTrust: true},
	}
	got := previewAction(in)
	if got.PreviewTruncated {
		t.Fatal("ordinary plan flagged as truncated")
	}
	got.PreviewTruncated = false
	a, _ := json.Marshal(in)
	b, _ := json.Marshal(got)
	if string(a) != string(b) {
		t.Fatalf("projection changed an ordinary plan:\n%s\n%s", a, b)
	}
}

func TestPreviewResponseRollsUpActionFlag(t *testing.T) {
	r := previewResponse(response{Actions: []action{{PreviewTruncated: true}}, Warnings: []string{"x"}})
	if !r.PreviewTruncated {
		t.Fatal("envelope flag not rolled up")
	}
	if r := previewResponse(response{Warnings: []string{"x"}}); r.PreviewTruncated {
		t.Fatal("flag set without a cut")
	}
	r = previewResponse(response{Warnings: make([]string, 80), Next: "n\x1b[2J"})
	if !r.PreviewTruncated || len(r.Warnings) != maxProseItems || r.Next != "n" {
		t.Fatalf("%+v", r)
	}
}

func TestHostilePackagePlanIsBoundedAndPlanIDCoversFullText(t *testing.T) {
	hostile := func(tail string) string {
		src := testenv.TempDir(t)
		args, _ := json.Marshal([]string{"--serve", "\x1b[2K\x1b[1A" + strings.Repeat("A", 4000) + tail, "\u202e"})
		version, _ := json.Marshal("1.0.0\x1b[2J\u202e" + strings.Repeat("9", 500) + tail)
		writeFile(t, src+"/reasonix-plugin.json", `{"apiVersion":"reasonix.io/plugin/v2","name":"evil","version":`+string(version)+`,
"runtime":{"command":"${REASONIX_PLUGIN_ROOT}/bin/x","args":`+string(args)+`,"required":true,"intercepts":["input.receive"]}}`)
		writeFile(t, src+"/bin/x", "#!/bin/sh\n")
		tl := NewTool(Options{ProjectRoot: testenv.TempDir(t), HomeDir: testenv.TempDir(t)})
		raw, _ := json.Marshal(map[string]any{"source": src, "kind": "plugin"})
		out, err := tl.Execute(t.Context(), raw)
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	one, two := hostile("one"), hostile("two")
	var r1, r2 response
	if err := json.Unmarshal([]byte(one), &r1); err != nil {
		t.Fatal(err)
	}
	_ = json.Unmarshal([]byte(two), &r2)
	if !r1.PreviewTruncated || len(r1.Actions) != 1 || !r1.Actions[0].PreviewTruncated {
		t.Fatalf("hostile plan not flagged: %s", one)
	}
	if strings.ContainsAny(one, "\x1b‮") || strings.Contains(one, `\u001b`) || strings.Contains(one, `‮`) {
		t.Fatalf("raw hidden characters reached the JSON: %s", one)
	}
	if r1.PlanID == r2.PlanID || r1.ContentDigest == r2.ContentDigest && r1.ContentDigest != "" {
		t.Fatalf("two packages that differ only past the cut share a ticket: %s %s", r1.PlanID, r2.PlanID)
	}
}

func TestPreviewResponseCapsActionCount(t *testing.T) {
	acts := make([]action, 80)
	for i := range acts {
		acts[i].Name = "a"
	}
	r := previewResponse(response{Actions: acts})
	if len(r.Actions) != textutil.MaxActions || r.HiddenActions != 30 || r.PreviewTruncated {
		t.Fatalf("%d actions, truncated=%v", len(r.Actions), r.PreviewTruncated)
	}
	if r := previewResponse(response{Actions: acts[:textutil.MaxActions]}); len(r.Actions) != textutil.MaxActions || r.PreviewTruncated {
		t.Fatal("a plan at the cap is complete and must not be flagged")
	}
}

func TestExecuteAppliedReportsRealPathsWhileJSONIsBounded(t *testing.T) {
	src := testenv.TempDir(t)
	writeFile(t, src+"/SKILL.md", "---\nname: kit\ndescription: d\n---\nbody")
	home := filepath.Join(testenv.TempDir(t), "h​ome")
	tl := NewTool(Options{ProjectRoot: testenv.TempDir(t), HomeDir: home})
	raw, _ := json.Marshal(map[string]any{"source": src, "kind": "skill", "scope": "global", "apply": true})
	out, res, err := tl.ExecuteApplied(t.Context(), raw)
	applied := res.Applied
	if err != nil || len(applied) != 1 {
		t.Fatalf("applied=%v err=%v out=%s", applied, err, out)
	}
	if !strings.Contains(applied[0].Target, "h​ome") {
		t.Fatalf("applied target lost its real path: %q", applied[0].Target)
	}
	if strings.Contains(out, "​") {
		t.Fatal("projected JSON carries the raw character")
	}
}
