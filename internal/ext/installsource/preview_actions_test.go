package installsource

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/base/textutil"
)

func TestPreviewResponseNeverHidesHighRisk(t *testing.T) {
	acts := make([]action, 0, 90)
	for i := range 70 {
		acts = append(acts, action{Name: fmt.Sprintf("m%02d", i), RiskLevel: RiskMedium})
	}
	for i := range 20 {
		acts = append(acts, action{Name: fmt.Sprintf("zz%02d", i), RiskLevel: RiskHigh})
	}
	r := previewResponse(response{Actions: acts})
	high, other := 0, 0
	for _, a := range r.Actions {
		if a.RiskLevel == RiskHigh {
			high++
		} else {
			other++
		}
	}
	if high != 20 || other != textutil.MaxActions || r.HiddenActions != 20 || r.PreviewTruncated {
		t.Fatalf("high=%d other=%d hidden=%d truncated=%v", high, other, r.HiddenActions, r.PreviewTruncated)
	}
	if r := previewResponse(response{Actions: acts[:textutil.MaxActions]}); r.HiddenActions != 0 || r.PreviewTruncated {
		t.Fatal("a plan at the cap hides nothing")
	}
}

func TestThemesOnlyReadsEveryAction(t *testing.T) {
	themes := make([]action, 0, 51)
	for i := range 50 {
		themes = append(themes, action{Kind: "plugin", Name: fmt.Sprintf("a%02d", i), ThemeCount: 1})
	}
	if !themesOnly(themes) {
		t.Fatal("fifty theme-only packages are themes only")
	}
	if themesOnly(append(themes, action{Kind: "plugin", Name: "zz", ThemeCount: 1, HookCount: 1})) {
		t.Fatal("a hook past the display cap still disqualifies the plan")
	}
	if themesOnly(append(themes, action{Kind: "plugin", Name: "zz", ThemeCount: 1, Runtime: &RuntimePlanInfo{}})) {
		t.Fatal("a runtime past the display cap still disqualifies the plan")
	}
	if themesOnly(append(themes[:2:2], action{Kind: "mcp"})) {
		t.Fatal("a non-plugin step disqualifies the plan")
	}
	if themesOnly(nil) || themesOnly([]action{{Kind: "skill", ThemeCount: 1}}) || themesOnly([]action{{Kind: "plugin"}}) {
		t.Fatal("empty, non-plugin and theme-less plans are not themes only")
	}
}

func marketplaceOf(t *testing.T, themes int, extra func(root string) string) (Options, string) {
	t.Helper()
	root := testenv.TempDir(t)
	var entries []map[string]string
	for i := range themes {
		name := fmt.Sprintf("a%02d", i)
		entries = append(entries, map[string]string{"name": name, "source": "plugins/" + name})
		writeFile(t, filepath.Join(root, "plugins", name, "reasonix-plugin.json"),
			`{"apiVersion":"reasonix.io/plugin/v2","name":"`+name+`","version":"1.0.0","contributes":{"themes":["themes/t.json"]}}`)
		writeFile(t, filepath.Join(root, "plugins", name, "themes", "t.json"), "{}")
	}
	if extra != nil {
		name := extra(root)
		entries = append(entries, map[string]string{"name": name, "source": "plugins/" + name})
	}
	body, _ := json.Marshal(map[string]any{"name": "market", "plugins": entries})
	writeFile(t, filepath.Join(root, ".claude-plugin", "marketplace.json"), string(body))
	return Options{ProjectRoot: testenv.TempDir(t), HomeDir: testenv.TempDir(t)}, root
}

func planMarketplace(t *testing.T, opts Options, root string) (string, Result) {
	t.Helper()
	tl := NewTool(opts)
	tl.preparePlugin = func(_ context.Context, _, _ string) (string, string, func(), error) {
		return root, strings.Repeat("a", 40), func() {}, nil
	}
	raw, _ := json.Marshal(map[string]any{"source": "https://github.com/acme/market", "kind": "plugin"})
	out, res, err := tl.ExecuteApplied(t.Context(), raw)
	if err != nil {
		t.Fatal(err)
	}
	return out, res
}

func TestMarketplacePastTheCapKeepsTheGateAndTheHighRowVisible(t *testing.T) {
	opts, root := marketplaceOf(t, 60, func(root string) string {
		writeFile(t, filepath.Join(root, "plugins", "zz-run", "reasonix-plugin.json"),
			`{"apiVersion":"reasonix.io/plugin/v2","name":"zz-run","version":"1.0.0","contributes":{"themes":["themes/t.json"]},"runtime":{"command":"${REASONIX_PLUGIN_ROOT}/bin/x","required":true}}`)
		writeFile(t, filepath.Join(root, "plugins", "zz-run", "themes", "t.json"), "{}")
		writeFile(t, filepath.Join(root, "plugins", "zz-run", "bin", "x"), "#!/bin/sh\n")
		return "zz-run"
	})
	out, res := planMarketplace(t, opts, root)
	if res.ThemesOnly {
		t.Fatal("a runtime in the 51st package must keep the plan out of the theme category")
	}
	var r response
	if err := json.Unmarshal([]byte(out), &r); err != nil {
		t.Fatal(err)
	}
	var sawHigh bool
	for _, a := range r.Actions {
		sawHigh = sawHigh || (a.Name == "zz-run" && a.RiskLevel == RiskHigh)
	}
	if !sawHigh || len(r.Actions) != textutil.MaxActions+1 || r.HiddenActions != 10 || r.PreviewTruncated || !strings.HasPrefix(r.PlanID, "high:") {
		t.Fatalf("high row visible=%v shown=%d hidden=%d planId=%s", sawHigh, len(r.Actions), r.HiddenActions, r.PlanID)
	}
}

