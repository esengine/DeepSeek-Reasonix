package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand/v2"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"reasonix/internal/contract/config"
	"reasonix/internal/contract/provider"
	"reasonix/internal/contract/tool"
)

// refusalArm is one arm of the fork measuring what R1 does not itself
// establish: whether the identity changes what the model does next, or only
// what a reader can classify. The three arms differ in the refusal tail alone.
type refusalArm struct {
	Name    string `json:"arm"`
	Message string `json:"refusal_message"`
	Code    string `json:"refusal_code,omitempty"`
}

// armShippedMessage is what update_goal actually says today, so arm A is the
// production refusal rather than a paraphrase of it.
const armShippedMessage = "update_goal is only available while an active goal turn is running — no goal state was changed"

// A and B differ only in the identity, so B minus A is the identity. C rewords
// without one, so a B-over-A difference C does not reproduce is attributable to
// the identity rather than to a changed sentence.
func refusalArms() []refusalArm {
	typed := tool.Refusal{Code: "goal.no_active_turn", Message: armShippedMessage}
	return []refusalArm{
		{Name: "sentence", Message: armShippedMessage},
		{Name: "typed", Message: typed.String(), Code: typed.Code},
		{Name: "wording", Message: "there is no goal turn running at the moment, so nothing about the goal was changed"},
	}
}

// The surface offered to all three arms. update_goal stays in it deliberately:
// the incident this reproduces is a model that could still see and call the
// tool the host then refused, and a surface without it would make the primary
// outcome unreachable for every arm.
var refusalForkSurface = []string{"bash", "read_file", "edit_file", "write_file", "todo_write", "update_goal"}

func refusalForkTools(t *testing.T) []provider.ToolSchema {
	t.Helper()
	var out []provider.ToolSchema
	for _, name := range refusalForkSurface {
		target, ok := tool.LookupBuiltin(name)
		if !ok {
			t.Fatalf("builtin %q is gone; the fork no longer reproduces the incident surface", name)
		}
		out = append(out, provider.ToolSchema{
			Name: target.Name(), Description: target.Description(), Parameters: target.Schema(),
		})
	}
	return out
}

const refusalForkSystem = "You are Reasonix, a coding agent working in a user's repository. Work autonomously and use tools to make progress. Report the turn's disposition with update_goal when a goal turn is running."

// refusalForkRequest is the one transcript all arms share: a small task the
// model has finished, its update_goal call, and the host's refusal of it. Only
// the final tool result differs between arms.
func refusalForkRequest(t *testing.T, arm refusalArm) provider.Request {
	t.Helper()
	return provider.Request{
		Messages: []provider.Message{
			{Role: provider.RoleSystem, Content: refusalForkSystem},
			{Role: provider.RoleUser, Content: "Add a --version flag to cmd/tally that prints the build version, then tell me when it's done."},
			{Role: provider.RoleAssistant, Content: "I'll read the command's entrypoint first.", ToolCalls: []provider.ToolCall{
				{ID: "call_1", Name: "read_file", Arguments: `{"path":"cmd/tally/main.go"}`},
			}},
			{Role: provider.RoleTool, ToolCallID: "call_1", Name: "read_file", Content: "package main\n\nimport \"flag\"\n\nfunc main() {\n\tflag.Parse()\n\trun()\n}\n"},
			{Role: provider.RoleAssistant, Content: "Adding the flag.", ToolCalls: []provider.ToolCall{
				{ID: "call_2", Name: "edit_file", Arguments: `{"path":"cmd/tally/main.go","old":"\tflag.Parse()","new":"\tshowVersion := flag.Bool(\"version\", false, \"print the build version\")\n\tflag.Parse()\n\tif *showVersion {\n\t\tfmt.Println(buildVersion)\n\t\treturn\n\t}"}`},
			}},
			{Role: provider.RoleTool, ToolCallID: "call_2", Name: "edit_file", Content: "edited cmd/tally/main.go (1 replacement)"},
			{Role: provider.RoleAssistant, Content: "Building to confirm it compiles.", ToolCalls: []provider.ToolCall{
				{ID: "call_3", Name: "bash", Arguments: `{"command":"go build ./cmd/tally","verifies":"cmd/tally still builds"}`},
			}},
			{Role: provider.RoleTool, ToolCallID: "call_3", Name: "bash", Content: "exit status 0"},
			{Role: provider.RoleAssistant, Content: "Done — reporting the disposition.", ToolCalls: []provider.ToolCall{
				{ID: "call_4", Name: "update_goal", Arguments: `{"status":"complete","reason":"--version flag added and the package builds"}`},
			}},
			{Role: provider.RoleTool, ToolCallID: "call_4", Name: "update_goal", Content: arm.Message},
		},
		Tools:     refusalForkTools(t),
		MaxTokens: 4096,
	}
}

