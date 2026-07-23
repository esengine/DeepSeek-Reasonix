package main

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"reasonix/internal/agent"
	"reasonix/internal/control"
	"reasonix/internal/event"
	"reasonix/internal/jobs"
)

func TestProjectTreeShowsDetachedRuntimeStatus(t *testing.T) {
	isolateDesktopUserDirs(t)
	dir := desktopSessionDir(globalTabWorkspaceRoot())
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir sessions: %v", err)
	}
	topicID := "topic_detached_status"
	topicTitle := "Detached status"
	if err := setTopicTitle("", topicID, topicTitle); err != nil {
		t.Fatalf("set topic title: %v", err)
	}
	path := writeTopicSessionWithPrompt(t, dir, "detached.jsonl", topicID, topicTitle, "", "detached prompt", time.Now())

	app := NewApp()
	sink := &tabEventSink{tabID: "detached", app: app}
	runner := &blockingRunner{started: make(chan struct{}), release: make(chan struct{})}
	ctrl := control.New(control.Options{Runner: runner, SessionDir: dir, SessionPath: path, Label: "detached", Sink: sink})
	defer ctrl.Close()
	app.detachedSessions[sessionRuntimeKey(path)] = &WorkspaceTab{
		ID:            "detached",
		Scope:         "global",
		WorkspaceRoot: globalTabWorkspaceRoot(),
		TopicID:       topicID,
		TopicTitle:    topicTitle,
		SessionPath:   path,
		Ctrl:          ctrl,
		Ready:         true,
		sink:          sink,
		disabledMCP:   map[string]ServerView{},
	}

	ctrl.Submit("keep detached runtime running")
	<-runner.started
	nodes := app.ListProjectTree()
	if len(nodes) != 1 || len(nodes[0].Children) != 1 {
		t.Fatalf("project tree = %#v, want one global topic", nodes)
	}
	topic := nodes[0].Children[0]
	if topic.TopicID != topicID || topic.Status != topicStatusThinking || !topic.Running {
		t.Fatalf("detached topic status = %+v, want thinking/running for %q", topic, topicID)
	}

	close(runner.release)
	waitNotRunning(t, ctrl)
}

func TestProjectTreeSplitsMultipleRuntimeSessionsInSameTopic(t *testing.T) {
	isolateDesktopUserDirs(t)
	dir := desktopSessionDir(globalTabWorkspaceRoot())
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir sessions: %v", err)
	}
	topicID := "topic_multi_runtime_status"
	topicTitle := "Multi runtime status"
	if err := setTopicTitle("", topicID, topicTitle); err != nil {
		t.Fatalf("set topic title: %v", err)
	}
	sessionA := writeTopicSessionWithPrompt(t, dir, "session-a.jsonl", topicID, topicTitle, "", "session A prompt", time.Now().Add(-time.Hour))
	sessionB := writeTopicSessionWithPrompt(t, dir, "session-b.jsonl", topicID, topicTitle, "", "session B prompt", time.Now())

	app := NewApp()
	runnerA := &blockingRunner{started: make(chan struct{}), release: make(chan struct{})}
	runnerB := &blockingRunner{started: make(chan struct{}), release: make(chan struct{})}
	ctrlA := control.New(control.Options{Runner: runnerA, SessionDir: dir, SessionPath: sessionA, Label: "a", Sink: event.Discard})
	ctrlB := control.New(control.Options{Runner: runnerB, SessionDir: dir, SessionPath: sessionB, Label: "b", Sink: event.Discard})
	defer ctrlA.Close()
	defer ctrlB.Close()

	detached := &WorkspaceTab{
		ID:             detachedRuntimeTabID(sessionRuntimeKey(sessionA)),
		Scope:          "global",
		WorkspaceRoot:  globalTabWorkspaceRoot(),
		TopicID:        topicID,
		TopicTitle:     topicTitle,
		SessionPath:    sessionA,
		Ctrl:           ctrlA,
		Ready:          true,
		ActivityStatus: topicStatusWaitingConfirmation,
		disabledMCP:    map[string]ServerView{},
	}
	visible := &WorkspaceTab{
		ID:             "visible",
		Scope:          "global",
		WorkspaceRoot:  globalTabWorkspaceRoot(),
		TopicID:        topicID,
		TopicTitle:     topicTitle,
		SessionPath:    sessionB,
		Ctrl:           ctrlB,
		Ready:          true,
		ActivityStatus: topicStatusThinking,
		disabledMCP:    map[string]ServerView{},
	}
	app.detachedSessions[sessionRuntimeKey(sessionA)] = detached
	app.tabs[visible.ID] = visible
	app.tabOrder = []string{visible.ID}
	app.activeTabID = visible.ID

	ctrlA.Submit("block A")
	ctrlB.Submit("block B")
	<-runnerA.started
	<-runnerB.started

	nodes := app.ListProjectTree()
	if len(nodes) != 1 || len(nodes[0].Children) != 1 {
		t.Fatalf("project tree = %#v, want one global topic", nodes)
	}
	topic := nodes[0].Children[0]
	if topic.Status != "" || topic.Running {
		t.Fatalf("topic should not merge child runtime statuses: %+v", topic)
	}
	if len(topic.Children) != 2 {
		t.Fatalf("topic children = %#v, want two session runtime rows", topic.Children)
	}
	statusByPath := map[string]string{}
	for _, child := range topic.Children {
		statusByPath[sessionRuntimeKey(child.SessionPath)] = child.Status
	}
	if statusByPath[sessionRuntimeKey(sessionA)] != topicStatusWaitingConfirmation {
		t.Fatalf("session A status = %q, want waiting; children=%#v", statusByPath[sessionRuntimeKey(sessionA)], topic.Children)
	}
	if statusByPath[sessionRuntimeKey(sessionB)] != topicStatusThinking {
		t.Fatalf("session B status = %q, want thinking; children=%#v", statusByPath[sessionRuntimeKey(sessionB)], topic.Children)
	}

	close(runnerA.release)
	close(runnerB.release)
	waitNotRunning(t, ctrlA)
	waitNotRunning(t, ctrlB)
}

