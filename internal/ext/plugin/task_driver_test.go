package plugin

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"maps"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/tool"
)

// taskServer is a strict 2026-07-28 server that answers its one tool with a
// task and then serves the scripted tasks/get replies in order. The schema the
// shapes follow is ext-tasks specification/2026-07-28.
type taskServer struct {
	t       *testing.T
	mu      sync.Mutex
	polls   []map[string]any // successive tasks/get results; the last repeats
	getErr  int              // tasks/get answers HTTP 500 this many times first
	gone    bool             // tasks/get answers -32602
	created map[string]any   // the tools/call task; nil for the default
	seen    []seenRequest
}

type seenRequest struct {
	method string
	name   string
	params map[string]any
}

func (s *taskServer) requests(method string) []seenRequest {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []seenRequest
	for _, r := range s.seen {
		if r.method == method {
			out = append(out, r)
		}
	}
	return out
}

func (s *taskServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID     any            `json:"id"`
		Method string         `json:"method"`
		Params map[string]any `json:"params"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad body", http.StatusBadRequest)
		return
	}
	s.mu.Lock()
	s.seen = append(s.seen, seenRequest{method: req.Method, name: r.Header.Get("Mcp-Name"), params: req.Params})
	s.mu.Unlock()
	reply := func(result map[string]any) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": result})
	}
	fail := func(code int, msg string) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": req.ID, "error": map[string]any{"code": code, "message": msg}})
	}
	if req.Method == "tasks/get" || req.Method == "tasks/update" || req.Method == "tasks/cancel" {
		if r.Header.Get("Mcp-Name") != "task-1" {
			fail(codeHeaderMismatch, "Mcp-Name must be the taskId")
			return
		}
	}
	switch req.Method {
	case discoverMethod:
		reply(map[string]any{"supportedVersions": []string{modernProtocolVersion}, "capabilities": map[string]any{
			"tools": map[string]any{}, "extensions": map[string]any{tasksExtension: map[string]any{}}}})
	case "tools/list":
		reply(map[string]any{"ttlMs": 0, "tools": []map[string]any{{"name": "job", "description": "A long job.", "inputSchema": map[string]any{"type": "object"}}}})
	case "tools/call":
		meta, _ := req.Params["_meta"].(map[string]any)
		caps, _ := meta[metaClientCapabilities].(map[string]any)
		ext, _ := caps["extensions"].(map[string]any)
		if _, ok := ext[tasksExtension]; !ok {
			fail(codeMissingClientCapability, "tasks required")
			return
		}
		task := map[string]any{"resultType": "task", "taskId": "task-1", "status": "working", "createdAt": "2026-10-15T00:00:00Z", "lastUpdatedAt": "2026-10-15T00:00:00Z", "ttlMs": 60000}
		maps.Copy(task, s.created)
		reply(task)
	case "tasks/get":
		s.mu.Lock()
		defer s.mu.Unlock()
		if s.getErr > 0 {
			s.getErr--
			http.Error(w, "upstream down", http.StatusInternalServerError)
			return
		}
		if s.gone {
			fail(-32602, "Failed to retrieve task: Task has expired")
			return
		}
		next := s.polls[0]
		if len(s.polls) > 1 {
			s.polls = s.polls[1:]
		}
		out := map[string]any{"resultType": "complete", "taskId": "task-1", "createdAt": "2026-10-15T00:00:00Z", "lastUpdatedAt": "2026-10-15T00:00:01Z", "ttlMs": 60000}
		maps.Copy(out, next)
		reply(out)
	case "tasks/update", "tasks/cancel":
		reply(map[string]any{"resultType": "complete"})
	default:
		fail(-32601, "Method not found")
	}
}

type clockLog struct {
	mu    sync.Mutex
	slept []time.Duration
}

func (c *clockLog) sleep(ctx context.Context, d time.Duration) error {
	c.mu.Lock()
	c.slept = append(c.slept, d)
	c.mu.Unlock()
	return ctx.Err()
}

func startTaskTool(t *testing.T, s *taskServer, clock *clockLog) (tool.Tool, string) {
	return startTaskToolWith(t, s, clock, nil)
}

func startTaskToolWith(t *testing.T, s *taskServer, clock *clockLog, timeouts map[string]time.Duration) (tool.Tool, string) {
	t.Helper()
	srv := httptest.NewServer(s)
	t.Cleanup(srv.Close)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	t.Cleanup(cancel)
	root := testenv.TempDir(t)
	host, tools, err := StartAll(ctx, []Spec{{Name: "m", Type: "http", URL: srv.URL, WorkspaceRoot: root, ToolTimeouts: timeouts}})
	if err != nil {
		t.Fatalf("StartAll: %v", err)
	}
	t.Cleanup(func() { host.Close() })
	job := findToolByName(tools, "mcp__m__job")
	if job == nil {
		t.Fatal("tool job not found")
	}
	if clock != nil {
		job.(*remoteTool).client.tasks.sleep = clock.sleep
	}
	return job, root
}

func (s *taskServer) script(polls ...map[string]any) {
	s.mu.Lock()
	s.polls = polls
	s.mu.Unlock()
}

func working(ms any) map[string]any {
	m := map[string]any{"status": "working"}
	if ms != nil {
		m["pollIntervalMs"] = ms
	}
	return m
}

func completedWith(text string, isError bool) map[string]any {
	return map[string]any{"status": "completed", "result": map[string]any{"content": []map[string]any{{"type": "text", "text": text}}, "isError": isError}}
}

func TestTaskResultFollowsWorkingToInputToCompleted(t *testing.T) {
	s := &taskServer{t: t, created: map[string]any{"pollIntervalMs": 10}, polls: []map[string]any{
		working(10),
		{"status": "input_required", "inputRequests": map[string]any{"roots": map[string]any{"method": "roots/list", "params": map[string]any{}}}},
		working(99999999),
		completedWith("done", false),
	}}
	clock := &clockLog{}
	job, root := startTaskTool(t, s, clock)
	out, err := job.Execute(context.Background(), json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(out, "done") {
		t.Fatalf("result = %q, want the task's final result", out)
	}
	if got := len(s.requests("tasks/get")); got != 4 {
		t.Fatalf("tasks/get calls = %d, want 4", got)
	}
	updates := s.requests("tasks/update")
	if len(updates) != 1 {
		t.Fatalf("tasks/update calls = %d, want 1", len(updates))
	}
	responses, _ := updates[0].params["inputResponses"].(map[string]any)
	roots, _ := responses["roots"].(map[string]any)
	list, _ := roots["roots"].([]any)
	if len(list) != 1 || !strings.Contains(list[0].(map[string]any)["uri"].(string), strings.TrimPrefix(strings.ReplaceAll(root, "\\", "/"), "/")) {
		t.Fatalf("roots answered = %v, want only the workspace root %s", roots, root)
	}
	if got, want := clock.slept[0], time.Second; got != want {
		t.Fatalf("a 10 ms hint slept %v, want the %v floor", got, want)
	}
	if got := clock.slept[len(clock.slept)-1]; got != maxTaskPoll {
		t.Fatalf("a huge hint slept %v, want the %v ceiling", got, maxTaskPoll)
	}
}

func TestTaskPollIntervalDefaultsWhenTheServerGivesNone(t *testing.T) {
	s := &taskServer{t: t, polls: []map[string]any{working(nil), completedWith("ok", false)}}
	clock := &clockLog{}
	job, _ := startTaskTool(t, s, clock)
	if _, err := job.Execute(context.Background(), json.RawMessage(`{}`)); err != nil {
		t.Fatal(err)
	}
	if clock.slept[0] != defaultTaskPoll {
		t.Fatalf("slept %v, want the %v default", clock.slept[0], defaultTaskPoll)
	}
}

func TestTaskCompletedWithAToolErrorIsTheToolsResult(t *testing.T) {
	s := &taskServer{t: t, polls: []map[string]any{completedWith("bad input", true)}}
	job, _ := startTaskTool(t, s, &clockLog{})
	out, err := job.Execute(context.Background(), json.RawMessage(`{}`))
	if err == nil || !strings.Contains(err.Error(), "bad input") || out != "bad input" {
		t.Fatalf("out=%q err=%v, want the tool error exactly as a synchronous call reports it", out, err)
	}
	if got := len(s.requests("tasks/cancel")); got != 0 {
		t.Fatalf("a completed task was cancelled %d times", got)
	}
}

func TestTaskEndsAreTypedNotSentences(t *testing.T) {
	cases := []struct {
		name string
		poll map[string]any
		want error
	}{
		{"failed", map[string]any{"status": "failed", "error": map[string]any{"code": -32603, "message": "boom"}}, ErrMCPTaskFailed},
		{"cancelled", map[string]any{"status": "cancelled"}, ErrMCPTaskCancelled},
		{"unknown status", map[string]any{"status": "paused"}, ErrMCPTaskUnknownStatus},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := &taskServer{t: t, polls: []map[string]any{tc.poll}}
			job, _ := startTaskTool(t, s, &clockLog{})
			_, err := job.Execute(context.Background(), json.RawMessage(`{}`))
			if !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
		})
	}
}

func TestTaskTheServerForgotIsGone(t *testing.T) {
	s := &taskServer{t: t, gone: true}
	job, _ := startTaskTool(t, s, &clockLog{})
	if _, err := job.Execute(context.Background(), json.RawMessage(`{}`)); !errors.Is(err, ErrMCPTaskGone) {
		t.Fatalf("err = %v, want ErrMCPTaskGone", err)
	}
}

func TestTaskPollingEndsAfterConsecutiveTransportErrors(t *testing.T) {
	s := &taskServer{t: t, getErr: 100, polls: []map[string]any{working(nil)}}
	job, _ := startTaskTool(t, s, &clockLog{})
	_, err := job.Execute(context.Background(), json.RawMessage(`{}`))
	if err == nil || errors.Is(err, ErrMCPTaskGone) {
		t.Fatalf("err = %v, want the transport failure", err)
	}
	if got := len(s.requests("tasks/get")); got != maxTaskPollErrors {
		t.Fatalf("tasks/get calls = %d, want %d and no more", got, maxTaskPollErrors)
	}
}

func TestTaskTheCallerGivingUpCancelsItOnTheServer(t *testing.T) {
	s := &taskServer{t: t, polls: []map[string]any{working(nil)}}
	job, _ := startTaskTool(t, s, nil)
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(300 * time.Millisecond)
		cancel()
	}()
	if _, err := job.Execute(ctx, json.RawMessage(`{}`)); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	cancels := s.requests("tasks/cancel")
	if len(cancels) != 1 || cancels[0].params["taskId"] != "task-1" || cancels[0].name != "task-1" {
		t.Fatalf("tasks/cancel = %+v, want one for task-1 with Mcp-Name", cancels)
	}
}

func TestTaskInputThisClientCannotAnswerEndsTheTask(t *testing.T) {
	s := &taskServer{t: t, polls: []map[string]any{{"status": "input_required", "inputRequests": map[string]any{
		"ask": map[string]any{"method": "sampling/createMessage", "params": map[string]any{}}}}}}
	job, _ := startTaskTool(t, s, &clockLog{})
	if _, err := job.Execute(context.Background(), json.RawMessage(`{}`)); !errors.Is(err, ErrMCPInputRequired) {
		t.Fatalf("err = %v, want ErrMCPInputRequired", err)
	}
	if len(s.requests("tasks/update")) != 0 {
		t.Fatal("a request this client cannot answer was answered anyway")
	}
	if len(s.requests("tasks/cancel")) != 1 {
		t.Fatalf("tasks/cancel = %d, want the task cancelled once", len(s.requests("tasks/cancel")))
	}
}

func TestTaskSlotsCapConcurrentTasksAndYieldToTheContext(t *testing.T) {
	var st taskState
	var releases []func()
	for range maxConcurrentTasks {
		release, err := st.acquire(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		releases = append(releases, release)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if _, err := st.acquire(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("a ninth task got a slot: err = %v", err)
	}
	releases[0]()
	if release, err := st.acquire(context.Background()); err != nil {
		t.Fatalf("a freed slot was not reusable: %v", err)
	} else {
		release()
	}
}

func TestTaskClampsPollingHintsInMilliseconds(t *testing.T) {
	ms := func(v int64) *int64 { return &v }
	for _, tc := range []struct {
		in   *int64
		want time.Duration
	}{
		{nil, defaultTaskPoll}, {ms(0), defaultTaskPoll}, {ms(-5), defaultTaskPoll},
		{ms(1), minTaskPoll}, {ms(5000), 5 * time.Second}, {ms(1 << 62), maxTaskPoll},
	} {
		if got := clampTaskPoll(tc.in); got != tc.want {
			t.Errorf("clampTaskPoll(%v) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

func TestTaskThatKeepsAskingForInputStopsAtTheRoundLimit(t *testing.T) {
	ask := map[string]any{"status": "input_required", "inputRequests": map[string]any{"roots": map[string]any{"method": "roots/list", "params": map[string]any{}}}}
	s := &taskServer{t: t, polls: []map[string]any{ask}}
	job, _ := startTaskTool(t, s, &clockLog{})
	if _, err := job.Execute(context.Background(), json.RawMessage(`{}`)); !errors.Is(err, ErrMCPInputRequired) {
		t.Fatalf("err = %v, want ErrMCPInputRequired at the limit", err)
	}
	if got := len(s.requests("tasks/update")); got != maxInputRounds {
		t.Fatalf("tasks/update calls = %d, want exactly %d answered rounds", got, maxInputRounds)
	}
	if got := len(s.requests("tasks/cancel")); got != 1 {
		t.Fatalf("tasks/cancel calls = %d, want the task cancelled once", got)
	}
}

// background is a BackgroundStarter that records the run instead of starting it.
type background struct {
	mu   sync.Mutex
	kind string
	name string
	run  func(context.Context, io.Writer) (string, error)
}

func (b *background) start(kind, label string, run func(context.Context, io.Writer) (string, error)) string {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.kind, b.name, b.run = kind, label, run
	return "mcp_task-1"
}

func TestTaskThatOutrunsItsBudgetBecomesABackgroundJob(t *testing.T) {
	s := &taskServer{t: t, polls: []map[string]any{working(nil)}}
	job, _ := startTaskToolWith(t, s, &clockLog{}, map[string]time.Duration{"job": 150 * time.Millisecond})
	bg := &background{}
	ctx := tool.WithBackgroundStarter(context.Background(), bg.start)
	out, err := job.Execute(ctx, json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(out, "mcp_task-1") || strings.Contains(out, "task-1\"") || strings.Contains(out, "taskId") {
		t.Fatalf("result = %q, want the host job id and no task handle", out)
	}
	if bg.kind != "mcp_task" || bg.name != "m.job" {
		t.Fatalf("job = %q %q, want mcp_task m.job", bg.kind, bg.name)
	}
	if got := len(s.requests("tasks/cancel")); got != 0 {
		t.Fatalf("promoting cancelled the task %d times", got)
	}

	s.script(completedWith("late result", false))
	text, err := bg.run(context.Background(), io.Discard)
	if err != nil || !strings.Contains(text, "late result") || !strings.Contains(text, "[external content") {
		t.Fatalf("job result = %q, %v, want the result under the external-content label", text, err)
	}
}

func TestKillingThePromotedJobCancelsTheTaskOnTheServer(t *testing.T) {
	s := &taskServer{t: t, polls: []map[string]any{working(nil)}}
	job, _ := startTaskToolWith(t, s, &clockLog{}, map[string]time.Duration{"job": 150 * time.Millisecond})
	bg := &background{}
	if _, err := job.Execute(tool.WithBackgroundStarter(context.Background(), bg.start), json.RawMessage(`{}`)); err != nil {
		t.Fatal(err)
	}
	ctx, kill := context.WithCancel(context.Background())
	go func() {
		time.Sleep(100 * time.Millisecond)
		kill()
	}()
	if _, err := bg.run(ctx, io.Discard); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if got := len(s.requests("tasks/cancel")); got != 1 {
		t.Fatalf("tasks/cancel = %d, want the task cancelled once", got)
	}
}

func TestTaskThatOutrunsItsBudgetWithoutAJobManagerIsCancelled(t *testing.T) {
	s := &taskServer{t: t, polls: []map[string]any{working(nil)}}
	job, _ := startTaskToolWith(t, s, &clockLog{}, map[string]time.Duration{"job": 150 * time.Millisecond})
	if _, err := job.Execute(context.Background(), json.RawMessage(`{}`)); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want the timeout", err)
	}
	if got := len(s.requests("tasks/cancel")); got != 1 {
		t.Fatalf("tasks/cancel = %d, want the task cancelled once", got)
	}
}

func TestPromotedJobEndsInErrorWhenTheTaskResultIsAToolError(t *testing.T) {
	s := &taskServer{t: t, polls: []map[string]any{working(nil)}}
	job, _ := startTaskToolWith(t, s, &clockLog{}, map[string]time.Duration{"job": 150 * time.Millisecond})
	bg := &background{}
	if _, err := job.Execute(tool.WithBackgroundStarter(context.Background(), bg.start), json.RawMessage(`{}`)); err != nil {
		t.Fatal(err)
	}
	s.script(completedWith("bad input", true))
	text, err := bg.run(context.Background(), io.Discard)
	if err == nil || !strings.Contains(text, "bad input") {
		t.Fatalf("job result = %q, %v, want an error carrying the tool's text", text, err)
	}
}

func TestBackgroundTaskSlotsAreBoundedAndReleased(t *testing.T) {
	var st taskState
	var releases []func()
	for range maxConcurrentTasks {
		release, ok := st.reserveBackground()
		if !ok {
			t.Fatal("a slot below the cap was refused")
		}
		releases = append(releases, release)
	}
	if _, ok := st.reserveBackground(); ok {
		t.Fatal("a job beyond the cap got a slot")
	}
	releases[0]()
	if release, ok := st.reserveBackground(); !ok {
		t.Fatal("a released slot was not reusable")
	} else {
		release()
	}
	inline, err := st.acquire(context.Background())
	if err != nil {
		t.Fatalf("background jobs took the inline slots: %v", err)
	}
	inline()
}

func TestCallerGivingUpWhileWaitingForATaskSlotStillCancelsTheTask(t *testing.T) {
	s := &taskServer{t: t, polls: []map[string]any{working(nil)}}
	job, _ := startTaskTool(t, s, &clockLog{})
	client := job.(*remoteTool).client
	for range maxConcurrentTasks {
		if _, err := client.tasks.acquire(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(150 * time.Millisecond)
		cancel()
	}()
	if _, err := job.Execute(ctx, json.RawMessage(`{}`)); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if got := len(s.requests("tasks/cancel")); got != 1 {
		t.Fatalf("tasks/cancel = %d, want the created task cancelled exactly once", got)
	}
}
