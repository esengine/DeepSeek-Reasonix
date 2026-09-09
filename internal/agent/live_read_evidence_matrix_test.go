//go:build live

package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"reasonix/internal/event"
	"reasonix/internal/extension"
	"reasonix/internal/extension/dispatch"
	"reasonix/internal/extension/protocol"
	"reasonix/internal/provider"
	"reasonix/internal/readcoord"
	"reasonix/internal/tool"
	"reasonix/internal/tool/builtin"
)

// Opt-in paid matrix. Only fixture-confined file tools are exposed; no shell,
// network tools, host configuration, or user project content enters a request.
func TestLiveReadEvidenceMatrix(t *testing.T) {
	if os.Getenv("REASONIX_LIVE_READ_EVIDENCE") != "1" {
		t.Skip("set REASONIX_LIVE_READ_EVIDENCE=1 to authorize this paid matrix")
	}
	key := os.Getenv("DEEPSEEK_API_KEY")
	if key == "" {
		t.Skip("DEEPSEEK_API_KEY not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Hour)
	defer cancel()
	budget := &liveReadBudget{cancel: cancel}
	ctx = context.WithValue(ctx, liveReadBudgetKey{}, budget)
	server := httptest.NewServer(budget)
	defer server.Close()
	totalTokens, totalRequests := 0, 0
	names := []string{"inspect_large", "full_pages", "range_tail", "range_edit", "overwrite", "multi_edit", "invalid_recovery", "unicode_crlf", "new_write", "full_budget", "independent_edit", "stale_version", "full_many_pages", "transport_cut", "disjoint_edit", "same_batch_recovery"}
	names = append(names, "completed_reread", "partial_reread", "projected_finish", "projected_reread", "same_content_files", "changed_after_complete", "repeat_budget", "pause_resume", "overwrite_exact", "overwrite_exact", "overwrite_exact")
	if selected := os.Getenv("REASONIX_LIVE_READ_SCENARIOS"); selected != "" {
		names = strings.Split(selected, ",")
	}
	for _, model := range []string{"deepseek-v4-flash", "deepseek-v4-pro"} {
		for _, effort := range []string{"disabled", "high"} {
			for _, name := range names {
				t.Run(model+"/"+effort+"/"+name, func(t *testing.T) {
					if ctx.Err() != nil || !budget.admit() || totalTokens >= 3_000_000 || totalRequests >= 600 {
						t.Skip("live suite resource ceiling reached; scenario NOT verified")
					}
					p := officialMatrixProvider(t, key, model, "chat", effort, server.URL)
					result := runReadEvidenceLiveCase(t, ctx, p, name, "deepseek/"+model)
					if strings.Contains(result.Failure, "*provider.AuthError") {
						cancel()
					}
					totalTokens += result.Prompt + result.Completion
					totalRequests += result.Requests
					data, _ := json.Marshal(result)
					t.Logf("LIVE_RESULT %s", data)
					if !result.Passed {
						t.Errorf("live invariant failed: %s", result.Failure)
					}
				})
			}
		}
	}
	t.Logf("LIVE_TOTAL requests=%d tokens=%d", totalRequests, totalTokens)
	budget.mu.Lock()
	t.Logf("LIVE_HTTP_BUDGET requests=%d charged_tokens=%d (includes unresolved reservations)", budget.requests, budget.tokens)
	budget.mu.Unlock()
}

type readEvidenceLiveResult struct {
	Scenario                               string
	Passed                                 bool
	Failure                                string
	RunError                               string
	ElapsedMS                              int64
	Prompt, Completion, CacheHit, Requests int
	Tools                                  []string
	States                                 []string
	Final                                  string
	Errors                                 []string
	Details                                []string
}

type readEvidenceLiveGate struct {
	root        string
	a           *Agent
	pageBudget  bool
	initialized bool
}

func (g *readEvidenceLiveGate) Check(_ context.Context, _ string, args json.RawMessage, _ bool) (bool, string, error) {
	var fields struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal(args, &fields); err != nil {
		return false, "invalid fixture arguments", nil
	}
	rel, err := filepath.Rel(g.root, fields.Path)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || !filepath.IsAbs(fields.Path) {
		return false, "live test only permits absolute paths inside its disposable fixture", nil
	}
	if g.pageBudget && !g.initialized {
		policy := readcoord.DefaultPolicy()
		policy.MaxPages = 1
		g.a.turn.readShadow.coord = readcoord.NewWithPolicy(policy)
		g.initialized = true
	}
	return true, "", nil
}

type readEvidenceLiveSink struct {
	incompleteReadEventSink
	onStatus func(*event.ReadStatusPayload)
}

func (s *readEvidenceLiveSink) Emit(e event.Event) {
	s.incompleteReadEventSink.Emit(e)
	if e.ReadStatus != nil && s.onStatus != nil {
		s.onStatus(e.ReadStatus)
	}
}

func runReadEvidenceLiveCase(t *testing.T, parent context.Context, p provider.Provider, name, modelRef string) readEvidenceLiveResult {
	t.Helper()
	root := t.TempDir()
	path, other := filepath.Join(root, "fixture.txt"), filepath.Join(root, "other.txt")
	content, expected, marker := "alpha\nbeta\ngamma\n", "", "ALPHA_MARKER"
	paged := func(n, at int, value string) string {
		lines := make([]string, n)
		for i := range lines {
			lines[i] = "x"
		}
		lines[at-1] = value
		return strings.Join(lines, "\n") + "\n"
	}
	var instruction string
	switch name {
	case "completed_reread", "partial_reread", "projected_finish", "projected_reread", "repeat_budget", "pause_resume":
		content = paged(2105, 2051, marker)
		instruction = "Read the ENTIRE file with intent=full, follow host continuation, and report the marker."
		switch name {
		case "completed_reread", "projected_reread":
			instruction += " After the full read is complete, call read_file with intent=full once more on this same file to verify it is unchanged, then finish."
		case "partial_reread":
			instruction += " For this duplicate-window test, repeat the first full read call once before reading the tail. Then follow host continuation and finish."
		case "repeat_budget":
			instruction = "For this bounded-read diagnostic, call read_file with intent=full repeatedly on the same file from the beginning. Do not use a cursor or offset. If the host pauses automatic reading, stop and explain that the read remains incomplete."
		}
	case "same_content_files":
		content = marker + "\n"
		instruction = fmt.Sprintf("Read the target file with intent=full, then read %s with intent=full too. Both must be checked separately. Report their marker.", other)
	case "changed_after_complete":
		content = marker + "\n"
		instruction = "Read the target file with intent=full twice in separate tool rounds to detect an external update. Report the marker in the second read."
	case "inspect_large":
		content = paged(2105, 1, marker)
		instruction = "Inspect the beginning using read_file without intent, offset, or limit. Report the marker on the first line. Do not request full-file coverage."
	case "full_pages", "full_budget", "stale_version", "full_many_pages", "transport_cut":
		content = paged(2105, 2051, marker)
		if name == "full_many_pages" {
			content = paged(6500, 6400, marker)
		}
		if name == "transport_cut" {
			content = strings.Repeat(strings.Repeat("q", 240)+"\n", 300) + marker + "\n"
		}
		instruction = "Read the ENTIRE file: start read_file with intent=full and no explicit window. Continue every missing page using host guidance before reporting the marker near the end. Never claim full coverage if reading is blocked."
	case "range_tail":
		content = paged(5000, 4900, marker)
		instruction = "Read only the ten lines starting at zero-based offset 4895 using intent=range and limit=10. Report the marker. No whole-file review is needed."
	case "range_edit":
		content = paged(5000, 4900, marker)
		expected = strings.Replace(content, marker, "UPDATED_MARKER", 1)
		instruction = "Read only offset=4895 limit=10, then use edit_file to change ALPHA_MARKER to UPDATED_MARKER. Preserve every other byte."
	case "overwrite", "overwrite_exact":
		expected = "alpha\nbeta\nupdated\n"
		instruction = "First read the whole current file. In a later tool call use write_file to replace only its last line with updated, preserving the earlier lines and final newline."
		if name == "overwrite_exact" {
			instruction = "First read the whole current file. Then use write_file to replace the last line with the exact literal text `updated` (not gamma-updated). The required final bytes are alpha, newline, beta, newline, updated, newline. Preserve the first two lines."
		}
	case "multi_edit":
		content = "alpha\nkeep\nomega\n"
		expected = "first\nkeep\nlast\n"
		instruction = "Read the current file, then use one multi_edit to change alpha to first and omega to last. Preserve the middle line and final newline."
	case "disjoint_edit":
		content = "alpha\n" + strings.Repeat("keep\n", 98) + "omega\n"
		expected = "first\n" + strings.Repeat("keep\n", 98) + "last\n"
		instruction = "Read only offset=0 limit=1 and offset=99 limit=1 (two disjoint ranges), then use multi_edit to change alpha to first and omega to last. Preserve every other byte. Do not read the middle of the file."
	case "same_batch_recovery":
		expected = "alpha\nbeta\nupdated\n"
		instruction = "In your first response call read_file and edit_file together: replace gamma with updated. If the host blocks the edit until evidence arrives, retry it in your next response after the read result. Preserve all other bytes and final newline."
	case "invalid_recovery":
		content = marker + "\n"
		instruction = fmt.Sprintf("First call read_file on %s (which does not exist). Recover from that expected error by reading the real file below and reporting its marker.", filepath.Join(root, "missing.txt"))
	case "unicode_crlf":
		marker = "你好世界_标记"
		content = "开始\r\n" + marker + "\r\n结束\r\n"
		instruction = "Read the file and report the exact Chinese marker on its second line. Do not modify the file."
	case "new_write":
		expected = "created-marker\n"
		instruction = "Create this new file using write_file with exactly created-marker followed by one newline. Do not read it before creation."
	case "independent_edit":
		content = paged(2105, 1, marker)
		expected = "alpha\nbeta\nupdated\n"
		instruction = fmt.Sprintf("First inspect the beginning of the large file below with default read_file arguments, without intent=full. Then read %s and edit its gamma line to updated, preserving everything else. The unrelated large file need not be read in full.", other)
	default:
		t.Fatalf("unknown live scenario %q", name)
	}
	if name != "new_write" {
		if err := os.WriteFile(path, []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(other, []byte("alpha\nbeta\ngamma\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if name == "same_content_files" {
		if err := os.WriteFile(other, []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	reg := tool.NewRegistry()
	reg.Add(incompleteReadBuiltin(t))
	for _, candidate := range builtin.ConfineWriters([]string{root}, builtin.SessionDataGuard{}, builtin.ManagedConfigPaths{}) {
		if candidate.Name() == "write_file" || candidate.Name() == "edit_file" || candidate.Name() == "multi_edit" {
			reg.Add(candidate)
		}
	}
	budget, _ := parent.Value(liveReadBudgetKey{}).(*liveReadBudget)
	sink := &readEvidenceLiveSink{}
	changed := false
	if name == "stale_version" {
		sink.onStatus = func(status *event.ReadStatusPayload) {
			if !changed && status.State == "needs_more" {
				changed = true
				if err := os.WriteFile(path, []byte(paged(2105, 2051, "NEW_VERSION_MARKER")), 0600); err != nil {
					t.Error("fixture mutation failed")
				}
			}
		}
	}
	if name == "changed_after_complete" {
		sink.onStatus = func(status *event.ReadStatusPayload) {
			if !changed && status.State == "satisfied" {
				changed = true
				if err := os.WriteFile(path, []byte("NEW_VERSION_MARKER\n"), 0600); err != nil {
					t.Error(err)
				}
			}
		}
		marker = "NEW_VERSION_MARKER"
	}
	gate := &readEvidenceLiveGate{root: root, pageBudget: name == "full_budget" || name == "pause_resume"}
	a := New(p, reg, NewSession("You are a file-task assistant. Follow the user's requested tools and read intent. Use only the exact supplied absolute paths. Recover from actionable tool errors. Never invent unseen file content. Keep the final answer short."), Options{Gate: gate, MaxSteps: 10, MaxOutputTokens: 2048, ContextWindow: 64000, ModelRef: modelRef, MissingReasoningWarnStateDir: t.TempDir()}, sink)
	gate.a = a
	projected := false
	install := func(target *Agent) {
		client := &fakeDispatchClient{interceptFn: func(ev protocol.InterceptEvent, data json.RawMessage) (protocol.InterceptResult, error) {
			if ev == protocol.EventProviderRequest && budget != nil && !budget.admit() {
				return protocol.InterceptResult{Decision: protocol.DecisionBlock}, nil
			}
			if ev == protocol.EventContextPrepare && !projected && (name == "projected_finish" || name == "projected_reread") {
				for _, ob := range target.turn.readShadow.coord.Snapshot() {
					if ob.Requirement.WholeFile && ob.State == readcoord.StateSatisfied {
						var payload dispatch.ContextPayload
						if err := json.Unmarshal(data, &payload); err != nil {
							return protocol.InterceptResult{}, err
						}
						for i := range payload.Messages {
							if payload.Messages[i].Role == protocol.ProviderRoleTool {
								payload.Messages[i].Content = "Original source text archived. The full read completed on one version; marker ALPHA_MARKER."
							}
						}
						projected = true
						return replaceWith(t, payload), nil
					}
				}
			}
			return protocol.InterceptResult{Decision: protocol.DecisionContinue}, nil
		}}
		target.SetExtensions(newExtDispatcher(client, true, nil, extension.PointContextPrepare, extension.PointProviderRequest))
	}
	install(a)
	ctx, cancel := context.WithTimeout(parent, 150*time.Second)
	defer cancel()
	start := time.Now()
	err := a.Run(ctx, instruction+"\nTarget file: "+path)
	if name == "pause_resume" {
		var pause *IncompleteReadError
		if !errors.As(err, &pause) || pause.Pause == nil {
			t.Fatal("first run did not preserve a read pause")
		}
		sessionPath := filepath.Join(t.TempDir(), "session.jsonl")
		if saveErr := a.Session().SaveWithEphemeralWriter(sessionPath, nil); saveErr != nil {
			t.Fatal(saveErr)
		}
		restored, loadErr := LoadSession(sessionPath)
		if loadErr != nil {
			t.Fatal(loadErr)
		}
		gate.pageBudget = false
		a = New(p, reg, restored, Options{Gate: gate, MaxSteps: 10, MaxOutputTokens: 2048, ContextWindow: 64000, ModelRef: modelRef, MissingReasoningWarnStateDir: t.TempDir()}, sink)
		gate.a = a
		install(a)
		err = a.Run(ctx, "This is an explicit new attempt with a fresh reading budget. Read the entire file with intent=full and report its marker: "+path)
	}
	r := readEvidenceLiveResult{Scenario: name, ElapsedMS: time.Since(start).Milliseconds(), Passed: true}
	if err != nil {
		r.RunError = fmt.Sprintf("%T", err)
	}
	fail := func(reason string) { r.Passed = false; r.Failure += reason + "; " }
	for _, msg := range a.Session().Snapshot() {
		for _, call := range msg.ToolCalls {
			r.Tools = append(r.Tools, call.Name)
		}
		if msg.Role == provider.RoleAssistant && len(msg.ToolCalls) == 0 && msg.Content != "" {
			r.Final = msg.Content
		}
		if msg.Role == provider.RoleTool && strings.HasPrefix(msg.Content, "error:") {
			r.Errors = append(r.Errors, sanitizeReadEvidenceLive(msg.Content, root))
		}
	}
	for _, e := range sink.events {
		if e.Kind == event.Usage && e.Usage != nil {
			u := e.Usage
			r.Prompt += u.PromptTokens
			r.Completion += u.CompletionTokens
			r.CacheHit += u.CacheHitTokens
			r.Requests += max(1, u.RequestCount)
		}
		if e.ReadStatus != nil {
			r.States = append(r.States, e.ReadStatus.State)
		}
		if e.Code == event.NoticeCodeReadContinuationRequired {
			fail("legacy continuation warning")
		}
	}
	if r.Requests == 0 || r.Prompt == 0 || r.Completion == 0 {
		fail("missing live usage")
	}
	if len(r.Tools) == 0 {
		fail("model skipped tool execution")
	}
	var incomplete *IncompleteReadError
	if name == "full_budget" || name == "repeat_budget" {
		if !errors.As(err, &incomplete) {
			fail("page budget did not produce bounded incomplete result")
		}
	} else if name == "stale_version" {
		if !changed {
			fail("source-change path was not exercised")
		}
		if err == nil && !strings.Contains(r.Final, "NEW_VERSION_MARKER") {
			fail("completed without current source marker")
		}
		if err != nil && !errors.As(err, &incomplete) {
			fail(fmt.Sprintf("unexpected run error %T", err))
		}
	} else {
		if err != nil {
			fail(fmt.Sprintf("run error %T", err))
		}
		if expected != "" {
			target := path
			if name == "independent_edit" {
				target = other
			}
			actual, readErr := os.ReadFile(target)
			if readErr != nil || string(actual) != expected {
				fail("file postcondition mismatch")
				r.Details = append(r.Details, fmt.Sprintf("expected=%q actual=%q", sanitizeReadEvidenceLive(expected, root), sanitizeReadEvidenceLive(string(actual), root)))
			}
		} else if !strings.Contains(r.Final, marker) {
			fail("final marker missing")
		}
	}
	if strings.HasPrefix(name, "projected_") && !projected {
		fail("projected context path not exercised")
	}
	if name == "completed_reread" || name == "projected_reread" || name == "partial_reread" {
		if len(r.Tools) < 3 {
			fail("repeat-read path not exercised")
		}
		if obs := a.turn.readShadow.coord.Snapshot(); len(obs) != 1 || obs[0].State != readcoord.StateSatisfied {
			fail("repeat did not preserve one completed obligation")
		}
	}
	if name == "same_content_files" && len(a.turn.readShadow.coord.Snapshot()) != 2 {
		fail("separate file requirements not exercised")
	}
	if name == "full_pages" || name == "full_many_pages" || name == "transport_cut" {
		obs := a.turn.readShadow.coord.Snapshot()
		full := 0
		for _, ob := range obs {
			if ob.Requirement.WholeFile && ob.State == readcoord.StateSatisfied && ob.Pages >= 2 {
				full++
			}
		}
		if full != 1 {
			fail("full coverage did not complete as one logical task")
		}
	}
	if expected == "" && name != "stale_version" && name != "changed_after_complete" {
		actual, _ := os.ReadFile(path)
		if string(actual) != content {
			fail("read-only fixture changed")
		}
	}
	r.Final = sanitizeReadEvidenceLive(r.Final, root)
	if !r.Passed {
		for _, msg := range a.Session().Snapshot() {
			for _, call := range msg.ToolCalls {
				r.Details = append(r.Details, call.Name+": "+sanitizeReadEvidenceLive(string(call.Arguments), root))
			}
		}
	}
	return r
}

func sanitizeReadEvidenceLive(value, root string) string {
	value = strings.ReplaceAll(value, root, "<fixture>")
	if len(value) > 1600 {
		value = value[:1600]
	}
	return value
}
