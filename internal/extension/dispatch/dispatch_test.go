package dispatch

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"testing"

	"reasonix/internal/extension"
	"reasonix/internal/extension/protocol"
	"reasonix/internal/extension/sidecar"
)

// The production Client interface exists so the real sidecar client drops in
// without an adapter; pin that here so a signature drift fails the build.
var _ Client = (*sidecar.Client)(nil)

// testSecret is a credential shape secrets.RedactCredentials reliably masks
// (it appears in internal/secrets' own tests).
const testSecret = "sk-real-secret-value-123456"

func interceptor(pluginID string, point extension.InterceptorPoint, priority int) extension.Contribution {
	return extension.Contribution{
		Kind:     extension.KindInterceptor,
		ID:       string(point),
		Source:   extension.ContributionSource{Scope: extension.ScopePlugin, PluginID: pluginID, Origin: "extension-runtime"},
		Priority: priority,
	}
}

func owner(pluginID string) extension.ContributionSource {
	return extension.ContributionSource{Scope: extension.ScopePlugin, PluginID: pluginID, Origin: "extension-runtime"}
}

// buildDispatcher wires a dispatcher over the given fakes; a plugin absent
// from fakes resolves to a nil (untyped) client, mirroring the documented
// adapter contract.
func buildDispatcher(chain map[extension.InterceptorPoint][]extension.Contribution, replacements map[extension.Slot]extension.ContributionSource, fakes map[string]*fakeClient, required map[string]bool, warns *warnRecorder) *Dispatcher {
	clients := func(pluginID string) Client {
		if client := fakes[pluginID]; client != nil {
			return client
		}
		return nil
	}
	return New(chain, replacements, clients, required, Options{Warn: warns.warn})
}

func timeoutError(pluginID string, point extension.InterceptorPoint) error {
	return &protocol.ProtocolError{
		Reason:  protocol.ErrInterceptTimeout,
		Message: fmt.Sprintf("extension %s did not answer %s within 5s", pluginID, point),
	}
}

// pointCase describes one intercept point for the dispatch matrix.
type pointCase struct {
	point         extension.InterceptorPoint
	sample        func() any
	replaceJSON   string
	checkReplaced func(t *testing.T, payload any)
	violateJSON   string
}

func userMessage(content string) protocol.ProviderMessage {
	return protocol.ProviderMessage{Role: protocol.ProviderRoleUser, Content: content}
}

