package cli

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"reasonix/internal/agent"
	"reasonix/internal/control"
	"reasonix/internal/event"
	"reasonix/internal/eventwire"
	"reasonix/internal/jobs"
	"reasonix/internal/provider"
)

type takeoverRecordSink struct {
	mu     sync.Mutex
	events []event.Event
}

func (s *takeoverRecordSink) Emit(e event.Event) {
	s.mu.Lock()
	s.events = append(s.events, e)
	s.mu.Unlock()
}

func TestCLITakeoverManagerRetriesSameFrameBatchInOrder(t *testing.T) {
	var mu sync.Mutex
	var requests [][]eventwire.Event
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Frames []eventwire.Event `json:"frames"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		mu.Lock()
		requests = append(requests, append([]eventwire.Event(nil), body.Frames...))
		mu.Unlock()
		if calls.Add(1) == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]bool{"reclaimRequested": false})
	}))
	defer srv.Close()

	m := newCLITakeoverManager(&takeoverRecordSink{}, nil)
	m.binding = &cliTakeoverBinding{
		path: "session.jsonl", record: cliServeRecord{base: srv.URL}, client: srv.Client(),
		grant: cliTakeoverGrant{MirrorID: "mirror-1"},
	}
	m.Emit(event.Event{Kind: event.Text, Text: "first"})
	m.Emit(event.Event{Kind: event.Text, Text: "second"})
	if !m.push(false) {
		t.Fatal("frame push stopped unexpectedly")
	}
	if !m.push(false) {
		t.Fatal("frame push stopped unexpectedly")
	}

	mu.Lock()
	defer mu.Unlock()
	if len(requests) != 2 || len(requests[0]) != 2 || len(requests[1]) != 2 {
		t.Fatalf("request batches = %+v, want two complete two-frame batches", requests)
	}
	for i := range requests[0] {
		if requests[0][i].Text != requests[1][i].Text {
			t.Fatalf("retry reordered frame %d: %q != %q", i, requests[0][i].Text, requests[1][i].Text)
		}
	}
}

func TestCLITakeoverManagerChunksWithoutDroppingFrames(t *testing.T) {
	var got []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Frames []eventwire.Event `json:"frames"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if len(body.Frames) > cliTakeoverMaxFrames {
			t.Fatalf("batch size = %d", len(body.Frames))
		}
		for _, frame := range body.Frames {
			got = append(got, frame.Text)
		}
		_ = json.NewEncoder(w).Encode(map[string]bool{"reclaimRequested": false})
	}))
	defer srv.Close()
	m := newCLITakeoverManager(&takeoverRecordSink{}, nil)
	m.binding = &cliTakeoverBinding{
		path: "session.jsonl", record: cliServeRecord{base: srv.URL}, client: srv.Client(),
		grant: cliTakeoverGrant{MirrorID: "mirror-1"},
	}
	for i := range cliTakeoverMaxFrames + 37 {
		m.Emit(event.Event{Kind: event.Text, Text: strconv.Itoa(i)})
	}
	if !m.push(false) {
		t.Fatal("chunked frame push stopped unexpectedly")
	}
	if !m.push(false) {
		t.Fatal("chunked frame push stopped unexpectedly")
	}
	if len(got) != cliTakeoverMaxFrames+37 {
		t.Fatalf("received %d frames", len(got))
	}
	for i, text := range got {
		if text != strconv.Itoa(i) {
			t.Fatalf("frame %d = %q", i, text)
		}
	}
}