func TestProjectTreeShowsBackgroundJobStatus(t *testing.T) {
	isolateDesktopUserDirs(t)
	dir := desktopSessionDir(globalTabWorkspaceRoot())
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir sessions: %v", err)
	}
	topicID := "topic_background_job"
	topicTitle := "Background job"
	if err := setTopicTitle("", topicID, topicTitle); err != nil {
		t.Fatalf("set topic title: %v", err)
	}
	path := writeTopicSessionWithPrompt(t, dir, "job.jsonl", topicID, topicTitle, "", "job prompt", time.Now())

	jm := jobs.NewManager(event.Discard)
	release := make(chan struct{})
	jm.StartForSession(agent.BranchID(path), "bash", "sleep", func(ctx context.Context, _ io.Writer) (string, error) {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-release:
			return "", nil
		}
	})
	ctrl := control.New(control.Options{SessionDir: dir, SessionPath: path, Label: "job", Jobs: jm})
	defer ctrl.Close()
	app := NewApp()
	app.tabs["job"] = &WorkspaceTab{
		ID:            "job",
		Scope:         "global",
		WorkspaceRoot: globalTabWorkspaceRoot(),
		TopicID:       topicID,
		TopicTitle:    topicTitle,
		SessionPath:   path,
		Ctrl:          ctrl,
		Ready:         true,
		disabledMCP:   map[string]ServerView{},
	}
	app.tabOrder = []string{"job"}
	app.activeTabID = "job"

	nodes := app.ListProjectTree()
	if len(nodes) != 1 || len(nodes[0].Children) != 1 {
		t.Fatalf("project tree = %#v, want one global topic", nodes)
	}
	topic := nodes[0].Children[0]
	if topic.Status != topicStatusBackgroundJob || !topic.Running {
		t.Fatalf("background job topic status = %+v, want background_job/running", topic)
	}
	tabs := app.ListTabs()
	if len(tabs) != 1 {
		t.Fatalf("tabs = %d, want 1", len(tabs))
	}
	if !tabs[0].Running || tabs[0].PendingPrompt || tabs[0].Cancellable || tabs[0].BackgroundJobs != 1 {
		t.Fatalf("tab runtime = running:%v pending:%v cancellable:%v background:%d, want background-only running tab", tabs[0].Running, tabs[0].PendingPrompt, tabs[0].Cancellable, tabs[0].BackgroundJobs)
	}
	raw, err := json.Marshal(tabs[0])
	if err != nil {
		t.Fatalf("marshal tab meta: %v", err)
	}
	if !strings.Contains(string(raw), `"cancellable":false`) {
		t.Fatalf("tab metadata should serialize explicit cancellable=false: %s", raw)
	}

	close(release)
	waitNoJobs(t, ctrl)
	nodes = app.ListProjectTree()
	if len(nodes) != 1 || len(nodes[0].Children) != 1 {
		t.Fatalf("project tree after job finish = %#v, want one global topic", nodes)
	}
	topic = nodes[0].Children[0]
	if topic.Status != "" || topic.Running {
		t.Fatalf("background job topic status after finish = %+v, want idle", topic)
	}
}

func TestBackgroundJobNoticeForcesProjectTreeRefresh(t *testing.T) {
	isolateDesktopUserDirs(t)

	var refreshes int32
	app := NewApp()
	app.projectTreeChangedHook = func() {
		atomic.AddInt32(&refreshes, 1)
	}
	app.tabs["job"] = &WorkspaceTab{
		ID:          "job",
		Scope:       "global",
		TopicID:     "topic_background_notice",
		Ready:       true,
		disabledMCP: map[string]ServerView{},
	}
	sink := &tabEventSink{tabID: "job", app: app}

	sink.Emit(event.Event{Kind: event.Notice, Level: event.LevelInfo, Text: "background bash finished: bash-1"})
	if got := atomic.LoadInt32(&refreshes); got != 1 {
		t.Fatalf("project-tree refreshes = %d, want 1 for background job finish notice", got)
	}
}

func waitNoJobs(t *testing.T, ctrl control.SessionAPI) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for len(ctrl.Jobs()) > 0 {
		if time.Now().After(deadline) {
			t.Fatalf("controller still has jobs: %+v", ctrl.Jobs())
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestTopicActivityStatusPresentsReadinessAsPaused(t *testing.T) {
	readiness := event.Event{
		Kind:    event.TurnDone,
		Err:     &agent.FinalReadinessError{Attempts: 3, Reason: "missing verification"},
		Outcome: event.TurnOutcomeFinalReadiness,
	}
	if status, ok := topicActivityStatusFromEvent(readiness); !ok || status != topicStatusPaused {
		t.Fatalf("readiness turn end = (%q, %v), want (%q, true)", status, ok, topicStatusPaused)
	}
	if status, ok := topicActivityStatusFromEvent(event.Event{Kind: event.TurnDone, Err: io.EOF}); !ok || status != topicStatusError {
		t.Fatalf("ordinary turn error = (%q, %v), want (%q, true)", status, ok, topicStatusError)
	}
	if status, ok := topicActivityStatusFromEvent(event.Event{Kind: event.TurnDone}); !ok || status != "" {
		t.Fatalf("clean turn end = (%q, %v), want cleared status", status, ok)
	}
}
