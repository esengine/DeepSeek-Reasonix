package serve

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"testing"
	"time"

	"reasonix/internal/contract/config"
	"reasonix/internal/contract/planmode"
	"reasonix/internal/contract/pricing"
	"reasonix/internal/contract/provider"
	"reasonix/internal/session/control"
	"reasonix/internal/tools/jobs"
)

// The exact key set GET /status sends. The type used to be a map literal, so
// nothing could read its shape: a key could appear or disappear without a test
// or a lint failing. This list is the declaration the golden test holds the
// struct to — adding a key means adding it here, on purpose.
var statusPayloadKeys = []string{
	"agentComposition",
	"agentCompositionLocked",
	"approvalDefault",
	"autoApproveTools",
	"busy",
	"bypass",
	"cacheHit",
	"cacheMiss",
	"cwd",
	"decisions",
	"effort",
	"goal",
	"goalRuntime",
	"goalStatus",
	"jobs",
	"label",
	"lastUsage",
	"modelRef",
	"modes",
	"plan",
	"planPhase",
	"preset",
	"providerDisplayName",
	"running",
	"sessionCostQuote",
	"sessionPath",
	"sessionUsage",
	"tariff",
	"toolApprovalMode",
	"used",
	"vision",
	"visionDeclared",
	"window",
	"workspaceRoot",
}

// statusPayloadCases is a representative set of the states /status describes:
// an idle session, the delivery window its `busy` key exists for, a session
// whose model face states zeros, and one where every optional key is present.
func statusPayloadCases() map[string]statusFacts {
	usage := &provider.Usage{
		PromptTokens: 1200, CompletionTokens: 80, TotalTokens: 1280,
		CacheHitTokens: 900, CacheMissTokens: 300, FinishReason: "stop", RequestCount: 1,
	}
	tariff := pricing.Tariff{
		Peak: true, Changes: true,
		ChangesAt: time.Date(2026, 10, 26, 18, 0, 0, 0, time.UTC),
	}
	decisions := []control.Decision{{ID: "ask-1", Kind: control.DecisionAsk}}
	noDecisions := []control.Decision{}
	var noDecisionsAtAll []control.Decision
	quote := pricing.CostQuote{
		Original:     pricing.Money{Amount: "0.012300000", Currency: "USD"},
		Selected:     &pricing.Money{Amount: "0.012300000", Currency: "USD"},
		BillingMode:  pricing.BillingModePAYG,
		CostComplete: true, DisplayComplete: true, Complete: true,
		DisplayStatus: pricing.DisplayStatusMatched,
		AggregateMode: pricing.AggregateModeSingleCurrency,
	}
	return map[string]statusFacts{
		"idle session, nothing projected": {},
		"delivery window: the turn ended, its result is landing": {
			Label: "审查", Running: false, Busy: true,
			Plan: false, AutoApproveTools: false, ToolApprovalMode: "ask",
			Preset: "balanced", Goal: "", GoalStatus: "",
			Cwd: "/work", WorkspaceRoot: "/work", SessionPath: "/work/.reasonix/sessions/s.jsonl",
			Used: 1200, Window: 128000, CacheHit: 900, CacheMiss: 300,
			Decisions:        &noDecisions,
			LastUsage:        usage,
			SessionCostQuote: quote,
			SessionUsage: SessionUsage{
				Requests: 1, PromptTokens: 1200, CompletionTokens: 80,
				CacheHit: 900, CacheMiss: 300, WorkMs: 4200,
				Sources: []SourceUsage{{Source: "main", Requests: 1, PromptTokens: 1200, CompletionTokens: 80, CacheHit: 900, CacheMiss: 300, Cost: quote}},
			},
		},
		"a model face stating its zeros, and no label for the provider": {
			Label: "deepseek/deepseek-v4-pro", Running: true, Busy: true,
			Face: &control.ModelFace{Ref: "deepseek/deepseek-v4-pro", Effort: "", Vision: false, VisionDeclared: false},
		},
		"every optional key present": {
			AgentCompositionLocked: true, Label: "reviewer", Running: true, Busy: true,
			Plan: true, AutoApproveTools: true, ToolApprovalMode: "auto",
			Posture: control.PostureReport{Defaulted: true, Trust: config.WorkspaceTrusted, Trustable: true},
			Preset:  "strict", Goal: "ship the fix", GoalStatus: "active",
			Cwd: "/work", WorkspaceRoot: "/work", SessionPath: "/work/.reasonix/sessions/s.jsonl",
			Used: 12000, Window: 128000, CacheHit: 9000, CacheMiss: 3000,
			Composition: &compositionSummary{
				ID: "c1", Name: "reviewer", Revision: "r2", Digest: "sha256:abc",
				Started: true, FormatVersion: 1, ResourceMode: "full",
			},
			GoalRuntime: &control.GoalRuntimeView{
				TurnsUsed: 2, TokensUsed: 100, RequestsUsed: 3, WorkDurationMs: 1500,
				NoProgressTurns: 1, LastReason: "still working", StopCause: "",
			},
			Decisions:           &decisions,
			PlanPhase:           planmode.Executing,
			LastUsage:           usage,
			Face:                &control.ModelFace{Ref: "deepseek/deepseek-v4-pro", Effort: "high", Vision: true, VisionDeclared: true},
			ProviderDisplayName: "DeepSeek",
			Modes:               []control.ModelModeView{{ID: "think", LabelKey: "mode.think", HintKey: "mode.think.hint", Costlier: true, Active: true}},
			SessionCostQuote:    quote,
			SessionUsage: SessionUsage{
				Requests: 1, PromptTokens: 1200, CompletionTokens: 80,
				CacheHit: 900, CacheMiss: 300, WorkMs: 4200,
				Sources: []SourceUsage{{Source: "main", Requests: 1, PromptTokens: 1200, CompletionTokens: 80, CacheHit: 900, CacheMiss: 300, Cost: quote}},
			},
			Jobs:   []jobs.View{{ID: "j1", Kind: "bash", Label: "npm test", Description: "unit tests", Detail: "npm test -- --run", Status: "running", StartedAt: 1}},
			Tariff: &tariff,
		},
		"a controller that projects no decisions at all": {
			Label: "reviewer", Running: false, Busy: false,
			Decisions: &noDecisionsAtAll,
		},
	}
}