func TestCLITakeoverManagerHeartbeatSendsEmptyFrameBatch(t *testing.T) {
	got := make(chan int, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Frames []eventwire.Event `json:"frames"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		got <- len(body.Frames)
		_ = json.NewEncoder(w).Encode(map[string]bool{"reclaimRequested": false})
	}))
	defer srv.Close()
	m := newCLITakeoverManager(&takeoverRecordSink{}, nil)
	m.binding = &cliTakeoverBinding{
		path: "session.jsonl", record: cliServeRecord{base: srv.URL}, client: srv.Client(),
		grant: cliTakeoverGrant{MirrorID: "mirror-1"},
	}
	if !m.push(true) {
		t.Fatal("heartbeat stopped the manager")
	}
	if frames := <-got; frames != 0 {
		t.Fatalf("heartbeat frames = %d, want 0", frames)
	}
}

func TestCLITakeoverManagerReclaimReturnsLeaseAndSignalsExit(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "active.jsonl")
	session := agent.NewSession("system")
	session.Add(provider.Message{Role: provider.RoleUser, Content: "hello"})
	if err := session.Save(path); err != nil {
		t.Fatal(err)
	}
	loaded, err := agent.LoadSession(path)
	if err != nil {
		t.Fatal(err)
	}
	exec := agent.New(nil, nil, loaded, agent.Options{}, &takeoverRecordSink{})
	ctrl := control.New(control.Options{Executor: exec, SessionDir: dir, SessionPath: path})
	defer ctrl.Close()
	leases := control.NewSessionLeaseKeeper()
	defer leases.Release()
	if err := leases.Rebind(path); err != nil {
		t.Fatal(err)
	}
	if err := leases.BindControllerAuthority(ctrl); err != nil {
		t.Fatal(err)
	}

	var mirrorEnded atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/external/frames":
			_ = json.NewEncoder(w).Encode(map[string]any{"reclaimRequested": true, "reclaimMode": "wait"})
		case "/mirror-end":
			mirrorEnded.Store(true)
			w.WriteHeader(http.StatusNoContent)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	exited := make(chan struct{}, 1)
	m := newCLITakeoverManager(&takeoverRecordSink{}, leases)
	m.AttachController(ctrl)
	m.SetYieldCallback(func() { exited <- struct{}{} })
	m.Activate(&cliTakeoverBinding{
		path: path, record: cliServeRecord{base: srv.URL}, client: srv.Client(),
		grant: cliTakeoverGrant{
			MirrorID: "mirror-1", SourceWriterID: "serve-writer", ReturnHandoffID: "return-1",
		},
	})
	m.Emit(event.Event{Kind: event.Text, Text: "answer"})
	select {
	case <-exited:
	case <-time.After(5 * time.Second):
		t.Fatal("reclaim did not return the lease")
	}
	if !m.Returned() || !mirrorEnded.Load() {
		t.Fatalf("returned=%v mirrorEnded=%v", m.Returned(), mirrorEnded.Load())
	}
	info, err := agent.LoadSessionLeaseInfo(path)
	if err != nil {
		t.Fatal(err)
	}
	if info == nil || info.HandoffTo != "serve-writer" || info.HandoffID != "return-1" {
		t.Fatalf("reverse reservation = %+v", info)
	}
	if err := m.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestCLITakeoverManagerReclaimWaitsForBackgroundJobs(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "background.jsonl")
	session := agent.NewSession("system")
	if err := session.Save(path); err != nil {
		t.Fatal(err)
	}
	manager := jobs.NewManager(event.Discard)
	ctrl := control.New(control.Options{
		Executor:    agent.New(nil, nil, session, agent.Options{}, event.Discard),
		Jobs:        manager,
		SessionDir:  dir,
		SessionPath: path,
	})
	defer ctrl.Close()
	jobStarted := make(chan struct{})
	releaseJob := make(chan struct{})
	manager.StartForSession(agent.BranchID(path), "task", "background", func(ctx context.Context, _ io.Writer) (string, error) {
		close(jobStarted)
		select {
		case <-releaseJob:
			return "done", nil
		case <-ctx.Done():
			return "", ctx.Err()
		}
	})
	<-jobStarted

	leases := control.NewSessionLeaseKeeper()
	defer leases.Release()
	if err := leases.Rebind(path); err != nil {
		t.Fatal(err)
	}
	if err := leases.BindControllerAuthority(ctrl); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/mirror-end":
			w.WriteHeader(http.StatusNoContent)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	exited := make(chan struct{}, 1)
	m := newCLITakeoverManager(event.Discard, leases)
	m.AttachController(ctrl)
	m.SetYieldCallback(func() { exited <- struct{}{} })
	binding := &cliTakeoverBinding{
		path: path, record: cliServeRecord{base: srv.URL}, client: srv.Client(),
		grant: cliTakeoverGrant{MirrorID: "mirror", SourceWriterID: "serve-writer", ReturnHandoffID: "return"},
	}
	m.mu.Lock()
	m.binding = binding
	m.revision = 1
	m.mu.Unlock()
	m.requestYieldFor(binding, 1, false)
	select {
	case <-exited:
		t.Fatal("reclaim returned the lease while a background job was active")
	case <-time.After(150 * time.Millisecond):
	}
	contender, err := agent.TryAcquireSessionLease(path)
	if contender != nil {
		contender.Release()
	}
	if !errors.Is(err, agent.ErrSessionLeaseHeld) {
		t.Fatalf("background reclaim lease error = %v, want held", err)
	}

	close(releaseJob)
	select {
	case <-exited:
	case <-time.After(5 * time.Second):
		t.Fatal("reclaim did not finish after the background job completed")
	}
	if err := m.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestCLITakeoverManagerTransitionsBetweenMirrorsAtomically(t *testing.T) {
	dir := t.TempDir()
	current := filepath.Join(dir, "current.jsonl")
	nextPath := filepath.Join(dir, "next.jsonl")
	leases := control.NewSessionLeaseKeeper()
	defer leases.Release()
	if err := leases.Rebind(current); err != nil {
		t.Fatal(err)
	}
	nextSource, err := agent.TryAcquireSessionLease(nextPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := nextSource.ReleaseForHandoff(agent.SessionWriterID(), "forward-next"); err != nil {
		t.Fatal(err)
	}

	var oldEnded atomic.Bool
	oldServe := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/mirror-end" {
			oldEnded.Store(true)
			w.WriteHeader(http.StatusNoContent)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]bool{"reclaimRequested": false})
	}))
	defer oldServe.Close()
	m := newCLITakeoverManager(&takeoverRecordSink{}, leases)
	m.binding = &cliTakeoverBinding{
		path: current, record: cliServeRecord{base: oldServe.URL}, client: oldServe.Client(),
		grant: cliTakeoverGrant{MirrorID: "old-mirror", SourceWriterID: "old-serve", ReturnHandoffID: "return-old"},
	}
	next := &cliTakeoverBinding{
		path:  nextPath,
		grant: cliTakeoverGrant{MirrorID: "next-mirror", SourceWriterID: agent.SessionWriterID(), HandoffID: "forward-next"},
	}
	next.previous, err = leases.RebindDetachingWithHandoff(nextPath, agent.SessionWriterID(), "forward-next")
	if err != nil {
		t.Fatal(err)
	}
	next.priorMirror = m.binding
	if err := next.commitPrevious(m); err != nil {
		t.Fatalf("commitPrevious: %v", err)
	}
	if got := leases.HeldPath(); got != agent.CanonicalSessionPath(nextPath) {
		t.Fatalf("held path = %q, want next", got)
	}
	if !oldEnded.Load() {
		t.Fatal("old mirror was not ended after the atomic keeper transition")
	}
	info, err := agent.LoadSessionLeaseInfo(current)
	if err != nil {
		t.Fatal(err)
	}
	if info == nil || info.HandoffTo != "old-serve" || info.HandoffID != "return-old" {
		t.Fatalf("old reverse reservation = %+v", info)
	}
}

func TestCLIFailedTakeoverRestoresSourceAndReturnsTarget(t *testing.T) {
	dir := t.TempDir()
	current := filepath.Join(dir, "current.jsonl")
	target := filepath.Join(dir, "target.jsonl")
	leases := control.NewSessionLeaseKeeper()
	defer leases.Release()
	if err := leases.Rebind(current); err != nil {
		t.Fatal(err)
	}
	targetSource, err := agent.TryAcquireSessionLease(target)
	if err != nil {
		t.Fatal(err)
	}
	if err := targetSource.ReleaseForHandoff(agent.SessionWriterID(), "forward-target"); err != nil {
		t.Fatal(err)
	}
	previous, err := leases.RebindDetachingWithHandoff(target, agent.SessionWriterID(), "forward-target")
	if err != nil {
		t.Fatal(err)
	}
	var mirrorEnded atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/mirror-end" {
			mirrorEnded.Store(true)
			w.WriteHeader(http.StatusNoContent)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()
	binding := &cliTakeoverBinding{
		path: target, previous: previous, record: cliServeRecord{base: srv.URL}, client: srv.Client(),
		grant: cliTakeoverGrant{
			MirrorID: "target-mirror", SourceWriterID: "target-serve", ReturnHandoffID: "return-target",
		},
	}
	manager := newCLITakeoverManager(&takeoverRecordSink{}, leases)
	if err := cliReturnFailedTakeover(binding, leases, manager); err != nil {
		t.Fatal(err)
	}
	if got := leases.HeldPath(); got != agent.CanonicalSessionPath(current) {
		t.Fatalf("held path = %q, want restored source", got)
	}
	info, err := agent.LoadSessionLeaseInfo(target)
	if err != nil {
		t.Fatal(err)
	}
	if info == nil || info.HandoffTo != "target-serve" || info.HandoffID != "return-target" {
		t.Fatalf("target reverse reservation = %+v", info)
	}
	if !mirrorEnded.Load() {
		t.Fatal("failed target mirror was not ended")
	}
}

func TestCLITakeoverManagerSerializesRequestsAndDiscardsStaleReclaim(t *testing.T) {
	firstEntered := make(chan struct{})
	releaseFirst := make(chan struct{})
	var inFlight atomic.Int32
	var maxInFlight atomic.Int32
	var mu sync.Mutex
	var batches [][]eventwire.Event
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		current := inFlight.Add(1)
		defer inFlight.Add(-1)
		for {
			observed := maxInFlight.Load()
			if current <= observed || maxInFlight.CompareAndSwap(observed, current) {
				break
			}
		}
		var body struct {
			Frames []eventwire.Event `json:"frames"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
			return
		}
		mu.Lock()
		batches = append(batches, append([]eventwire.Event(nil), body.Frames...))
		call := len(batches)
		mu.Unlock()
		if call == 1 {
			close(firstEntered)
			<-releaseFirst
			_ = json.NewEncoder(w).Encode(map[string]bool{"reclaimRequested": true})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]bool{"reclaimRequested": false})
	}))
	defer srv.Close()

	m := newCLITakeoverManager(&takeoverRecordSink{}, nil)
	firstBinding := &cliTakeoverBinding{
		path: "a.jsonl", record: cliServeRecord{base: srv.URL}, client: srv.Client(),
		grant: cliTakeoverGrant{MirrorID: "mirror-a"},
	}
	m.binding = firstBinding
	m.revision = 1
	m.Emit(event.Event{Kind: event.Text, Text: "A"})
	firstDone := make(chan struct{})
	go func() {
		m.push(false)
		close(firstDone)
	}()
	<-firstEntered

	// Model a completed generation switch while the old HTTP response is in
	// flight. The response must be ignored before it can request a reclaim.
	m.mu.Lock()
	m.binding = &cliTakeoverBinding{
		path: "b.jsonl", record: cliServeRecord{base: srv.URL}, client: srv.Client(),
		grant: cliTakeoverGrant{MirrorID: "mirror-b"},
	}
	m.revision++
	m.mu.Unlock()
	m.Emit(event.Event{Kind: event.Text, Text: "B"})
	secondDone := make(chan struct{})
	go func() {
		m.push(false)
		close(secondDone)
	}()
	close(releaseFirst)
	<-firstDone
	<-secondDone

	if got := maxInFlight.Load(); got != 1 {
		t.Fatalf("max requests in flight = %d, want 1", got)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(batches) != 2 || len(batches[0]) != 1 || batches[0][0].Text != "A" || len(batches[1]) != 1 || batches[1][0].Text != "B" {
		t.Fatalf("batches = %+v, want A then B", batches)
	}
	if m.Reclaiming() {
		t.Fatal("stale reclaim response affected the new binding")
	}
}