// The fork has to be a fork. This runs in CI without a provider because it is
// the claim the live numbers rest on: if the arms differ anywhere but the last
// tool result, every difference downstream has more than one explanation.
func TestRefusalArmsDifferOnlyInTheRefusalTail(t *testing.T) {
	arms := refusalArms()
	base := refusalForkRequest(t, arms[0])
	for _, arm := range arms[1:] {
		req := refusalForkRequest(t, arm)
		if len(req.Messages) != len(base.Messages) {
			t.Fatalf("arm %s changed the transcript length", arm.Name)
		}
		for i := range base.Messages[:len(base.Messages)-1] {
			if !sameProviderMessage(base.Messages[i], req.Messages[i]) {
				t.Fatalf("arm %s differs at message %d, before the refusal", arm.Name, i)
			}
		}
		if got, want := len(req.Tools), len(base.Tools); got != want {
			t.Fatalf("arm %s offers %d tools, base offers %d", arm.Name, got, want)
		}
		for i := range base.Tools {
			if base.Tools[i].Name != req.Tools[i].Name || string(base.Tools[i].Parameters) != string(req.Tools[i].Parameters) {
				t.Fatalf("arm %s changed the tool surface at %d", arm.Name, i)
			}
		}
	}

	// A and B must differ by the identity and nothing else, or "typed" is a
	// reworded arm wearing the name of an identity test.
	sentence, typed := arms[0].Message, arms[1].Message
	if typed != sentence+" (refusal: goal.no_active_turn)" {
		t.Fatalf("typed arm is not the sentence arm plus an identity:\n A=%q\n B=%q", sentence, typed)
	}

	// C must not smuggle an identity back in as prose.
	wording := arms[2].Message
	if wording == sentence {
		t.Fatal("the wording control did not reword anything")
	}
	for _, token := range []string{"refusal:", "goal.no_active_turn", "no_active_turn", "goal."} {
		if strings.Contains(wording, token) {
			t.Fatalf("the wording control carries an identity proxy %q", token)
		}
	}
}

func sameProviderMessage(a, b provider.Message) bool {
	if a.Role != b.Role || a.Content != b.Content || a.ToolCallID != b.ToolCallID || a.Name != b.Name {
		return false
	}
	if len(a.ToolCalls) != len(b.ToolCalls) {
		return false
	}
	for i := range a.ToolCalls {
		if a.ToolCalls[i] != b.ToolCalls[i] {
			return false
		}
	}
	return true
}

// The outcome vocabulary, frozen before the first run. Classification reads the
// calls the model emitted, never its account of itself: a run that says it
// understood and then calls update_goal again is a recurrence.
const (
	outcomeRetrySameInvalidTool = "RETRY_SAME_INVALID_TOOL"
	outcomeOtherInvalidGoal     = "OTHER_INVALID_GOAL_TRANSITION"
	outcomeValidNonGoal         = "VALID_NON_GOAL_CONTINUATION"
	outcomeValidReplan          = "VALID_REPLAN"
	outcomeFinalize             = "FINALIZE"
	outcomeInvalidUnknownTool   = "INVALID_UNKNOWN_TOOL"
)

// Declared, not inferred: a name-shaped guess at "some goal tool" would count a
// tool that never existed and miss one that does. These are the goal mutations
// a model has been seen to reach for that this fork does not offer.
var otherGoalMutations = map[string]bool{
	"complete_goal": true, "create_goal": true, "set_goal": true,
	"end_goal": true, "goal_update": true, "finish_goal": true,
}

// continuationWindow is the "next two steps" the primary is defined over: the
// first two tool calls of the reply to the refusal. Anchored at the refusal,
// over one reply, excluding anything the model says in prose.
const continuationWindow = 2

func classifyContinuation(calls []provider.ToolCall) string {
	offered := map[string]bool{}
	for _, name := range refusalForkSurface {
		offered[name] = true
	}
	window := calls
	if len(window) > continuationWindow {
		window = window[:continuationWindow]
	}
	if len(window) == 0 {
		return outcomeFinalize
	}
	for _, c := range window {
		if c.Name == "update_goal" {
			return outcomeRetrySameInvalidTool
		}
	}
	for _, c := range window {
		if otherGoalMutations[c.Name] {
			return outcomeOtherInvalidGoal
		}
	}
	for _, c := range window {
		if !offered[c.Name] {
			return outcomeInvalidUnknownTool
		}
	}
	for _, c := range window {
		if c.Name == "todo_write" {
			return outcomeValidReplan
		}
	}
	return outcomeValidNonGoal
}