func pointCases() []pointCase {
	cases := []pointCase{
		{
			point:       extension.PointInputReceive,
			sample:      func() any { return &InputPayload{Text: "hello"} },
			replaceJSON: `{"text":"rewritten"}`,
			checkReplaced: func(t *testing.T, payload any) {
				t.Helper()
				if got := payload.(*InputPayload).Text; got != "rewritten" {
					t.Fatalf("Text = %q, want %q", got, "rewritten")
				}
			},
			violateJSON: `{"text":""}`,
		},
		{
			point:       extension.PointAgentBeforeStart,
			sample:      func() any { return &AgentStartPayload{Model: "openai/gpt-5", ToolCount: 3, SessionID: "s1"} },
			replaceJSON: `{"model":"other/model","toolCount":7,"sessionId":"s1"}`,
			checkReplaced: func(t *testing.T, payload any) {
				t.Helper()
				got := payload.(*AgentStartPayload)
				if got.Model != "other/model" || got.ToolCount != 7 {
					t.Fatalf("payload = %+v, want model other/model with 7 tools", got)
				}
			},
			violateJSON: `{"model":"m"}`,
		},
		{
			point:       extension.PointSystemPromptBuild,
			sample:      func() any { return &SystemPromptPayload{Prompt: "base prompt", WorkspaceRoot: "/ws"} },
			replaceJSON: `{"prompt":"owned prompt","workspaceRoot":"/ws"}`,
			checkReplaced: func(t *testing.T, payload any) {
				t.Helper()
				if got := payload.(*SystemPromptPayload).Prompt; got != "owned prompt" {
					t.Fatalf("Prompt = %q, want %q", got, "owned prompt")
				}
			},
			violateJSON: `{"prompt":"x"}`,
		},
		{
			point:       extension.PointContextPrepare,
			sample:      func() any { return &ContextPayload{Messages: []protocol.ProviderMessage{userMessage("hi")}} },
			replaceJSON: `{"messages":[{"role":"user","content":"replaced"}]}`,
			checkReplaced: func(t *testing.T, payload any) {
				t.Helper()
				got := payload.(*ContextPayload)
				if len(got.Messages) != 1 || got.Messages[0].Content != "replaced" {
					t.Fatalf("Messages = %+v, want one replaced message", got.Messages)
				}
			},
			violateJSON: `{}`,
		},
		{
			point: extension.PointProviderRequest,
			sample: func() any {
				return &ProviderRequestPayload{Request: protocol.ProviderRequest{
					Messages: []protocol.ProviderMessage{userMessage("q")},
					Tools:    []protocol.ProviderToolSchema{},
				}}
			},
			replaceJSON: `{"request":{"messages":[{"role":"user","content":"q2"}],"tools":[],"maxTokens":99}}`,
			checkReplaced: func(t *testing.T, payload any) {
				t.Helper()
				got := payload.(*ProviderRequestPayload)
				if got.Request.MaxTokens != 99 || got.Request.Messages[0].Content != "q2" {
					t.Fatalf("Request = %+v, want maxTokens 99 and replaced message", got.Request)
				}
			},
			// tool parameters must be a JSON object, not an array.
			violateJSON: `{"request":{"messages":[],"tools":[{"name":"t","parameters":[1]}]}}`,
		},
		{
			point: extension.PointProviderResponse,
			sample: func() any {
				return &ProviderResponsePayload{Text: "answer", Usage: &protocol.ProviderUsage{PromptTokens: 1, TotalTokens: 2}}
			},
			replaceJSON: `{"text":"changed","calls":[{"id":"c1","name":"bash","arguments":"{}"}]}`,
			checkReplaced: func(t *testing.T, payload any) {
				t.Helper()
				got := payload.(*ProviderResponsePayload)
				if got.Text != "changed" || len(got.Calls) != 1 {
					t.Fatalf("payload = %+v, want changed text with one call", got)
				}
				// Whole-value assignment: fields absent from the replacement
				// must not leak the previous value through.
				if got.Usage != nil {
					t.Fatalf("Usage = %+v, want nil (replacement omitted it)", got.Usage)
				}
			},
			violateJSON: `{"calls":[{"id":"","name":"x"}]}`,
		},
		{
			point:       extension.PointToolBefore,
			sample:      func() any { return &ToolBeforePayload{Name: "bash", Arguments: `{"cmd":"ls"}`} },
			replaceJSON: `{"name":"bash","arguments":"{\"cmd\":\"pwd\"}"}`,
			checkReplaced: func(t *testing.T, payload any) {
				t.Helper()
				if got := payload.(*ToolBeforePayload).Arguments; !strings.Contains(got, "pwd") {
					t.Fatalf("Arguments = %q, want a pwd command", got)
				}
			},
			violateJSON: `{"name":"bash","arguments":"not json"}`,
		},
		{
			point:       extension.PointToolAfter,
			sample:      func() any { return &ToolAfterPayload{Name: "bash", Arguments: `{"cmd":"ls"}`, Result: "out"} },
			replaceJSON: `{"name":"bash","result":"new out"}`,
			checkReplaced: func(t *testing.T, payload any) {
				t.Helper()
				got := payload.(*ToolAfterPayload)
				if got.Result != "new out" || got.Arguments != "" {
					t.Fatalf("payload = %+v, want new result and cleared arguments", got)
				}
			},
			violateJSON: `{}`,
		},
		{
			point: extension.PointPermissionDecision,
			sample: func() any {
				return &PermissionPayload{Name: "bash", Arguments: `{"cmd":"rm -rf x"}`, HostDecision: "deny"}
			},
			replaceJSON: `{"name":"bash","arguments":"{\"cmd\":\"ls\"}","hostDecision":"deny"}`,
			checkReplaced: func(t *testing.T, payload any) {
				t.Helper()
				if got := payload.(*PermissionPayload).Arguments; !strings.Contains(got, "ls") {
					t.Fatalf("Arguments = %q, want an ls command", got)
				}
			},
			violateJSON: `{"name":"bash","hostDecision":"maybe"}`,
		},
		{
			point: extension.PointCompactionPrepare,
			sample: func() any {
				return &CompactionPreparePayload{Messages: []protocol.ProviderMessage{userMessage("m")}, Guidance: "g"}
			},
			replaceJSON: `{"messages":[],"guidance":"new guidance"}`,
			checkReplaced: func(t *testing.T, payload any) {
				t.Helper()
				got := payload.(*CompactionPreparePayload)
				if got.Guidance != "new guidance" || got.Messages == nil || len(got.Messages) != 0 {
					t.Fatalf("payload = %+v, want new guidance with an empty non-nil messages array", got)
				}
			},
			violateJSON: `{}`,
		},
		{
			point:       extension.PointCompactionComplete,
			sample:      func() any { return &CompactionCompletePayload{Summary: "summary"} },
			replaceJSON: `{"summary":"new summary"}`,
			checkReplaced: func(t *testing.T, payload any) {
				t.Helper()
				if got := payload.(*CompactionCompletePayload).Summary; got != "new summary" {
					t.Fatalf("Summary = %q, want %q", got, "new summary")
				}
			},
			violateJSON: `{}`,
		},
		{
			point:       extension.PointFrontendEvent,
			sample:      func() any { return &FrontendEventPayload{Kind: "notice", Text: "t", Detail: "d"} },
			replaceJSON: `{"kind":"notice","text":"replaced text"}`,
			checkReplaced: func(t *testing.T, payload any) {
				t.Helper()
				if got := payload.(*FrontendEventPayload).Text; got != "replaced text" {
					t.Fatalf("Text = %q, want %q", got, "replaced text")
				}
			},
			violateJSON: `{}`,
		},
	}
	for _, phase := range []string{PhaseStart, PhaseEnd, PhaseLoad, PhaseSave, PhaseRotate} {
		point := extension.InterceptorPoint("session." + phase)
		cases = append(cases, pointCase{
			point:       point,
			sample:      func() any { return &SessionPayload{SessionPath: "/tmp/s.json", Phase: phase} },
			replaceJSON: fmt.Sprintf(`{"sessionPath":"/tmp/other.json","phase":%q}`, phase),
			checkReplaced: func(t *testing.T, payload any) {
				t.Helper()
				got := payload.(*SessionPayload)
				if got.SessionPath != "/tmp/other.json" || got.Phase != phase {
					t.Fatalf("payload = %+v, want replaced path at phase %q", got, phase)
				}
			},
			violateJSON: fmt.Sprintf(`{"sessionPath":"/x","phase":%q}`, "bogus"),
		})
	}
	return cases
}

// TestInterceptMatrixContinue verifies all 17 points: a continue ruling
// passes the payload through unchanged.
func TestInterceptMatrixContinue(t *testing.T) {
	for _, tc := range pointCases() {
		t.Run(string(tc.point), func(t *testing.T) {
			fake := &fakeClient{}
			warns := &warnRecorder{}
			d := buildDispatcher(
				map[extension.InterceptorPoint][]extension.Contribution{tc.point: {interceptor("p1", tc.point, 0)}},
				nil, map[string]*fakeClient{"p1": fake}, nil, warns)
			payload := tc.sample()
			result, err := d.Intercept(context.Background(), tc.point, payload)
			if err != nil {
				t.Fatalf("Intercept: %v", err)
			}
			if result.Blocked || result.Permission != nil || len(result.Applied) != 0 {
				t.Fatalf("result = %+v, want a clean pass-through", result)
			}
			if !reflect.DeepEqual(payload, tc.sample()) {
				t.Fatalf("payload = %+v, want unchanged %+v", payload, tc.sample())
			}
			if fake.interceptCount() != 1 {
				t.Fatalf("intercept calls = %d, want 1", fake.interceptCount())
			}
			if warns.count() != 0 {
				t.Fatalf("warns = %v, want none", warns.msgs)
			}
		})
	}
}

// TestInterceptMatrixBlock verifies all 17 points: a block ruling stops the
// operation and the reason is credential-redacted.
func TestInterceptMatrixBlock(t *testing.T) {
	for _, tc := range pointCases() {
		t.Run(string(tc.point), func(t *testing.T) {
			fake := &fakeClient{interceptFn: func(protocol.InterceptEvent, json.RawMessage) (protocol.InterceptResult, error) {
				return protocol.InterceptResult{Decision: protocol.DecisionBlock, Reason: "denied, token " + testSecret}, nil
			}}
			warns := &warnRecorder{}
			d := buildDispatcher(
				map[extension.InterceptorPoint][]extension.Contribution{tc.point: {interceptor("p1", tc.point, 0)}},
				nil, map[string]*fakeClient{"p1": fake}, nil, warns)
			payload := tc.sample()
			result, err := d.Intercept(context.Background(), tc.point, payload)
			if err != nil {
				t.Fatalf("Intercept: %v", err)
			}
			if !result.Blocked {
				t.Fatalf("result = %+v, want blocked", result)
			}
			if strings.Contains(result.BlockReason, testSecret) {
				t.Fatalf("BlockReason %q leaks the credential", result.BlockReason)
			}
			if !strings.Contains(result.BlockReason, "denied, token") {
				t.Fatalf("BlockReason %q lost the human-readable reason", result.BlockReason)
			}
			if result.BlockReason == "denied, token "+testSecret {
				t.Fatalf("BlockReason was not redacted at all")
			}
		})
	}
}