func TestCLITakeoverManagerReadoptsOnAuthFailureAndServerMove(t *testing.T) {
	var delivered atomic.Int32
	newServe := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/auth/token":
			w.WriteHeader(http.StatusNoContent)
		case "/adopt":
			_ = json.NewEncoder(w).Encode(cliTakeoverGrant{
				SessionPath: "session.jsonl", MirrorID: "mirror-new", ReturnHandoffID: "return-new",
				SourceWriterID: "serve-new", TargetWriterID: agent.SessionWriterID(),
			})
		case "/external/frames":
			delivered.Add(1)
			_ = json.NewEncoder(w).Encode(map[string]bool{"reclaimRequested": false})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer newServe.Close()
	oldServe := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer oldServe.Close()
	originalDiscover := discoverCLIServesForTakeover
	discoverCLIServesForTakeover = func() []cliServeRecord {
		return []cliServeRecord{{base: newServe.URL, token: "fresh"}}
	}
	t.Cleanup(func() { discoverCLIServesForTakeover = originalDiscover })

	m := newCLITakeoverManager(&takeoverRecordSink{}, nil)
	m.binding = &cliTakeoverBinding{
		path: "session.jsonl", record: cliServeRecord{base: oldServe.URL}, client: oldServe.Client(),
		grant: cliTakeoverGrant{MirrorID: "mirror-old"},
	}
	m.revision = 1
	m.Emit(event.Event{Kind: event.Text, Text: "recover me"})
	if !m.push(false) {
		t.Fatal("manager stopped during re-adopt")
	}
	if !m.push(false) {
		t.Fatal("manager stopped during re-adopt")
	}
	if delivered.Load() != 1 {
		t.Fatalf("new serve received %d frame batches, want 1", delivered.Load())
	}
	binding, _, _, revision := m.snapshot()
	if binding == nil || binding.grant.MirrorID != "mirror-new" || revision <= 1 {
		t.Fatalf("binding after re-adopt = %+v revision=%d", binding, revision)
	}
}