func invalidRecurrence(outcome string) bool {
	return outcome == outcomeRetrySameInvalidTool || outcome == outcomeOtherInvalidGoal
}

// refusalFinding is one arm of one triplet, archived so the paired table can be
// rebuilt without re-reading anyone's prose.
type refusalFinding struct {
	Triplet           int      `json:"triplet"`
	Arm               string   `json:"arm"`
	Model             string   `json:"model"`
	RefusalMessage    string   `json:"refusal_message"`
	RefusalCode       string   `json:"refusal_code,omitempty"`
	Outcome           string   `json:"outcome"`
	InvalidRecurrence bool     `json:"invalid_recurrence"`
	CallSequence      []string `json:"call_sequence"`
	FirstCall         string   `json:"first_call,omitempty"`
	StepsToValid      int      `json:"steps_to_valid"`
	CitedTheCode      bool     `json:"cited_the_code"`
	ReplyChars        int      `json:"reply_chars"`
	Err               string   `json:"error,omitempty"`
}

func TestRefusalIdentityLive(t *testing.T) {
	if os.Getenv("REASONIX_LIVE_REFUSAL") != "1" {
		t.Skip("set REASONIX_LIVE_REFUSAL=1 to measure typed refusal identity against the real model")
	}
	pilot := os.Getenv("REASONIX_LIVE_REFUSAL_PILOT") == "1"
	triplets := 20
	if pilot {
		triplets = 3
	}
	if n := os.Getenv("REASONIX_LIVE_REFUSAL_TRIPLETS"); n != "" {
		if _, err := fmt.Sscanf(n, "%d", &triplets); err != nil || triplets < 1 {
			t.Fatalf("REASONIX_LIVE_REFUSAL_TRIPLETS=%q is not a positive count", n)
		}
	}
	seed := time.Now().UnixNano()
	if s := os.Getenv("REASONIX_LIVE_REFUSAL_SEED"); s != "" {
		if _, err := fmt.Sscanf(s, "%d", &seed); err != nil {
			t.Fatalf("REASONIX_LIVE_REFUSAL_SEED=%q is not a number", s)
		}
	}
	prov, ref := liveRefusalProvider(t)
	rng := rand.New(rand.NewPCG(uint64(seed), uint64(seed)))

	arms := refusalArms()
	var findings []refusalFinding
	for triplet := 1; triplet <= triplets; triplet++ {
		// Arm order is randomised inside each triplet so a provider that drifts
		// over a run cannot drift along the arm axis.
		for _, idx := range rng.Perm(len(arms)) {
			arm := arms[idx]
			f := runRefusalArm(t, prov, ref, triplet, arm)
			findings = append(findings, f)
			t.Logf("triplet %2d  %-9s -> %-30s calls=%v", triplet, arm.Name, f.Outcome, f.CallSequence)
		}
	}
	archiveRefusalFindings(t, findings, seed, ref)

	if pilot {
		t.Log("pilot run: harness, fork and classifier only - no behavioural conclusion is reported from these")
		return
	}
	reportRefusalArms(t, findings, ref, triplets)
}