// TestInterceptMatrixReplace verifies all 17 points: a replace ruling
// substitutes the payload and the caller observes the new value.
func TestInterceptMatrixReplace(t *testing.T) {
	for _, tc := range pointCases() {
		t.Run(string(tc.point), func(t *testing.T) {
			fake := &fakeClient{interceptFn: func(protocol.InterceptEvent, json.RawMessage) (protocol.InterceptResult, error) {
				return protocol.InterceptResult{Decision: protocol.DecisionReplace, Replacement: json.RawMessage(tc.replaceJSON)}, nil
			}}
			warns := &warnRecorder{}
			d := buildDispatcher(
				map[extension.InterceptorPoint][]extension.Contribution{tc.point: {interceptor("p1", tc.point, 0)}},
				nil, map[string]*fakeClient{"p1": fake}, nil, warns)
			payload := tc.sample()
			result, err := d.Intercept(context.Background(), tc.point, payload)
			if err != nil {
				t.Fatalf("Intercept: %v", err)
			}
			tc.checkReplaced(t, payload)
			if !reflect.DeepEqual(result.Applied, []string{"p1"}) {
				t.Fatalf("Applied = %v, want [p1]", result.Applied)
			}
			if warns.count() != 0 {
				t.Fatalf("warns = %v, want none", warns.msgs)
			}
		})
	}
}

// TestInterceptMatrixInvalidReplace verifies all 17 points: a replacement
// with unknown fields or one that fails Validate is a protocol violation —
// optional extensions are warned about once and skipped (payload unchanged),
// required extensions fail the operation.
func TestInterceptMatrixInvalidReplace(t *testing.T) {
	badPayloads := map[string]string{
		"unknown field":   `{"bogusField":1}`,
		"failed validate": "", // filled per point from violateJSON
	}
	for _, tc := range pointCases() {
		for name, bad := range badPayloads {
			if name == "failed validate" {
				bad = tc.violateJSON
			}
			t.Run(string(tc.point)+"/"+name+"_optional", func(t *testing.T) {
				fake := &fakeClient{interceptFn: func(protocol.InterceptEvent, json.RawMessage) (protocol.InterceptResult, error) {
					return protocol.InterceptResult{Decision: protocol.DecisionReplace, Replacement: json.RawMessage(bad)}, nil
				}}
				warns := &warnRecorder{}
				d := buildDispatcher(
					map[extension.InterceptorPoint][]extension.Contribution{tc.point: {interceptor("p1", tc.point, 0)}},
					nil, map[string]*fakeClient{"p1": fake}, nil, warns)
				payload := tc.sample()
				result, err := d.Intercept(context.Background(), tc.point, payload)
				if err != nil {
					t.Fatalf("Intercept: optional violation must not fail, got %v", err)
				}
				if result.Blocked || len(result.Applied) != 0 {
					t.Fatalf("result = %+v, want the ruling skipped", result)
				}
				if !reflect.DeepEqual(payload, tc.sample()) {
					t.Fatalf("payload = %+v, want unchanged %+v", payload, tc.sample())
				}
				if warns.count() != 1 || !warns.contains("p1") {
					t.Fatalf("warns = %v, want one warning naming p1", warns.msgs)
				}
			})
			t.Run(string(tc.point)+"/"+name+"_required", func(t *testing.T) {
				fake := &fakeClient{interceptFn: func(protocol.InterceptEvent, json.RawMessage) (protocol.InterceptResult, error) {
					return protocol.InterceptResult{Decision: protocol.DecisionReplace, Replacement: json.RawMessage(bad)}, nil
				}}
				warns := &warnRecorder{}
				d := buildDispatcher(
					map[extension.InterceptorPoint][]extension.Contribution{tc.point: {interceptor("p1", tc.point, 0)}},
					nil, map[string]*fakeClient{"p1": fake}, map[string]bool{"p1": true}, warns)
				payload := tc.sample()
				_, err := d.Intercept(context.Background(), tc.point, payload)
				var violation *ViolationError
				if !errors.As(err, &violation) {
					t.Fatalf("err = %v (%T), want *ViolationError", err, err)
				}
				if violation.Plugin != "p1" || violation.Point != tc.point {
					t.Fatalf("violation = %+v, want p1 at %s", violation, tc.point)
				}
			})
		}
	}
}

// TestInterceptMatrixAllowDenyRejected verifies the 16 non-permission points:
// allow/deny rulings there are a protocol violation.
func TestInterceptMatrixAllowDenyRejected(t *testing.T) {
	for _, tc := range pointCases() {
		if tc.point == extension.PointPermissionDecision {
			continue
		}
		for _, decision := range []protocol.InterceptDecision{protocol.DecisionAllow, protocol.DecisionDeny} {
			t.Run(string(tc.point)+"/"+string(decision)+"_optional", func(t *testing.T) {
				fake := &fakeClient{interceptFn: func(protocol.InterceptEvent, json.RawMessage) (protocol.InterceptResult, error) {
					return protocol.InterceptResult{Decision: decision}, nil
				}}
				warns := &warnRecorder{}
				d := buildDispatcher(
					map[extension.InterceptorPoint][]extension.Contribution{tc.point: {interceptor("p1", tc.point, 0)}},
					nil, map[string]*fakeClient{"p1": fake}, nil, warns)
				payload := tc.sample()
				result, err := d.Intercept(context.Background(), tc.point, payload)
				if err != nil {
					t.Fatalf("Intercept: optional violation must not fail, got %v", err)
				}
				if result.Permission != nil {
					t.Fatalf("Permission = %v, want nil outside permission.decision", *result.Permission)
				}
				if warns.count() != 1 || !warns.contains("only legal") {
					t.Fatalf("warns = %v, want one warning about the illegal decision", warns.msgs)
				}
			})
			t.Run(string(tc.point)+"/"+string(decision)+"_required", func(t *testing.T) {
				fake := &fakeClient{interceptFn: func(protocol.InterceptEvent, json.RawMessage) (protocol.InterceptResult, error) {
					return protocol.InterceptResult{Decision: decision}, nil
				}}
				warns := &warnRecorder{}
				d := buildDispatcher(
					map[extension.InterceptorPoint][]extension.Contribution{tc.point: {interceptor("p1", tc.point, 0)}},
					nil, map[string]*fakeClient{"p1": fake}, map[string]bool{"p1": true}, warns)
				payload := tc.sample()
				_, err := d.Intercept(context.Background(), tc.point, payload)
				var violation *ViolationError
				if !errors.As(err, &violation) {
					t.Fatalf("err = %v (%T), want *ViolationError", err, err)
				}
				if !strings.Contains(violation.Detail, "only legal") {
					t.Fatalf("violation detail = %q, want the legality explanation", violation.Detail)
				}
			})
		}
	}
}