func TestCLITakeoverManagerReadoptsAfterConnectionRefused(t *testing.T) {
	deadServe := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	deadURL := deadServe.URL
	deadClient := deadServe.Client()
	deadServe.Close()
	var delivered atomic.Bool
	newServe := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/auth/token":
			w.WriteHeader(http.StatusNoContent)
		case "/adopt":
			_ = json.NewEncoder(w).Encode(cliTakeoverGrant{SessionPath: "session.jsonl", MirrorID: "new", ReturnHandoffID: "return", SourceWriterID: "source", TargetWriterID: agent.SessionWriterID()})
		case "/external/frames":
			delivered.Store(true)
			_ = json.NewEncoder(w).Encode(map[string]bool{"reclaimRequested": false})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer newServe.Close()
	originalDiscover := discoverCLIServesForTakeover
	discoverCLIServesForTakeover = func() []cliServeRecord {
		return []cliServeRecord{{base: newServe.URL, token: "fresh"}}
	}
	t.Cleanup(func() { discoverCLIServesForTakeover = originalDiscover })
	m := newCLITakeoverManager(nil, nil)
	m.binding = &cliTakeoverBinding{path: "session.jsonl", record: cliServeRecord{base: deadURL}, client: deadClient, grant: cliTakeoverGrant{MirrorID: "old"}}
	m.revision = 1
	m.Emit(event.Event{Kind: event.Text, Text: "recover"})
	if !m.push(false) {
		t.Fatal("connection refusal stopped the manager during rediscovery")
	}
	if !m.push(false) {
		t.Fatal("connection refusal stopped the manager after rediscovery")
	}
	if !delivered.Load() {
		t.Fatal("connection refusal did not rediscover and deliver through the new serve")
	}
}