// legacyStatusMap is the map literal /status was built from, kept only here so
// the golden test can prove the named type sends the same bytes. It is dead
// weight on purpose: when the struct gains or loses a key, this file has to be
// edited too, and that edit is the review.
func (f statusFacts) legacyStatusMap() map[string]any {
	sess := map[string]any{
		"agentCompositionLocked": f.AgentCompositionLocked,
		"label":                  f.Label,
		"running":                f.Running,
		"busy":                   f.Busy,
		"plan":                   f.Plan,
		"autoApproveTools":       f.AutoApproveTools,
		"bypass":                 f.AutoApproveTools,
		"toolApprovalMode":       f.ToolApprovalMode,
		"approvalDefault":        f.Posture,
		"preset":                 f.Preset,
		"goal":                   f.Goal,
		"goalStatus":             f.GoalStatus,
		"cwd":                    f.Cwd,
		"workspaceRoot":          f.WorkspaceRoot,
		"sessionPath":            f.SessionPath,
		"used":                   f.Used,
		"window":                 f.Window,
		"cacheHit":               f.CacheHit,
		"cacheMiss":              f.CacheMiss,
	}
	if f.Composition != nil {
		sess["agentComposition"] = f.Composition
	}
	if f.GoalRuntime != nil {
		sess["goalRuntime"] = f.GoalRuntime
	}
	if f.Decisions != nil {
		sess["decisions"] = *f.Decisions
		if f.PlanPhase != planmode.Inactive {
			sess["planPhase"] = f.PlanPhase.String()
		}
	}
	if f.LastUsage != nil {
		sess["lastUsage"] = f.LastUsage
	}
	if f.Face != nil {
		sess["effort"] = f.Face.Effort
		sess["modelRef"] = f.Face.Ref
		if f.ProviderDisplayName != "" {
			sess["providerDisplayName"] = f.ProviderDisplayName
		}
		sess["vision"] = f.Face.Vision
		sess["visionDeclared"] = f.Face.VisionDeclared
	}
	if len(f.Modes) > 0 {
		sess["modes"] = f.Modes
	}
	sess["sessionCostQuote"] = f.SessionCostQuote
	sess["sessionUsage"] = f.SessionUsage
	if len(f.Jobs) > 0 {
		sess["jobs"] = f.Jobs
	}
	if f.Tariff != nil {
		// Spelled out rather than through tariffBody: the helper is the code
		// under test, so calling it here would let a renamed nested key change
		// both sides at once.
		tariff := map[string]any{"peak": f.Tariff.Peak}
		if f.Tariff.Changes {
			tariff["changesAt"] = f.Tariff.ChangesAt.UTC().Format(time.RFC3339)
		}
		sess["tariff"] = tariff
	}
	return sess
}