// TestInterceptChainOrder verifies three extensions observe replaced payloads
// in exact chain order (priority ascending dominates plugin ID).
func TestInterceptChainOrder(t *testing.T) {
	point := extension.PointInputReceive
	// Deliberately unordered, with priority order opposite to plugin-ID order.
	contribs := extension.SortInterceptors([]extension.Contribution{
		interceptor("zeta", point, 5),
		interceptor("alpha", point, -3),
		interceptor("mid", point, 0),
	})
	appendSelf := func(pluginID string) func(protocol.InterceptEvent, json.RawMessage) (protocol.InterceptResult, error) {
		return func(_ protocol.InterceptEvent, raw json.RawMessage) (protocol.InterceptResult, error) {
			var payload InputPayload
			if err := json.Unmarshal(raw, &payload); err != nil {
				return protocol.InterceptResult{}, err
			}
			replacement, _ := json.Marshal(InputPayload{Text: payload.Text + ">" + pluginID})
			return protocol.InterceptResult{Decision: protocol.DecisionReplace, Replacement: replacement}, nil
		}
	}
	fakes := map[string]*fakeClient{
		"alpha": {interceptFn: appendSelf("alpha")},
		"mid":   {interceptFn: appendSelf("mid")},
		"zeta":  {interceptFn: appendSelf("zeta")},
	}
	warns := &warnRecorder{}
	d := buildDispatcher(map[extension.InterceptorPoint][]extension.Contribution{point: contribs}, nil, fakes, nil, warns)

	payload := &InputPayload{Text: "start"}
	result, err := d.Intercept(context.Background(), point, payload)
	if err != nil {
		t.Fatalf("Intercept: %v", err)
	}
	if want := "start>alpha>mid>zeta"; payload.Text != want {
		t.Fatalf("Text = %q, want %q", payload.Text, want)
	}
	if want := []string{"alpha", "mid", "zeta"}; !reflect.DeepEqual(result.Applied, want) {
		t.Fatalf("Applied = %v, want %v", result.Applied, want)
	}
	// Each extension observed exactly the value its predecessor produced.
	wantSeen := map[string]string{"alpha": "start", "mid": "start>alpha", "zeta": "start>alpha>mid"}
	for pluginID, want := range wantSeen {
		observed := fakes[pluginID].observedPayloads()
		if len(observed) != 1 {
			t.Fatalf("%s observed %d payloads, want 1", pluginID, len(observed))
		}
		var seen InputPayload
		if err := json.Unmarshal(observed[0], &seen); err != nil {
			t.Fatalf("%s observed payload: %v", pluginID, err)
		}
		if seen.Text != want {
			t.Fatalf("%s observed %q, want %q", pluginID, seen.Text, want)
		}
	}
}

func TestPermissionAllowOverridesHostDeny(t *testing.T) {
	point := extension.PointPermissionDecision
	fake := &fakeClient{interceptFn: func(protocol.InterceptEvent, json.RawMessage) (protocol.InterceptResult, error) {
		return protocol.InterceptResult{Decision: protocol.DecisionAllow}, nil
	}}
	warns := &warnRecorder{}
	d := buildDispatcher(
		map[extension.InterceptorPoint][]extension.Contribution{point: {interceptor("ext-sec", point, 0)}},
		nil, map[string]*fakeClient{"ext-sec": fake}, nil, warns)
	payload := &PermissionPayload{Name: "bash", Arguments: `{"cmd":"rm -rf x"}`, HostDecision: "deny"}
	result, err := d.Intercept(context.Background(), point, payload)
	if err != nil {
		t.Fatalf("Intercept: %v", err)
	}
	if result.Permission == nil || !*result.Permission {
		t.Fatalf("Permission = %v, want allow", result.Permission)
	}
	if len(result.Audit) != 1 || !strings.Contains(result.Audit[0], "ext-sec") || !strings.Contains(result.Audit[0], "host deny") {
		t.Fatalf("Audit = %v, want one override note naming ext-sec", result.Audit)
	}
}

func TestPermissionDeny(t *testing.T) {
	point := extension.PointPermissionDecision
	fake := &fakeClient{interceptFn: func(protocol.InterceptEvent, json.RawMessage) (protocol.InterceptResult, error) {
		return protocol.InterceptResult{Decision: protocol.DecisionDeny}, nil
	}}
	warns := &warnRecorder{}
	d := buildDispatcher(
		map[extension.InterceptorPoint][]extension.Contribution{point: {interceptor("ext-sec", point, 0)}},
		nil, map[string]*fakeClient{"ext-sec": fake}, nil, warns)
	payload := &PermissionPayload{Name: "bash", HostDecision: "allow"}
	result, err := d.Intercept(context.Background(), point, payload)
	if err != nil {
		t.Fatalf("Intercept: %v", err)
	}
	if result.Permission == nil || *result.Permission {
		t.Fatalf("Permission = %v, want deny", result.Permission)
	}
	if len(result.Audit) != 0 {
		t.Fatalf("Audit = %v, want none for a deny", result.Audit)
	}
}

func TestPermissionContinueLeavesHostDecision(t *testing.T) {
	point := extension.PointPermissionDecision
	fake := &fakeClient{}
	warns := &warnRecorder{}
	d := buildDispatcher(
		map[extension.InterceptorPoint][]extension.Contribution{point: {interceptor("ext-sec", point, 0)}},
		nil, map[string]*fakeClient{"ext-sec": fake}, nil, warns)
	payload := &PermissionPayload{Name: "bash", HostDecision: "deny"}
	result, err := d.Intercept(context.Background(), point, payload)
	if err != nil {
		t.Fatalf("Intercept: %v", err)
	}
	if result.Permission != nil {
		t.Fatalf("Permission = %v, want nil (host decision stands)", *result.Permission)
	}
}

