package cli

import (
	"errors"
	"io"
	"sync/atomic"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"reasonix/internal/control"
)

type shutdownSnapshotSpy struct {
	control.SessionAPI
	err           error
	started       chan<- struct{}
	release       <-chan struct{}
	snapshotCalls atomic.Int32
	shutdownCalls atomic.Int32
}

func (s *shutdownSnapshotSpy) Snapshot() error {
	s.snapshotCalls.Add(1)
	return nil
}

func (s *shutdownSnapshotSpy) SnapshotForShutdown() error {
	s.shutdownCalls.Add(1)
	if s.started != nil {
		s.started <- struct{}{}
	}
	if s.release != nil {
		<-s.release
	}
	return s.err
}

func TestTUIShutdownUsesRecoveringSnapshotAndKeepsFailure(t *testing.T) {
	wantErr := errors.New("final snapshot failed")
	ctrl := &shutdownSnapshotSpy{err: wantErr}
	m := newTestChatTUI()
	m.ctrl = ctrl
	completion := newTUIShutdownCompletion()

	next, cmd := m.update(tuiShutdownMsg{completion: completion})
	got := next.(chatTUI)
	if cmd == nil || cmd() != (tea.QuitMsg{}) {
		t.Fatal("shutdown message did not return tea.Quit")
	}
	if calls := ctrl.snapshotCalls.Load(); calls != 0 {
		t.Fatalf("plain Snapshot calls = %d, want 0", calls)
	}
	if calls := ctrl.shutdownCalls.Load(); calls != 1 {
		t.Fatalf("SnapshotForShutdown calls = %d, want 1", calls)
	}
	if !errors.Is(got.shutdownErr, wantErr) {
		t.Fatalf("shutdownErr = %v, want %v", got.shutdownErr, wantErr)
	}
	select {
	case <-completion.done:
	default:
		t.Fatal("shutdown completion was not acknowledged after the final snapshot")
	}
}

func TestTUIShutdownSignalDoesNotQuitAfterReclaim(t *testing.T) {
	m := newTestChatTUI()
	m.sessionReclaimed = true
	completion := newTUIShutdownCompletion()

	next, cmd := m.update(tuiShutdownMsg{completion: completion})
	if cmd != nil {
		t.Fatalf("stale shutdown after reclaim returned %T, want no quit command", cmd)
	}
	if !next.(chatTUI).sessionReclaimed {
		t.Fatal("reclaim marker was cleared by stale shutdown")
	}
	select {
	case <-completion.done:
	default:
		t.Fatal("stale shutdown completion was not acknowledged")
	}

	_, cmd = m.update(tuiShutdownMsg{userInitiated: true})
	if cmd == nil || cmd() != (tea.QuitMsg{}) {
		t.Fatal("explicit quit after reclaim did not return tea.Quit")
	}
}

func TestTUIShutdownSignalDoesNotQuitWhileReclaiming(t *testing.T) {
	m := newTestChatTUI()
	m.takeover = newCLITakeoverManager(nil, nil)
	m.takeover.reclaiming.Store(true)
	completion := newTUIShutdownCompletion()

	next, cmd := m.update(tuiShutdownMsg{completion: completion})
	if cmd != nil {
		t.Fatalf("shutdown during reclaim returned %T, want no quit command", cmd)
	}
	if !next.(chatTUI).takeover.Reclaiming() {
		t.Fatal("reclaim marker was cleared by shutdown race")
	}
	select {
	case <-completion.done:
	default:
		t.Fatal("shutdown during reclaim was not acknowledged")
	}
}