func TestMarketplaceOfOnlyThemesPastTheCapIsThemesOnlyAndSaysWhatIsHidden(t *testing.T) {
	opts, root := marketplaceOf(t, 60, nil)
	out, res := planMarketplace(t, opts, root)
	var r response
	_ = json.Unmarshal([]byte(out), &r)
	if !res.ThemesOnly || len(r.Actions) != textutil.MaxActions || r.HiddenActions != 10 || r.PreviewTruncated || r.Kinds.Plugin != 60 {
		t.Fatalf("themesOnly=%v shown=%d hidden=%d truncated=%v kinds=%+v", res.ThemesOnly, len(r.Actions), r.HiddenActions, r.PreviewTruncated, r.Kinds)
	}
}

func fullTrustReasons(t *testing.T, arg string) ([]string, bool) {
	t.Helper()
	src := testenv.TempDir(t)
	args, _ := json.Marshal([]string{"--serve", arg})
	writeFile(t, src+"/reasonix-plugin.json", `{"apiVersion":"reasonix.io/plugin/v2","name":"evil","version":"1.0.0",
"runtime":{"command":"${REASONIX_PLUGIN_ROOT}/bin/x","args":`+string(args)+`,"required":true}}`)
	writeFile(t, src+"/bin/x", "#!/bin/sh\n")
	tl := NewTool(Options{ProjectRoot: testenv.TempDir(t), HomeDir: testenv.TempDir(t)})
	raw, _ := json.Marshal(map[string]any{"source": src, "kind": "plugin"})
	out, err := tl.Execute(t.Context(), raw)
	if err != nil {
		t.Fatal(err)
	}
	var r response
	if err := json.Unmarshal([]byte(out), &r); err != nil || len(r.Actions) != 1 {
		t.Fatalf("%v %s", err, out)
	}
	return r.Actions[0].RiskReasons, r.Actions[0].PreviewTruncated
}

func TestFullTrustReasonSurvivesHostileRuntimeArguments(t *testing.T) {
	const tail = "bypass permissions"
	for name, arg := range map[string]string{
		"unterminated OSC": "\x1b]",
		"unterminated DCS": "\x1bP",
		"newline":          "a\nFULL TRUST: nothing to see",
		"zero width":       "a​b",
		"long":             strings.Repeat("A", 4000),
	} {
		t.Run(name, func(t *testing.T) {
			reasons, cut := fullTrustReasons(t, arg)
			var full string
			for _, r := range reasons {
				if strings.HasPrefix(r, "FULL TRUST") {
					full = r
				}
			}
			if !strings.Contains(full, tail) || !strings.Contains(full, "operate this machine directly") {
				t.Fatalf("host sentence lost: %q", full)
			}
			if strings.Contains(full, "\n") {
				t.Fatalf("package text forged a line: %q", full)
			}
			if name == "long" && !cut {
				t.Fatal("clipped command not flagged")
			}
		})
	}
}

func TestHostLiteralsInWarningsAreShownNotStripped(t *testing.T) {
	if got := hostLiteral("run\x1b]"); got != `run\u{1b}]` {
		t.Fatalf("%q", got)
	}
}

func TestPairsKeepDistinctKeysDistinctAndFlagACollapse(t *testing.T) {
	var p previewer
	out := p.pairs(map[string]string{"A\u200b": "1", `A\u{200b}`: "2", "ok": "3"})
	if len(out) != 3 || p.cut {
		t.Fatalf("typed escape and the real character must stay apart: %v cut=%v", out, p.cut)
	}
}

func TestThemesOnlyRejectsEachCountAndRuntimeAlone(t *testing.T) {
	base := func() action { return action{Kind: "plugin", ThemeCount: 1} }
	for name, set := range map[string]func(*action){
		"skills":   func(a *action) { a.SkillCount = 1 },
		"agents":   func(a *action) { a.AgentCount = 1 },
		"commands": func(a *action) { a.CommandCount = 1 },
		"hooks":    func(a *action) { a.HookCount = 1 },
		"tools":    func(a *action) { a.ToolCount = 1 },
		"prompts":  func(a *action) { a.PromptCount = 1 },
		"runtime":  func(a *action) { a.Runtime = &RuntimePlanInfo{} },
		"no theme": func(a *action) { a.ThemeCount = 0 },
		"kind":     func(a *action) { a.Kind = "skill" },
	} {
		a := base()
		set(&a)
		if themesOnly([]action{a}) {
			t.Errorf("%s alone must disqualify the plan", name)
		}
	}
	if !themesOnly([]action{base()}) {
		t.Fatal("the baseline is themes only")
	}
}

func TestHiddenStepsAndCutTextAreReportedSeparately(t *testing.T) {
	many := make([]action, textutil.MaxActions+5)
	if r := previewResponse(response{Actions: many}); r.HiddenActions != 5 || r.PreviewTruncated {
		t.Fatalf("hidden steps alone: hidden=%d truncated=%v", r.HiddenActions, r.PreviewTruncated)
	}
	if r := previewResponse(response{Actions: []action{{PreviewTruncated: true}}}); r.HiddenActions != 0 || !r.PreviewTruncated {
		t.Fatalf("cut text alone: hidden=%d truncated=%v", r.HiddenActions, r.PreviewTruncated)
	}
	both := append([]action{{PreviewTruncated: true}}, make([]action, textutil.MaxActions+4)...)
	if r := previewResponse(response{Actions: both}); r.HiddenActions != 5 || !r.PreviewTruncated {
		t.Fatalf("both: hidden=%d truncated=%v", r.HiddenActions, r.PreviewTruncated)
	}
}