// TestPermissionFirstRulingTerminal verifies the first allow/deny ends the
// extension phase: later interceptors are never called.
func TestPermissionFirstRulingTerminal(t *testing.T) {
	point := extension.PointPermissionDecision
	first := &fakeClient{interceptFn: func(protocol.InterceptEvent, json.RawMessage) (protocol.InterceptResult, error) {
		return protocol.InterceptResult{Decision: protocol.DecisionAllow}, nil
	}}
	second := &fakeClient{interceptFn: func(protocol.InterceptEvent, json.RawMessage) (protocol.InterceptResult, error) {
		return protocol.InterceptResult{Decision: protocol.DecisionDeny}, nil
	}}
	warns := &warnRecorder{}
	d := buildDispatcher(
		map[extension.InterceptorPoint][]extension.Contribution{point: {
			interceptor("aaa-first", point, 0), interceptor("zzz-second", point, 1),
		}},
		nil, map[string]*fakeClient{"aaa-first": first, "zzz-second": second}, nil, warns)
	payload := &PermissionPayload{Name: "bash", HostDecision: "deny"}
	result, err := d.Intercept(context.Background(), point, payload)
	if err != nil {
		t.Fatalf("Intercept: %v", err)
	}
	if result.Permission == nil || !*result.Permission {
		t.Fatalf("Permission = %v, want the first ruling (allow)", result.Permission)
	}
	if second.interceptCount() != 0 {
		t.Fatalf("second interceptor called %d times after a terminal ruling", second.interceptCount())
	}
}

// TestPermissionBlock verifies block remains legal at permission.decision and
// reports a redacted reason.
func TestPermissionBlock(t *testing.T) {
	point := extension.PointPermissionDecision
	fake := &fakeClient{interceptFn: func(protocol.InterceptEvent, json.RawMessage) (protocol.InterceptResult, error) {
		return protocol.InterceptResult{Decision: protocol.DecisionBlock, Reason: "suspicious, token " + testSecret}, nil
	}}
	warns := &warnRecorder{}
	d := buildDispatcher(
		map[extension.InterceptorPoint][]extension.Contribution{point: {interceptor("ext-sec", point, 0)}},
		nil, map[string]*fakeClient{"ext-sec": fake}, nil, warns)
	payload := &PermissionPayload{Name: "bash", HostDecision: "allow"}
	result, err := d.Intercept(context.Background(), point, payload)
	if err != nil {
		t.Fatalf("Intercept: %v", err)
	}
	if !result.Blocked || result.Permission != nil {
		t.Fatalf("result = %+v, want blocked with no permission ruling", result)
	}
	if strings.Contains(result.BlockReason, testSecret) {
		t.Fatalf("BlockReason %q leaks the credential", result.BlockReason)
	}
}

func TestStrategyOwnerReplacesSystemPrompt(t *testing.T) {
	fake := &fakeClient{interceptFn: func(event protocol.InterceptEvent, _ json.RawMessage) (protocol.InterceptResult, error) {
		if event != protocol.EventSystemPromptBuild {
			t.Errorf("strategy event = %q, want %q", event, protocol.EventSystemPromptBuild)
		}
		return protocol.InterceptResult{Decision: protocol.DecisionReplace, Replacement: json.RawMessage(`{"prompt":"owned","workspaceRoot":"/ws"}`)}, nil
	}}
	warns := &warnRecorder{}
	d := buildDispatcher(nil,
		map[extension.Slot]extension.ContributionSource{extension.SlotSystemPrompt: owner("prompt-owner")},
		map[string]*fakeClient{"prompt-owner": fake}, nil, warns)
	payload := &SystemPromptPayload{Prompt: "host default", WorkspaceRoot: "/ws"}
	if err := d.RunStrategy(context.Background(), extension.SlotSystemPrompt, extension.PointSystemPromptBuild, payload); err != nil {
		t.Fatalf("RunStrategy: %v", err)
	}
	if payload.Prompt != "owned" {
		t.Fatalf("Prompt = %q, want the owner's replacement", payload.Prompt)
	}
}

// TestStrategyOwnerTimeoutIsFatal verifies a strategy owner's timeout always
// fails the operation (slot owners are required-class even without
// required:true).
func TestStrategyOwnerTimeoutIsFatal(t *testing.T) {
	fake := &fakeClient{interceptFn: func(protocol.InterceptEvent, json.RawMessage) (protocol.InterceptResult, error) {
		return protocol.InterceptResult{}, timeoutError("prompt-owner", extension.PointSystemPromptBuild)
	}}
	warns := &warnRecorder{}
	d := buildDispatcher(nil,
		map[extension.Slot]extension.ContributionSource{extension.SlotSystemPrompt: owner("prompt-owner")},
		map[string]*fakeClient{"prompt-owner": fake}, nil, warns)
	payload := &SystemPromptPayload{Prompt: "host default", WorkspaceRoot: "/ws"}
	err := d.RunStrategy(context.Background(), extension.SlotSystemPrompt, extension.PointSystemPromptBuild, payload)
	var failure *FailureError
	if !errors.As(err, &failure) {
		t.Fatalf("err = %v (%T), want *FailureError", err, err)
	}
	var protocolErr *protocol.ProtocolError
	if !errors.As(err, &protocolErr) || protocolErr.Reason != protocol.ErrInterceptTimeout {
		t.Fatalf("err = %v, want the wrapped intercept_timeout protocol error", err)
	}
	if payload.Prompt != "host default" {
		t.Fatalf("Prompt = %q, want the host default untouched on failure", payload.Prompt)
	}
}

// TestStrategyNoOwnerKeepsHostDefault verifies an unowned slot is a no-op.
func TestStrategyNoOwnerKeepsHostDefault(t *testing.T) {
	warns := &warnRecorder{}
	d := buildDispatcher(nil, nil, nil, nil, warns)
	if _, ok := d.Strategy(extension.SlotSystemPrompt); ok {
		t.Fatalf("Strategy reported an owner for an unowned slot")
	}
	payload := &SystemPromptPayload{Prompt: "host default", WorkspaceRoot: "/ws"}
	if err := d.RunStrategy(context.Background(), extension.SlotSystemPrompt, extension.PointSystemPromptBuild, payload); err != nil {
		t.Fatalf("RunStrategy: %v", err)
	}
	if payload.Prompt != "host default" {
		t.Fatalf("Prompt = %q, want the host default", payload.Prompt)
	}
}

