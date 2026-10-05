package doctor

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reasonix/internal/state/sessionstore"
	"slices"
	"strings"
	"testing"

	"reasonix/internal/base/testenv"
)

func TestCollectQualityProducesPublicSafeSummary(t *testing.T) {
	dir := testenv.TempDir(t)
	path := filepath.Join(dir, "private-project-session.jsonl")
	const secret = "private-source-and-prompt-secret"
	messages := []string{
		`{"role":"system","content":"` + secret + `"}`,
		`{"role":"user","content":"fix /private/example/repo and keep ` + secret + `"}`,
		`{"role":"assistant","tool_calls":[{"id":"edit-1","name":"edit_file","arguments":"{\"path\":\"/private/example/repo/app.go\"}"}]}`,
		`{"role":"tool","tool_call_id":"edit-1","name":"edit_file","content":"updated ` + secret + `"}`,
		`{"role":"assistant","reasoning_content":"verify the change","tool_calls":[{"id":"test-1","name":"bash","arguments":"{\"command\":\"go test ./...\"}"}]}`,
		`{"role":"tool","tool_call_id":"test-1","name":"bash","content":"ok"}`,
		`{"role":"user","content":"<compaction-summary>\nprivate summary ` + secret + `"}`,
		`{"role":"assistant","content":"done"}`,
	}
	if err := os.WriteFile(path, []byte(strings.Join(messages, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := sessionstore.SaveBranchMeta(path, sessionstore.BranchMeta{
		ID:               "private-session-id",
		Model:            "corp.private.example/supersecret-model",
		TokenMode:        "economy",
		Mode:             "plan-yolo",
		ToolApprovalMode: "yolo",
		Goal:             "finish " + secret,
		Recovered:        true,
	}); err != nil {
		t.Fatal(err)
	}
	telemetry := `{
		"version":2,
		"usage":{
			"promptTokens":1000,
			"completionTokens":200,
			"reasoningTokens":80,
			"cacheHitTokens":750,
			"cacheMissTokens":250,
			"requestCount":5,
			"sources":{
				"executor":{"requestCount":2},
				"planner":{"requestCount":1},
				"subagent":{"requestCount":1},
				"` + secret + `":{"requestCount":1}
			}
		}
	}`
	if err := os.WriteFile(path+".telemetry.json", []byte(telemetry), 0o644); err != nil {
		t.Fatal(err)
	}

	report, err := CollectQuality(QualityOptions{Version: "v-test", SessionRef: path})
	if err != nil {
		t.Fatalf("CollectQuality: %v", err)
	}
	if report.Profile.ModelID != "custom" || report.Profile.RuntimeProfile != "economy" {
		t.Fatalf("profile = %+v", report.Profile)
	}
	if report.Profile.CollaborationMode != "plan" || report.Profile.ToolApprovalMode != "yolo" || !report.Profile.GoalActive || !report.Profile.Recovered {
		t.Fatalf("profile modes = %+v", report.Profile)
	}
	if report.Transcript.ToolCalls != 2 || report.Transcript.WriterCalls != 1 || report.Transcript.VerificationCalls != 1 || report.Transcript.CompactionSummaries != 1 {
		t.Fatalf("transcript = %+v", report.Transcript)
	}
	if report.Transcript.ToolCallTurnsWithoutReasoning != 1 || report.Transcript.ReasoningTurns != 1 {
		t.Fatalf("reasoning turns = %d, tool-call turns without reasoning = %d",
			report.Transcript.ReasoningTurns, report.Transcript.ToolCallTurnsWithoutReasoning)
	}
	// The session reasoned on one turn and not on the tool-call turn, so the
	// warning fires on the evidence alone — no vendor is consulted.
	if !slices.ContainsFunc(report.Warnings, func(w string) bool {
		return strings.Contains(w, "no persisted reasoning content")
	}) {
		t.Fatalf("warnings = %q", report.Warnings)
	}
	if report.Usage.CacheHitPercent == nil || *report.Usage.CacheHitPercent != 75 {
		t.Fatalf("usage = %+v", report.Usage)
	}
	if report.Signals.ExecutorRequests != 2 || report.Signals.PlannerRequests != 1 || report.Signals.SubagentRequests != 1 || report.Signals.OtherRequests != 1 {
		t.Fatalf("signals = %+v", report.Signals)
	}

	jsonData, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	text := RenderQualityText(report)
	for _, output := range []string{string(jsonData), text} {
		for _, forbidden := range []string{secret, dir, "/private/example", "private-session-id", "supersecret-model", "corp.private.example"} {
			if strings.Contains(output, forbidden) {
				t.Fatalf("quality summary leaked %q:\n%s", forbidden, output)
			}
		}
	}
	if !strings.Contains(text, "privacy: transcript text, paths") {
		t.Fatalf("text missing privacy statement:\n%s", text)
	}
}

func TestCollectQualityWithoutTelemetryStaysUseful(t *testing.T) {
	path := filepath.Join(testenv.TempDir(t), "session.jsonl")
	if err := os.WriteFile(path, []byte(`{"role":"user","content":"hello"}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	report, err := CollectQuality(QualityOptions{Version: "v-test", SessionRef: path})
	if err != nil {
		t.Fatalf("CollectQuality: %v", err)
	}
	if report.Usage.Available {
		t.Fatal("usage should be unavailable without desktop telemetry")
	}
	if len(report.Warnings) != 1 || !strings.Contains(report.Warnings[0], "telemetry") {
		t.Fatalf("warnings = %v", report.Warnings)
	}
}

func TestPublicTokenModeUsesRuntimeProfileNames(t *testing.T) {
	for input, want := range map[string]string{
		"":         "balanced",
		"full":     "balanced",
		"balanced": "balanced",
		"economy":  "economy",
		"delivery": "delivery",
	} {
		if got := publicTokenMode(input); got != want {
			t.Errorf("publicTokenMode(%q) = %q, want %q", input, got, want)
		}
	}
}

// The report may name a model only when the built-in catalog declares it.
// A vendor's name appearing inside a private deployment id is not a licence
// to report that vendor.
func TestPublicModelIDNamesOnlyDeclaredModels(t *testing.T) {
	cases := []struct {
		name string
		ref  string
		want string
	}{
		{"declared, bare", "deepseek-v4-flash", "deepseek-v4-flash"},
		{"declared, provider-qualified", "deepseek-responses/deepseek-v4-flash", "deepseek-v4-flash"},
		{"private deployment naming a vendor", "corp.internal/deepseek-tuned-secretproject", "custom"},
		{"private deployment", "corp.private.example/supersecret-model", "custom"},
		{"empty", "", "unknown"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := publicModelID(tc.ref); got != tc.want {
				t.Fatalf("publicModelID(%q) = %q, want %q", tc.ref, got, tc.want)
			}
		})
	}
}