func TestBubbleTeaKeepsRunningWhenShutdownRacesReclaim(t *testing.T) {
	m := newTestChatTUI()
	m.takeover = newCLITakeoverManager(nil, nil)
	m.takeover.reclaiming.Store(true)
	p := tea.NewProgram(
		shutdownOnlyProgramModel{chatTUI: m},
		tea.WithInput(nil),
		tea.WithOutput(io.Discard),
		tea.WithoutRenderer(),
		tea.WithoutSignals(),
	)

	type result struct {
		model tea.Model
		err   error
	}
	done := make(chan result, 1)
	go func() {
		model, err := p.Run()
		done <- result{model: model, err: err}
	}()
	assertRunning := func(stage string) {
		t.Helper()
		select {
		case result := <-done:
			t.Fatalf("shutdown ended Bubble Tea %s: model=%T err=%v", stage, result.model, result.err)
		case <-time.After(100 * time.Millisecond):
		}
	}

	// Exercise every ordering window in the real program loop. The process must
	// survive both the transaction and the gap before its UI callback arrives.
	p.Send(tuiShutdownMsg{})
	assertRunning("while reclaiming")
	m.takeover.returned.Store(true)
	m.takeover.reclaiming.Store(false)
	p.Send(tuiShutdownMsg{})
	assertRunning("after return and before the reclaim callback")
	p.Send(tuiSessionReclaimedMsg{})
	p.Send(tuiShutdownMsg{})
	assertRunning("after the reclaim callback")

	p.Send(tuiShutdownMsg{userInitiated: true})
	select {
	case result := <-done:
		if result.err != nil {
			t.Fatalf("explicit shutdown error = %v", result.err)
		}
	case <-time.After(time.Second):
		p.Kill()
		t.Fatal("Bubble Tea did not exit after explicit quit")
	}
}

// shutdownOnlyProgramModel suppresses chatTUI's unrelated rendering and Init
// work while delegating messages to the production shutdown handler.
type shutdownOnlyProgramModel struct{ chatTUI }

func (shutdownOnlyProgramModel) Init() tea.Cmd { return nil }

func (m shutdownOnlyProgramModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	next, cmd := m.update(msg)
	return shutdownOnlyProgramModel{chatTUI: next.(chatTUI)}, cmd
}

func (shutdownOnlyProgramModel) View() tea.View { return tea.NewView("") }

func TestWatchdogDoesNotReclassifyCompletedBubbleTeaShutdownAsKilled(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	ctrl := &shutdownSnapshotSpy{started: started, release: release}
	m := newTestChatTUI()
	m.ctrl = ctrl
	p := tea.NewProgram(
		shutdownOnlyProgramModel{chatTUI: m},
		tea.WithInput(nil),
		tea.WithOutput(io.Discard),
		tea.WithoutRenderer(),
		tea.WithoutSignals(),
	)

	type runResult struct {
		model tea.Model
		err   error
	}
	runDone := make(chan runResult, 1)
	go func() {
		model, err := p.Run()
		runDone <- runResult{model: model, err: err}
	}()

	scheduled := make(chan func(), 1)
	completionSeen := make(chan *tuiShutdownCompletion, 1)
	var kills atomic.Int32
	d := &tuiDiagnostics{
		afterFunc: func(delay time.Duration, fn func()) {
			if delay != watchdogKillFallbackDelay {
				t.Errorf("fallback delay = %s, want %s", delay, watchdogKillFallbackDelay)
			}
			scheduled <- fn
		},
		shutdownFn: func(completion *tuiShutdownCompletion) {
			completionSeen <- completion
			p.Send(tuiShutdownMsg{completion: completion})
		},
		killFn: func() {
			kills.Add(1)
			p.Kill()
		},
	}
	killRequestDone := make(chan struct{})
	go func() {
		d.doKill()
		close(killRequestDone)
	}()

	var fallback func()
	select {
	case fallback = <-scheduled:
	case <-time.After(time.Second):
		t.Fatal("watchdog fallback was not armed before shutdown")
	}
	var completion *tuiShutdownCompletion
	select {
	case completion = <-completionSeen:
	case <-time.After(time.Second):
		t.Fatal("watchdog did not send a completion-bearing shutdown message")
	}
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("Bubble Tea did not enter the final snapshot")
	}
	close(release)
	select {
	case <-completion.done:
	case <-time.After(time.Second):
		t.Fatal("final snapshot completed without acknowledging shutdown")
	}

	// Exercise the old failure window: Update has finished the snapshot, but
	// Bubble Tea may not have consumed tea.Quit yet when the timer callback runs.
	fallback()
	select {
	case <-killRequestDone:
	case <-time.After(time.Second):
		t.Fatal("watchdog shutdown request remained blocked")
	}
	select {
	case result := <-runDone:
		if result.err != nil {
			t.Fatalf("Bubble Tea shutdown error = %v, want graceful nil", result.err)
		}
		if _, ok := result.model.(shutdownOnlyProgramModel); !ok {
			t.Fatalf("final model = %T, want shutdownOnlyProgramModel", result.model)
		}
	case <-time.After(time.Second):
		p.Kill()
		t.Fatal("Bubble Tea program did not exit")
	}
	if got := kills.Load(); got != 0 {
		t.Fatalf("hard-kill calls = %d, want 0 after completed snapshot", got)
	}
}