// TestStrategyNonOwnerCannotClaim verifies chain membership at a point does
// not make an extension the strategy owner: only the Replacements owner gets
// the strategy call.
func TestStrategyNonOwnerCannotClaim(t *testing.T) {
	point := extension.PointSystemPromptBuild
	observer := &fakeClient{}
	owned := &fakeClient{}
	warns := &warnRecorder{}
	d := buildDispatcher(
		map[extension.InterceptorPoint][]extension.Contribution{point: {interceptor("observer", point, 0)}},
		map[extension.Slot]extension.ContributionSource{extension.SlotSystemPrompt: owner("prompt-owner")},
		map[string]*fakeClient{"observer": observer, "prompt-owner": owned}, nil, warns)
	client, ok := d.Strategy(extension.SlotSystemPrompt)
	if !ok {
		t.Fatalf("Strategy reported no owner")
	}
	if client != owned {
		t.Fatalf("Strategy returned the wrong client: the chain observer must not claim the slot")
	}
	payload := &SystemPromptPayload{Prompt: "host default", WorkspaceRoot: "/ws"}
	if err := d.RunStrategy(context.Background(), extension.SlotSystemPrompt, point, payload); err != nil {
		t.Fatalf("RunStrategy: %v", err)
	}
	if observer.interceptCount() != 0 {
		t.Fatalf("non-owner received %d strategy calls", observer.interceptCount())
	}
	if owned.interceptCount() != 1 {
		t.Fatalf("owner received %d strategy calls, want 1", owned.interceptCount())
	}
}

// TestStrategyRulingPolicy verifies strategy owners may only continue or
// replace; block is fatal with a redacted reason, allow/deny and invalid
// replacements are fatal contract violations.
func TestStrategyRulingPolicy(t *testing.T) {
	point := extension.PointSystemPromptBuild
	newDispatcher := func(answer protocol.InterceptResult) (*Dispatcher, *SystemPromptPayload) {
		fake := &fakeClient{interceptFn: func(protocol.InterceptEvent, json.RawMessage) (protocol.InterceptResult, error) {
			return answer, nil
		}}
		warns := &warnRecorder{}
		d := buildDispatcher(nil,
			map[extension.Slot]extension.ContributionSource{extension.SlotSystemPrompt: owner("prompt-owner")},
			map[string]*fakeClient{"prompt-owner": fake}, nil, warns)
		return d, &SystemPromptPayload{Prompt: "host default", WorkspaceRoot: "/ws"}
	}

	t.Run("continue_keeps_default", func(t *testing.T) {
		d, payload := newDispatcher(protocol.InterceptResult{Decision: protocol.DecisionContinue})
		if err := d.RunStrategy(context.Background(), extension.SlotSystemPrompt, point, payload); err != nil {
			t.Fatalf("RunStrategy: %v", err)
		}
		if payload.Prompt != "host default" {
			t.Fatalf("Prompt = %q, want the host default", payload.Prompt)
		}
	})
	t.Run("block_is_fatal_and_redacted", func(t *testing.T) {
		d, payload := newDispatcher(protocol.InterceptResult{Decision: protocol.DecisionBlock, Reason: "no, token " + testSecret})
		err := d.RunStrategy(context.Background(), extension.SlotSystemPrompt, point, payload)
		var blocked *BlockError
		if !errors.As(err, &blocked) {
			t.Fatalf("err = %v (%T), want *BlockError", err, err)
		}
		if strings.Contains(err.Error(), testSecret) {
			t.Fatalf("block error %q leaks the credential", err)
		}
	})
	t.Run("allow_is_violation", func(t *testing.T) {
		d, payload := newDispatcher(protocol.InterceptResult{Decision: protocol.DecisionAllow})
		err := d.RunStrategy(context.Background(), extension.SlotSystemPrompt, point, payload)
		var violation *ViolationError
		if !errors.As(err, &violation) {
			t.Fatalf("err = %v (%T), want *ViolationError", err, err)
		}
	})
	t.Run("invalid_replace_is_violation", func(t *testing.T) {
		d, payload := newDispatcher(protocol.InterceptResult{Decision: protocol.DecisionReplace, Replacement: json.RawMessage(`{"bogus":1}`)})
		err := d.RunStrategy(context.Background(), extension.SlotSystemPrompt, point, payload)
		var violation *ViolationError
		if !errors.As(err, &violation) {
			t.Fatalf("err = %v (%T), want *ViolationError", err, err)
		}
		if payload.Prompt != "host default" {
			t.Fatalf("Prompt = %q, want the host default untouched", payload.Prompt)
		}
	})
}

// TestOptionalTimeoutWarnsOnce verifies an optional extension's timeout is
// warned about exactly once per process and skipped.
func TestOptionalTimeoutWarnsOnce(t *testing.T) {
	point := extension.PointToolBefore
	fake := &fakeClient{interceptFn: func(protocol.InterceptEvent, json.RawMessage) (protocol.InterceptResult, error) {
		return protocol.InterceptResult{}, timeoutError("opt", point)
	}}
	warns := &warnRecorder{}
	d := buildDispatcher(
		map[extension.InterceptorPoint][]extension.Contribution{point: {interceptor("opt", point, 0)}},
		nil, map[string]*fakeClient{"opt": fake}, nil, warns)
	for i := range 2 {
		payload := &ToolBeforePayload{Name: "bash", Arguments: `{"cmd":"ls"}`}
		result, err := d.Intercept(context.Background(), point, payload)
		if err != nil {
			t.Fatalf("call %d: optional timeout must not fail, got %v", i, err)
		}
		if result.Blocked || len(result.Applied) != 0 {
			t.Fatalf("call %d: result = %+v, want the extension skipped", i, result)
		}
		if payload.Name != "bash" {
			t.Fatalf("call %d: payload changed to %+v", i, payload)
		}
	}
	if warns.count() != 1 {
		t.Fatalf("warns = %v, want exactly one warning across two timeouts", warns.msgs)
	}
	if !warns.contains("opt") || !warns.contains("skipping") {
		t.Fatalf("warn %v must name the plugin and the skip", warns.msgs)
	}
}

// TestRequiredTimeoutFails verifies a required extension's timeout fails the
// operation and preserves the frozen protocol error for errors.As.
func TestRequiredTimeoutFails(t *testing.T) {
	point := extension.PointToolBefore
	fake := &fakeClient{interceptFn: func(protocol.InterceptEvent, json.RawMessage) (protocol.InterceptResult, error) {
		return protocol.InterceptResult{}, timeoutError("req", point)
	}}
	warns := &warnRecorder{}
	d := buildDispatcher(
		map[extension.InterceptorPoint][]extension.Contribution{point: {interceptor("req", point, 0)}},
		nil, map[string]*fakeClient{"req": fake}, map[string]bool{"req": true}, warns)
	payload := &ToolBeforePayload{Name: "bash"}
	_, err := d.Intercept(context.Background(), point, payload)
	var failure *FailureError
	if !errors.As(err, &failure) {
		t.Fatalf("err = %v (%T), want *FailureError", err, err)
	}
	var protocolErr *protocol.ProtocolError
	if !errors.As(err, &protocolErr) || protocolErr.Reason != protocol.ErrInterceptTimeout {
		t.Fatalf("err = %v, want the wrapped intercept_timeout protocol error", err)
	}
}