func TestCLITakeoverManagerKeepsActualRequestBelowEightMiB(t *testing.T) {
	var maxBody atomic.Int64
	var delivered atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Error(err)
			return
		}
		for {
			observed := maxBody.Load()
			if int64(len(body)) <= observed || maxBody.CompareAndSwap(observed, int64(len(body))) {
				break
			}
		}
		if len(body) > eventwire.MirrorBatchMaxBytes {
			t.Errorf("request body = %d bytes", len(body))
		}
		var payload struct {
			Frames []eventwire.Event `json:"frames"`
		}
		if err := json.Unmarshal(body, &payload); err != nil {
			t.Error(err)
			return
		}
		delivered.Add(int32(len(payload.Frames)))
		_ = json.NewEncoder(w).Encode(map[string]bool{"reclaimRequested": false})
	}))
	defer srv.Close()
	m := newCLITakeoverManager(nil, nil)
	m.binding = &cliTakeoverBinding{path: "session.jsonl", record: cliServeRecord{base: srv.URL}, client: srv.Client(), grant: cliTakeoverGrant{MirrorID: "mirror"}}
	m.revision = 1
	chunk := strings.Repeat("x", 1<<20)
	for range 10 {
		m.Emit(event.Event{Kind: event.Text, Text: chunk})
	}
	for delivered.Load() < 10 {
		if !m.push(false) {
			t.Fatal("manager stopped while chunking large request")
		}
	}
	if maxBody.Load() > eventwire.MirrorBatchMaxBytes {
		t.Fatalf("largest body = %d", maxBody.Load())
	}
}