// TestStatusPayloadSendsTheBytesTheMapSent is the proof that naming the payload
// did not rename, drop, add or reorder a key: both constructions are marshalled
// over the same inputs and compared byte for byte.
func TestStatusPayloadSendsTheBytesTheMapSent(t *testing.T) {
	for name, facts := range statusPayloadCases() {
		t.Run(name, func(t *testing.T) {
			want, err := json.Marshal(facts.legacyStatusMap())
			if err != nil {
				t.Fatalf("marshal the map this replaced: %v", err)
			}
			got, err := json.Marshal(facts.view())
			if err != nil {
				t.Fatalf("marshal sessionStatus: %v", err)
			}
			reportStatusPayloadDiff(t, want, got)
		})
	}
}

// TestStatusPayloadKeySetIsTheDeclaredOne makes an added or removed key a
// deliberate edit rather than something the two sides of the golden test can
// drift into together.
func TestStatusPayloadKeySetIsTheDeclaredOne(t *testing.T) {
	facts := statusPayloadCases()["every optional key present"]
	body, err := json.Marshal(facts.view())
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]json.RawMessage
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatal(err)
	}
	if keys := sortedKeys(got); strings.Join(keys, ",") != strings.Join(statusPayloadKeys, ",") {
		t.Fatalf("GET /status keys = %v, want %v", keys, statusPayloadKeys)
	}
}

// TestTheStatusRouteSendsOnlyDeclaredKeys pins the HTTP boundary itself: the
// type could be right while the handler encoded something else, which is the
// way the map kept its shape unreadable.
func TestTheStatusRouteSendsOnlyDeclaredKeys(t *testing.T) {
	srv := newRichProviderServer(t)
	var body map[string]json.RawMessage
	getJSON(t, srv.URL+"/status", &body)
	declared := make(map[string]bool, len(statusPayloadKeys))
	for _, name := range statusPayloadKeys {
		declared[name] = true
	}
	for _, name := range sortedKeys(body) {
		if !declared[name] {
			t.Errorf("GET /status sends %q, which the declared key set does not name", name)
		}
	}
	// The keys every read carries, whatever the session is doing.
	for _, name := range []string{"busy", "running", "label", "plan", "used", "window"} {
		if _, ok := body[name]; !ok {
			t.Errorf("GET /status stopped sending %q", name)
		}
	}
}

// TestStatusPayloadOmitsNothingItUsedToOmit holds the absent half of the
// contract: an idle session with nothing projected sends exactly the always-on
// keys, so a client that reads an optional key still gets "absent", not a zero.
func TestStatusPayloadOmitsNothingItUsedToOmit(t *testing.T) {
	body, err := json.Marshal(statusFacts{}.view())
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]json.RawMessage
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatal(err)
	}
	want := []string{
		"agentCompositionLocked", "approvalDefault", "autoApproveTools", "busy", "bypass",
		"cacheHit", "cacheMiss", "cwd", "goal", "goalStatus", "label", "plan", "preset",
		"running", "sessionCostQuote", "sessionPath", "sessionUsage", "toolApprovalMode",
		"used", "window", "workspaceRoot",
	}
	if keys := sortedKeys(got); strings.Join(keys, ",") != strings.Join(want, ",") {
		t.Fatalf("idle GET /status keys = %v, want %v", keys, want)
	}
}

// reportStatusPayloadDiff names what moved rather than printing two 2 KB lines:
// a renamed key, a dropped key, a changed value, or the same snapshot in a
// different order (which is a byte change even when every key is right).
func reportStatusPayloadDiff(t *testing.T, want, got []byte) {
	t.Helper()
	if bytes.Equal(want, got) {
		return
	}
	var w, g map[string]json.RawMessage
	if err := json.Unmarshal(want, &w); err != nil {
		t.Fatalf("decode the map payload: %v", err)
	}
	if err := json.Unmarshal(got, &g); err != nil {
		t.Fatalf("decode the struct payload: %v", err)
	}
	var lines []string
	for _, k := range sortedKeys(w) {
		if _, ok := g[k]; !ok {
			lines = append(lines, fmt.Sprintf("- %q is sent by the map and not by the struct", k))
		}
	}
	for _, k := range sortedKeys(g) {
		if _, ok := w[k]; !ok {
			lines = append(lines, fmt.Sprintf("+ %q is sent by the struct and not by the map", k))
		}
	}
	for _, k := range sortedKeys(w) {
		if gv, ok := g[k]; ok && !bytes.Equal(w[k], gv) {
			lines = append(lines, fmt.Sprintf("~ %q: map %s, struct %s", k, w[k], gv))
		}
	}
	if len(lines) == 0 {
		lines = append(lines,
			"the same keys and values marshal in a different order",
			fmt.Sprintf("map:    %s", want),
			fmt.Sprintf("struct: %s", got))
	}
	t.Fatalf("GET /status changed over the same inputs:\n%s", strings.Join(lines, "\n"))
}

func sortedKeys(m map[string]json.RawMessage) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