// TestSlotOwnerTimeoutFails verifies slot ownership alone (no required:true)
// upgrades an extension to required-class error policy.
func TestSlotOwnerTimeoutFails(t *testing.T) {
	point := extension.PointInputReceive
	fake := &fakeClient{interceptFn: func(protocol.InterceptEvent, json.RawMessage) (protocol.InterceptResult, error) {
		return protocol.InterceptResult{}, timeoutError("ctx-owner", point)
	}}
	warns := &warnRecorder{}
	d := buildDispatcher(
		map[extension.InterceptorPoint][]extension.Contribution{point: {interceptor("ctx-owner", point, 0)}},
		map[extension.Slot]extension.ContributionSource{extension.SlotContext: owner("ctx-owner")},
		map[string]*fakeClient{"ctx-owner": fake}, nil, warns)
	payload := &InputPayload{Text: "hi"}
	if _, err := d.Intercept(context.Background(), point, payload); err == nil {
		t.Fatalf("slot owner's timeout must fail the operation")
	}
	if warns.count() != 0 {
		t.Fatalf("warns = %v, want none for a required-class failure", warns.msgs)
	}
}

// TestMissingClientPolicy verifies a chain member with no live sidecar client
// follows the same optional/required policy.
func TestMissingClientPolicy(t *testing.T) {
	point := extension.PointInputReceive
	t.Run("optional", func(t *testing.T) {
		warns := &warnRecorder{}
		d := buildDispatcher(
			map[extension.InterceptorPoint][]extension.Contribution{point: {interceptor("gone", point, 0)}},
			nil, nil, nil, warns)
		payload := &InputPayload{Text: "hi"}
		if _, err := d.Intercept(context.Background(), point, payload); err != nil {
			t.Fatalf("optional missing client must not fail, got %v", err)
		}
		if warns.count() != 1 {
			t.Fatalf("warns = %v, want one warning", warns.msgs)
		}
	})
	t.Run("required", func(t *testing.T) {
		warns := &warnRecorder{}
		d := buildDispatcher(
			map[extension.InterceptorPoint][]extension.Contribution{point: {interceptor("gone", point, 0)}},
			nil, nil, map[string]bool{"gone": true}, warns)
		payload := &InputPayload{Text: "hi"}
		var failure *FailureError
		if _, err := d.Intercept(context.Background(), point, payload); !errors.As(err, &failure) {
			t.Fatalf("err = %v, want *FailureError", err)
		}
	})
}

// TestSessionPhaseMustMatchPoint verifies a session replacement whose phase
// disagrees with the dispatched point is a contract violation.
func TestSessionPhaseMustMatchPoint(t *testing.T) {
	point := extension.PointSessionStart
	fake := &fakeClient{interceptFn: func(protocol.InterceptEvent, json.RawMessage) (protocol.InterceptResult, error) {
		return protocol.InterceptResult{Decision: protocol.DecisionReplace, Replacement: json.RawMessage(`{"sessionPath":"/x","phase":"end"}`)}, nil
	}}
	t.Run("optional", func(t *testing.T) {
		warns := &warnRecorder{}
		d := buildDispatcher(
			map[extension.InterceptorPoint][]extension.Contribution{point: {interceptor("p1", point, 0)}},
			nil, map[string]*fakeClient{"p1": fake}, nil, warns)
		payload := &SessionPayload{SessionPath: "/tmp/s.json", Phase: PhaseStart}
		if _, err := d.Intercept(context.Background(), point, payload); err != nil {
			t.Fatalf("optional violation must not fail, got %v", err)
		}
		if payload.SessionPath != "/tmp/s.json" {
			t.Fatalf("payload = %+v, want unchanged", payload)
		}
		if !warns.contains("does not match") {
			t.Fatalf("warns = %v, want the phase-mismatch explanation", warns.msgs)
		}
	})
	t.Run("required", func(t *testing.T) {
		warns := &warnRecorder{}
		d := buildDispatcher(
			map[extension.InterceptorPoint][]extension.Contribution{point: {interceptor("p1", point, 0)}},
			nil, map[string]*fakeClient{"p1": fake}, map[string]bool{"p1": true}, warns)
		payload := &SessionPayload{SessionPath: "/tmp/s.json", Phase: PhaseStart}
		var violation *ViolationError
		if _, err := d.Intercept(context.Background(), point, payload); !errors.As(err, &violation) {
			t.Fatalf("err = %v, want *ViolationError", err)
		}
	})
}

// TestEventNotifiesChainAndSlotObservers verifies fire-and-forget delivery to
// chain members and slot observers (deduplicated), best-effort on error.
func TestEventNotifiesChainAndSlotObservers(t *testing.T) {
	point := extension.PointSystemPromptBuild
	p1 := &fakeClient{}
	p2 := &fakeClient{notifyFn: func(protocol.InterceptEvent, json.RawMessage) error {
		return errors.New("notify blew up, token " + testSecret)
	}}
	p3 := &fakeClient{}
	warns := &warnRecorder{}
	d := buildDispatcher(
		map[extension.InterceptorPoint][]extension.Contribution{point: {
			interceptor("p1", point, 0), interceptor("p2", point, 1), interceptor("p3", point, 2),
		}},
		// p3 is both a chain member and the slot owner: it must be notified once.
		map[extension.Slot]extension.ContributionSource{extension.SlotSystemPrompt: owner("p3")},
		map[string]*fakeClient{"p1": p1, "p2": p2, "p3": p3}, nil, warns)
	d.Event(point, &SystemPromptPayload{Prompt: "p", WorkspaceRoot: "/ws"})
	for pluginID, fake := range map[string]*fakeClient{"p1": p1, "p2": p2, "p3": p3} {
		if fake.notifyCount() != 1 {
			t.Fatalf("%s notifyCount = %d, want 1", pluginID, fake.notifyCount())
		}
	}
	if warns.count() != 1 || !warns.contains("p2") {
		t.Fatalf("warns = %v, want one warning naming p2", warns.msgs)
	}
	if warns.contains(testSecret) {
		t.Fatalf("warning leaks the credential: %v", warns.msgs)
	}
}

// TestEventMarshalFailureWarns verifies an unmarshalable payload degrades to
// a warning instead of a panic.
func TestEventMarshalFailureWarns(t *testing.T) {
	warns := &warnRecorder{}
	d := buildDispatcher(nil, nil, nil, nil, warns)
	d.Event(extension.PointInputReceive, make(chan int))
	if warns.count() != 1 {
		t.Fatalf("warns = %v, want one marshal-failure warning", warns.msgs)
	}
}