func TestCLITakeoverManagerRediscoverAfterThreeServerErrors(t *testing.T) {
	oldServe := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer oldServe.Close()
	newServe := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/auth/token":
			w.WriteHeader(http.StatusNoContent)
		case "/adopt":
			_ = json.NewEncoder(w).Encode(cliTakeoverGrant{SessionPath: "session.jsonl", MirrorID: "new", ReturnHandoffID: "return", SourceWriterID: "source", TargetWriterID: agent.SessionWriterID()})
		default:
			_ = json.NewEncoder(w).Encode(map[string]bool{"reclaimRequested": false})
		}
	}))
	defer newServe.Close()
	var discoveries atomic.Int32
	originalDiscover := discoverCLIServesForTakeover
	discoverCLIServesForTakeover = func() []cliServeRecord {
		discoveries.Add(1)
		return []cliServeRecord{{base: newServe.URL, token: "fresh"}}
	}
	t.Cleanup(func() { discoverCLIServesForTakeover = originalDiscover })

	m := newCLITakeoverManager(&takeoverRecordSink{}, nil)
	m.binding = &cliTakeoverBinding{path: "session.jsonl", record: cliServeRecord{base: oldServe.URL}, client: oldServe.Client(), grant: cliTakeoverGrant{MirrorID: "old"}}
	m.revision = 1
	m.Emit(event.Event{Kind: event.Text, Text: "retry"})
	for range cliTakeoverRediscoverFailures {
		if !m.push(false) {
			t.Fatal("manager stopped before rediscovery")
		}
	}
	if discoveries.Load() != 1 {
		t.Fatalf("discoveries = %d, want 1 after threshold", discoveries.Load())
	}
}

func TestCLITakeoverManagerRetainsPendingReturnUntilReservationSucceeds(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "source.jsonl")
	target := filepath.Join(dir, "target.jsonl")
	leases := control.NewSessionLeaseKeeper()
	defer leases.Release()
	if err := leases.Rebind(source); err != nil {
		t.Fatal(err)
	}
	targetKeeper := control.NewSessionLeaseKeeper()
	if err := targetKeeper.Rebind(target); err != nil {
		t.Fatal(err)
	}
	var mirrorEnded atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/mirror-end" {
			mirrorEnded.Store(true)
			w.WriteHeader(http.StatusNoContent)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()
	binding := &cliTakeoverBinding{
		path: target, record: cliServeRecord{base: srv.URL}, client: srv.Client(),
		grant: cliTakeoverGrant{MirrorID: "mirror", SourceWriterID: "serve", ReturnHandoffID: "return"},
	}
	m := newCLITakeoverManager(&takeoverRecordSink{}, leases)
	wantErr := errors.New("injected reservation failure")
	var calls atomic.Int32
	m.retirePending = func(keeper *control.SessionLeaseKeeper, writerID, handoffID string) error {
		if calls.Add(1) == 1 {
			return wantErr
		}
		return keeper.RetireDetachedForHandoff(writerID, handoffID)
	}
	m.pending = []*cliPendingReturn{{keeper: targetKeeper, binding: binding, nextTry: time.Now()}}
	m.retryPendingReturns(true)
	if mirrorEnded.Load() {
		t.Fatal("mirror ended before the reservation succeeded")
	}
	if got := leases.HeldPath(); got != agent.CanonicalSessionPath(source) {
		t.Fatalf("source keeper moved to %q", got)
	}
	if third, err := agent.TryAcquireSessionLease(target); !errors.Is(err, agent.ErrSessionLeaseHeld) {
		if third != nil {
			third.Release()
		}
		t.Fatalf("third writer acquired pending target: %v", err)
	}
	m.retryPendingReturns(true)
	if !mirrorEnded.Load() {
		t.Fatal("mirror did not end after reservation retry succeeded")
	}
	info, err := agent.LoadSessionLeaseInfo(target)
	if err != nil {
		t.Fatal(err)
	}
	if info.HandoffTo != "serve" || info.HandoffID != "return" {
		t.Fatalf("target reservation = %+v", info)
	}
}