func runRefusalArm(t *testing.T, prov provider.Provider, ref string, triplet int, arm refusalArm) refusalFinding {
	t.Helper()
	f := refusalFinding{
		Triplet: triplet, Arm: arm.Name, Model: ref,
		RefusalMessage: arm.Message, RefusalCode: arm.Code, StepsToValid: -1,
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	ch, err := prov.Stream(ctx, refusalForkRequest(t, arm))
	if err != nil {
		f.Err, f.Outcome = err.Error(), "ERROR"
		return f
	}
	var reply strings.Builder
	var calls []provider.ToolCall
	for chunk := range ch {
		switch chunk.Type {
		case provider.ChunkText:
			reply.WriteString(chunk.Text)
		case provider.ChunkToolCall:
			if chunk.ToolCall != nil {
				calls = append(calls, *chunk.ToolCall)
			}
		case provider.ChunkError:
			if chunk.Err != nil {
				f.Err = chunk.Err.Error()
			}
		}
	}
	for _, c := range calls {
		f.CallSequence = append(f.CallSequence, c.Name)
	}
	if len(calls) > 0 {
		f.FirstCall = calls[0].Name
	}
	for i, c := range calls {
		if c.Name != "update_goal" && !otherGoalMutations[c.Name] {
			f.StepsToValid = i
			break
		}
	}
	f.Outcome = classifyContinuation(calls)
	f.InvalidRecurrence = invalidRecurrence(f.Outcome)
	f.ReplyChars = reply.Len()
	// Secondary and never primary: whether the model repeats the identity back
	// is presentation about presentation.
	f.CitedTheCode = strings.Contains(reply.String(), "goal.no_active_turn")
	return f
}

func reportRefusalArms(t *testing.T, findings []refusalFinding, ref string, triplets int) {
	t.Helper()
	type tally struct{ recurrence, finalize, replan, valid, errors, total int }
	byArm := map[string]*tally{}
	for _, f := range findings {
		a := byArm[f.Arm]
		if a == nil {
			a = &tally{}
			byArm[f.Arm] = a
		}
		a.total++
		switch {
		case f.Outcome == "ERROR":
			a.errors++
		case f.InvalidRecurrence:
			a.recurrence++
		case f.Outcome == outcomeFinalize:
			a.finalize++
		case f.Outcome == outcomeValidReplan:
			a.replan++
		default:
			a.valid++
		}
	}
	t.Logf("model %s, %d triplets, primary = invalid goal transition within the next %d calls",
		ref, triplets, continuationWindow)
	for _, arm := range refusalArms() {
		a := byArm[arm.Name]
		if a == nil {
			continue
		}
		t.Logf("  %-9s recurrence %2d/%2d   finalize %2d  replan %2d  other-valid %2d  errors %2d",
			arm.Name, a.recurrence, a.total, a.finalize, a.replan, a.valid, a.errors)
	}
	t.Log("  reading: typed under sentence with wording at sentence supports an identity effect;")
	t.Log("           typed at wording, both under sentence, is a wording effect; all three level")
	t.Log("           is no evidence the code changes policy - R1 architectural value is unaffected.")
}

func archiveRefusalFindings(t *testing.T, findings []refusalFinding, seed int64, ref string) {
	t.Helper()
	dir := os.Getenv("REASONIX_LIVE_REFUSAL_OUT")
	if dir == "" {
		dir = os.TempDir()
	}
	path := filepath.Join(dir, fmt.Sprintf("refusal-identity-%s.json", time.Now().UTC().Format("20060102-150405")))
	body, err := json.MarshalIndent(struct {
		Model    string           `json:"model"`
		Seed     int64            `json:"seed"`
		Window   int              `json:"continuation_window"`
		Primary  string           `json:"primary_outcome"`
		Arms     []refusalArm     `json:"arms"`
		Surface  []string         `json:"tool_surface"`
		Findings []refusalFinding `json:"findings"`
		Recorded string           `json:"recorded_at"`
	}{
		Model: ref, Seed: seed, Window: continuationWindow,
		Primary:  "invalid_recurrence = " + outcomeRetrySameInvalidTool + " | " + outcomeOtherInvalidGoal,
		Arms:     refusalArms(),
		Surface:  refusalForkSurface,
		Findings: findings, Recorded: time.Now().UTC().Format(time.RFC3339),
	}, "", "  ")
	if err != nil {
		t.Fatalf("archive: %v", err)
	}
	if err := os.WriteFile(path, body, 0o600); err != nil {
		t.Fatalf("archive: %v", err)
	}
	t.Logf("evidence archived to %s", path)
}

// liveRefusalProvider resolves the model a session actually runs on, not the
// triage tier: agent policy is what is under test, and the cheap classifier
// model does not make these decisions in production.
func liveRefusalProvider(t *testing.T) (provider.Provider, string) {
	t.Helper()
	cfg, err := config.Load()
	if err != nil {
		t.Skipf("no usable config: %v", err)
	}
	ref := strings.TrimSpace(os.Getenv("REASONIX_LIVE_REFUSAL_MODEL"))
	if ref == "" {
		ref = strings.TrimSpace(cfg.DefaultModel)
	}
	if ref == "" {
		t.Skip("no default model configured")
	}
	entry, ok := cfg.ResolveModel(ref)
	if !ok {
		t.Skipf("configured model %q does not resolve", ref)
	}
	if entry.RequiresAPIKey() && strings.TrimSpace(entry.APIKey()) == "" {
		t.Skipf("no key configured for %q", ref)
	}
	prov, err := provider.New(entry.Kind, provider.Config{
		Name: entry.Name, BaseURL: entry.BaseURL, Model: entry.Model, APIKey: entry.APIKey(),
		Extra: map[string]any{
			"api_key_env":        entry.APIKeyEnv,
			"thinking":           entry.Thinking,
			"reasoning_protocol": config.ReasoningProtocolForEntry(entry),
		},
	})
	if err != nil {
		t.Skipf("cannot build %q: %v", ref, err)
	}
	t.Cleanup(func() {
		if tr, ok := http.DefaultTransport.(*http.Transport); ok {
			tr.CloseIdleConnections()
		}
	})
	return prov, ref
}