// TestConcurrentDispatch hammers one Dispatcher from 32 goroutines; run with
// -race to prove the read-only dispatch path and the warn-once dedup are
// safe.
func TestConcurrentDispatch(t *testing.T) {
	inputPoint := extension.PointInputReceive
	toolPoint := extension.PointToolBefore
	replacer := &fakeClient{interceptFn: func(_ protocol.InterceptEvent, raw json.RawMessage) (protocol.InterceptResult, error) {
		var payload InputPayload
		if err := json.Unmarshal(raw, &payload); err != nil {
			return protocol.InterceptResult{}, err
		}
		replacement, _ := json.Marshal(InputPayload{Text: payload.Text + ">p2"})
		return protocol.InterceptResult{Decision: protocol.DecisionReplace, Replacement: replacement}, nil
	}}
	failing := &fakeClient{interceptFn: func(protocol.InterceptEvent, json.RawMessage) (protocol.InterceptResult, error) {
		return protocol.InterceptResult{}, timeoutError("p3", inputPoint)
	}}
	fakes := map[string]*fakeClient{"p1": {}, "p2": replacer, "p3": failing}
	warns := &warnRecorder{}
	d := buildDispatcher(map[extension.InterceptorPoint][]extension.Contribution{
		inputPoint: {interceptor("p1", inputPoint, 0), interceptor("p2", inputPoint, 1), interceptor("p3", inputPoint, 2)},
		toolPoint:  {interceptor("p1", toolPoint, 0)},
	}, nil, fakes, nil, warns)

	var wg sync.WaitGroup
	errs := make(chan error, 32)
	for i := range 32 {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if i%2 == 0 {
				payload := &InputPayload{Text: fmt.Sprintf("turn-%d", i)}
				result, err := d.Intercept(context.Background(), inputPoint, payload)
				if err != nil {
					errs <- err
					return
				}
				if want := fmt.Sprintf("turn-%d>p2", i); payload.Text != want {
					errs <- fmt.Errorf("payload = %q, want %q", payload.Text, want)
				}
				if !reflect.DeepEqual(result.Applied, []string{"p2"}) {
					errs <- fmt.Errorf("Applied = %v, want [p2]", result.Applied)
				}
			} else {
				payload := &ToolBeforePayload{Name: "bash", Arguments: `{}`}
				if _, err := d.Intercept(context.Background(), toolPoint, payload); err != nil {
					errs <- err
				}
			}
			d.Event(inputPoint, &InputPayload{Text: "observed"})
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
	// p3 timed out from 16 goroutines but warns exactly once.
	if warns.count() != 1 {
		t.Fatalf("warns = %v, want one deduplicated warning", warns.msgs)
	}
}

// TestFrozenInputs verifies New deep-copies its inputs: mutating the caller's
// chain, replacements, or required map afterwards cannot change dispatch
// behavior, and per-turn payloads never touch the frozen chain.
func TestFrozenInputs(t *testing.T) {
	point := extension.PointInputReceive
	real := &fakeClient{}
	evil := &fakeClient{}
	chain := map[extension.InterceptorPoint][]extension.Contribution{
		point: {interceptor("real", point, 0)},
	}
	replacements := map[extension.Slot]extension.ContributionSource{extension.SlotSystemPrompt: owner("real")}
	required := map[string]bool{"real": true}
	warns := &warnRecorder{}
	d := New(chain, replacements, func(pluginID string) Client {
		if pluginID == "evil" {
			return evil
		}
		return real
	}, required, Options{Warn: warns.warn})

	// Mutate every input after construction.
	chain[point][0] = interceptor("evil", point, 0)
	chain[point] = append(chain[point], interceptor("evil", point, 1))
	replacements[extension.SlotSystemPrompt] = owner("evil")
	delete(required, "real")

	payload := &InputPayload{Text: "hi"}
	if _, err := d.Intercept(context.Background(), point, payload); err != nil {
		t.Fatalf("Intercept: %v", err)
	}
	if real.interceptCount() != 1 || evil.interceptCount() != 0 {
		t.Fatalf("intercepts real=%d evil=%d, want 1 and 0", real.interceptCount(), evil.interceptCount())
	}
	client, ok := d.Strategy(extension.SlotSystemPrompt)
	if !ok || client != real {
		t.Fatalf("Strategy owner changed after the replacements map was mutated")
	}

	// The required set is frozen too: "real" still fails rather than warns.
	failing := New(
		map[extension.InterceptorPoint][]extension.Contribution{point: {interceptor("real", point, 0)}},
		nil, func(string) Client {
			return &fakeClient{interceptFn: func(protocol.InterceptEvent, json.RawMessage) (protocol.InterceptResult, error) {
				return protocol.InterceptResult{}, timeoutError("real", point)
			}}
		}, map[string]bool{"real": true}, Options{Warn: warns.warn})
	if _, err := failing.Intercept(context.Background(), point, &InputPayload{Text: "hi"}); err == nil {
		t.Fatalf("required-class failure must fail the operation")
	}
}

// TestPayloadTypeMismatch verifies a host programming error (wrong DTO for
// the point) fails loudly instead of dispatching garbage.
func TestPayloadTypeMismatch(t *testing.T) {
	warns := &warnRecorder{}
	d := buildDispatcher(nil, nil, nil, nil, warns)
	if _, err := d.Intercept(context.Background(), extension.PointInputReceive, &ToolBeforePayload{Name: "bash"}); err == nil {
		t.Fatalf("wrong payload type must fail")
	}
	if err := d.RunStrategy(context.Background(), extension.SlotSystemPrompt, extension.PointSystemPromptBuild, &InputPayload{}); err == nil {
		t.Fatalf("wrong strategy payload type must fail")
	}
}

// TestRedactionInWarnings verifies sidecar error text surfaced through
// warnings is credential-redacted.
func TestRedactionInWarnings(t *testing.T) {
	point := extension.PointToolBefore
	fake := &fakeClient{interceptFn: func(protocol.InterceptEvent, json.RawMessage) (protocol.InterceptResult, error) {
		return protocol.InterceptResult{}, errors.New("boom, token " + testSecret)
	}}
	warns := &warnRecorder{}
	d := buildDispatcher(
		map[extension.InterceptorPoint][]extension.Contribution{point: {interceptor("opt", point, 0)}},
		nil, map[string]*fakeClient{"opt": fake}, nil, warns)
	if _, err := d.Intercept(context.Background(), point, &ToolBeforePayload{Name: "bash"}); err != nil {
		t.Fatalf("Intercept: %v", err)
	}
	if warns.contains(testSecret) {
		t.Fatalf("warning leaks the credential: %v", warns.msgs)
	}
}
