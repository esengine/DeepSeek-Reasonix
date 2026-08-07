package cli

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	tea "charm.land/bubbletea/v2"
)

const (
	tuiDiagnosticLogLimit     = 4 << 20
	tuiDiagnosticLogRetention = 7 * 24 * time.Hour
	tuiWatchdogInterval       = time.Second
	tuiWatchdogStall          = 10 * time.Second
	tuiWatchdogCancelGrace    = 2 * time.Second
)

// Watchdog lifecycle phases. Only booting (no first Update) and running
// (active turn / shell with no event-loop heartbeat) can escalate to kill.
// Idle never terminates the process — that was the #7809 false-kill path.
type tuiWatchdogPhase int

const (
	watchdogBooting tuiWatchdogPhase = iota
	watchdogIdle
	watchdogRunning
	watchdogClosed
)

func (p tuiWatchdogPhase) String() string {
	switch p {
	case watchdogBooting:
		return "booting"
	case watchdogIdle:
		return "idle"
	case watchdogRunning:
		return "running"
	case watchdogClosed:
		return "closed"
	default:
		return fmt.Sprintf("phase(%d)", int(p))
	}
}

// tuiDiagnostics owns diagnostics for the interactive terminal UI. Logs and
// plugin stderr use a private file; typed notices remain user-facing if it
// cannot be created. The watchdog uses booting/idle/running/closed: idle never
// kills, while running stalls escalate through dump, cancel, grace, and kill.
type tuiDiagnostics struct {
	previous *slog.Logger
	logger   *slog.Logger
	writer   io.Writer
	file     *os.File
	path     string
	close    sync.Once

	stopWatch chan struct{}
	watchOnce sync.Once
	watchWG   sync.WaitGroup

	mu sync.Mutex

	phase               tuiWatchdogPhase
	generation          uint64 // increments on each idle→running transition
	lastHeartbeat       time.Time
	lastHeartbeatSource string
	// escalatedGeneration is the generation currently inside dump/cancel/grace
	// (0 means none). Cleared when a heartbeat aborts the grace window so a
	// later stall can re-enter escalation for hard-kill only.
	escalatedGeneration uint64
	// cancelIssuedGeneration is sticky for the Turn: Cancel() runs at most once
	// per generation even if the grace window is aborted and the turn stalls
	// again.
	cancelIssuedGeneration uint64
	cancelDeadline         time.Time // zero until escalated for current generation
	// hardKillIssued is true after a hard-kill for hardKilledGen.
	hardKillIssued bool
	hardKilledGen  uint64

	// cancelFn is the non-blocking controller cancel for the active generation.
	// Cleared on idle/closed. Invoked under mu after a generation check.
	cancelFn func()
	// statusFn optionally returns Controller RuntimeStatus text for dumps.
	statusFn func() string

	// Injectable seams for deterministic tests (nil = production defaults).
	nowFn     func() time.Time
	newTicker func(d time.Duration) watchdogTicker
	dumpFn    func(reason string)
	killFn    func()
	logFn     func(format string, args ...any)

	// Test observation counters (safe under mu).
	cancelCalls atomic.Int32
	killCalls   atomic.Int32
	dumpCalls   atomic.Int32
}

// watchdogTicker is the subset of time.Ticker used by the watch loop.
type watchdogTicker interface {
	C() <-chan time.Time
	Stop()
}

type realTicker struct{ *time.Ticker }

func (t realTicker) C() <-chan time.Time { return t.Ticker.C }

func startTUIDiagnostics(reasonixHome string) *tuiDiagnostics {
	d := &tuiDiagnostics{
		previous:  slog.Default(),
		writer:    io.Discard,
		stopWatch: make(chan struct{}),
		phase:     watchdogBooting,
	}
	if logDir := tuiDiagnosticLogDir(reasonixHome); logDir != "" {
		if err := os.MkdirAll(logDir, 0o700); err == nil {
			pruneTUIDiagnosticLogs(logDir, time.Now())
			if file, err := os.CreateTemp(logDir, "cli-tui-*.log"); err == nil {
				d.file = file
				d.path = file.Name()
				d.writer = &boundedDiagnosticWriter{dst: file, remaining: tuiDiagnosticLogLimit}
			}
		}
	}
	d.logger = slog.New(slog.NewTextHandler(d.writer, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(d.logger)
	d.lastHeartbeat = d.now()
	d.lastHeartbeatSource = "diagnostics_started"
	d.Milestone("diagnostics_started")
	return d
}

// Milestone records a startup/runtime phase and flushes the log immediately so a
// subsequent hang still leaves a non-zero diagnostic file. Milestones do not
// count as active event-loop heartbeats.
func (d *tuiDiagnostics) Milestone(name string) {
	if d == nil {
		return
	}
	msg := fmt.Sprintf("milestone=%s t=%s", strings.TrimSpace(name), d.now().UTC().Format(time.RFC3339Nano))
	_, _ = fmt.Fprintln(d.Writer(), msg)
	d.Sync()
}

// Sync flushes the diagnostic file to disk when possible.
func (d *tuiDiagnostics) Sync() {
	if d == nil || d.file == nil {
		return
	}
	_ = d.file.Sync()
}

// Path returns the diagnostic log path (empty when falling back to Discard).
func (d *tuiDiagnostics) Path() string {
	if d == nil {
		return ""
	}
	return d.path
}

func (d *tuiDiagnostics) Writer() io.Writer {
	if d == nil || d.writer == nil {
		return io.Discard
	}
	return d.writer
}

// NoteBooted transitions booting → idle after the first valid chatTUI.Update.
// Subsequent calls are no-ops until the watchdog is closed.
func (d *tuiDiagnostics) NoteBooted() {
	if d == nil {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.phase != watchdogBooting {
		return
	}
	d.phase = watchdogIdle
	d.lastHeartbeat = d.now()
	d.lastHeartbeatSource = "booted"
	d.logfLocked("watchdog_state phase=%s gen=%d source=booted", d.phase, d.generation)
}

// NoteRunning transitions idle/booting → running for a new Turn/shell generation.
// cancel must be non-blocking (context cancel / queue a cancel); it is invoked
// under the watchdog lifecycle lock after checking the active generation.
func (d *tuiDiagnostics) NoteRunning(cancel func()) {
	if d == nil {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.phase == watchdogClosed {
		return
	}
	d.generation++
	d.phase = watchdogRunning
	d.lastHeartbeat = d.now()
	d.lastHeartbeatSource = "enter_running"
	d.cancelFn = cancel
	d.cancelDeadline = time.Time{}
	d.logfLocked("watchdog_state phase=%s gen=%d source=enter_running", d.phase, d.generation)
}

// NoteIdle transitions running → idle after TurnDone, shell completion, or
// synchronous cancel. Clears the active cancel hook and cancels any pending
// hard-kill for the previous generation.
func (d *tuiDiagnostics) NoteIdle() {
	if d == nil {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.phase == watchdogClosed || d.phase == watchdogIdle {
		return
	}
	prev := d.phase
	d.phase = watchdogIdle
	d.cancelFn = nil
	d.cancelDeadline = time.Time{}
	d.lastHeartbeat = d.now()
	d.lastHeartbeatSource = "enter_idle"
	d.logfLocked("watchdog_state phase=%s gen=%d prev=%s source=enter_idle", d.phase, d.generation, prev)
}

// NoteActiveHeartbeat records event-loop progress that proves a running turn
// is still being serviced. Only elapsedTick, agent/shell/controller work
// events, and explicitly marked work progress should call this. Keyboard,
// mouse, and focus activity must not refresh the active heartbeat.
func (d *tuiDiagnostics) NoteActiveHeartbeat(source string) {
	if d == nil {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.phase != watchdogRunning {
		return
	}
	d.lastHeartbeat = d.now()
	if source == "" {
		source = "active"
	}
	d.lastHeartbeatSource = source
	// Fresh heartbeat aborts the in-flight grace window for this gen so a later
	// stall may re-enter dump/grace/hard-kill. Cancel stays sticky via
	// cancelIssuedGeneration (at most one Cancel per Turn).
	if d.escalatedGeneration == d.generation && d.generation != 0 && !d.cancelDeadline.IsZero() {
		d.cancelDeadline = time.Time{}
		d.escalatedGeneration = 0
		d.logfLocked("watchdog_escalation_aborted phase=%s gen=%d source=%s reason=heartbeat cancel_issued=%v",
			d.phase, d.generation, d.lastHeartbeatSource, d.cancelIssuedGeneration == d.generation)
	}
}

// SetStatusProvider installs an optional Controller RuntimeStatus snapshot for
// structured stall dumps. Safe to call at any time.
func (d *tuiDiagnostics) SetStatusProvider(fn func() string) {
	if d == nil {
		return
	}
	d.mu.Lock()
	d.statusFn = fn
	d.mu.Unlock()
}

// StartWatchdog arms the lifecycle watchdog. p may be nil in tests that inject killFn.
// The booting timer starts here (not at diagnostics construction) so slow config /
// controller setup before terminal takeover cannot trip a false boot stall.
func (d *tuiDiagnostics) StartWatchdog(p *tea.Program) {
	if d == nil {
		return
	}
	d.watchOnce.Do(func() {
		if d.killFn == nil && p != nil {
			d.killFn = p.Kill
		}
		d.mu.Lock()
		if d.phase == watchdogBooting {
			d.lastHeartbeat = d.now()
			d.lastHeartbeatSource = "watchdog_armed"
		}
		d.mu.Unlock()
		d.watchWG.Add(1)
		go func() {
			defer d.watchWG.Done()
			d.watch()
		}()
	})
}

func (d *tuiDiagnostics) watch() {
	newTicker := d.newTicker
	if newTicker == nil {
		newTicker = func(interval time.Duration) watchdogTicker {
			return realTicker{time.NewTicker(interval)}
		}
	}
	ticker := newTicker(tuiWatchdogInterval)
	defer ticker.Stop()
	for {
		select {
		case <-d.stopWatch:
			return
		case now := <-ticker.C():
			d.onTick(now)
		}
	}
}

// onTick is the pure escalation step. Tests drive it directly with a fake clock
// so no real sleeps are required.
func (d *tuiDiagnostics) onTick(now time.Time) {
	if d == nil {
		return
	}

	d.mu.Lock()
	phase := d.phase
	if phase == watchdogClosed {
		d.mu.Unlock()
		return
	}
	gen := d.generation
	last := d.lastHeartbeat
	if last.IsZero() {
		last = now
	}
	age := now.Sub(last)
	source := d.lastHeartbeatSource
	cancelDeadline := d.cancelDeadline
	escalatedGen := d.escalatedGeneration
	hardKillIssued := d.hardKillIssued
	hardKilledGen := d.hardKilledGen
	statusFn := d.statusFn
	cancelFn := d.cancelFn
	d.logfLocked("heartbeat t=%s phase=%s gen=%d last_progress_age=%s last_source=%s cancel_requested=%v hard_kill_phase=%v",
		now.UTC().Format(time.RFC3339Nano),
		phase,
		gen,
		age.Round(time.Millisecond),
		source,
		escalatedGen == gen && gen != 0 && !cancelDeadline.IsZero(),
		hardKillIssued && hardKilledGen == gen,
	)
	d.Sync()

	// Idle: never dump/cancel/kill. Heartbeat log above is the only activity.
	if phase == watchdogIdle {
		d.mu.Unlock()
		return
	}

	// Grace window follow-up: same generation still running after cancel.
	// Wait until the deadline; only then hard-kill if there is still no heartbeat.
	if phase == watchdogRunning && escalatedGen == gen && gen != 0 && !cancelDeadline.IsZero() {
		if now.Before(cancelDeadline) {
			d.mu.Unlock()
			return
		}
		// Deadline reached. Re-check heartbeat under the same lock before kill.
		if now.Sub(d.lastHeartbeat) >= tuiWatchdogStall && !(d.hardKillIssued && d.hardKilledGen == gen) {
			d.hardKillIssued = true
			d.hardKilledGen = gen
			d.cancelDeadline = time.Time{}
			diag := d.formatDiagLocked(now, "watchdog_hard_kill")
			d.mu.Unlock()
			d.killCalls.Add(1)
			d.doDump("watchdog_hard_kill")
			d.writeLine(diag)
			d.Sync()
			d.doKill()
			return
		}
		// Heartbeat recovered (or phase raced) before hard-kill — clear grace.
		d.cancelDeadline = time.Time{}
		d.mu.Unlock()
		return
	}

	if age < tuiWatchdogStall {
		d.mu.Unlock()
		return
	}

	// Already hard-killed this generation — stay quiet.
	if hardKillIssued && hardKilledGen == gen {
		d.mu.Unlock()
		return
	}

	// Already escalated (dump+cancel) for this generation; waiting on grace.
	// gen==0 is booting (no generation yet); use a dedicated escalated flag path.
	if phase == watchdogRunning && escalatedGen == gen && gen != 0 {
		d.mu.Unlock()
		return
	}
	if phase == watchdogBooting && hardKillIssued {
		d.mu.Unlock()
		return
	}

	// First escalation for this stall (or re-entry after a grace abort).
	diag := d.formatDiagLocked(now, "watchdog_stall")
	if phase == watchdogRunning {
		d.escalatedGeneration = gen
		d.cancelDeadline = now.Add(tuiWatchdogCancelGrace)
		// Cancel at most once per generation; re-stalls after recovery still
		// get dump + grace + hard-kill, but not a second Cancel().
		issueCancel := cancelFn != nil && d.cancelIssuedGeneration != gen
		if issueCancel {
			d.cancelIssuedGeneration = gen
		}
		// Snapshot cancel under lock; invoke after the dump outside this critical
		// section, with a second generation check immediately before the call.
		d.mu.Unlock()
		d.dumpCalls.Add(1)
		d.doDump("watchdog_stall")
		d.writeLine(diag)
		if statusFn != nil {
			if st := statusFn(); st != "" {
				d.writeLine("controller_status " + st)
			}
		}
		d.Sync()
		if issueCancel {
			d.cancelCurrentGeneration(gen, cancelFn)
		}
		return
	}

	// Booting stall: dump + hard-kill (no controller turn to cancel).
	d.hardKillIssued = true
	d.hardKilledGen = gen
	d.mu.Unlock()
	d.dumpCalls.Add(1)
	d.killCalls.Add(1)
	d.doDump("watchdog_boot_stall")
	d.writeLine(diag)
	d.Sync()
	d.doKill()
}

func (d *tuiDiagnostics) cancelCurrentGeneration(gen uint64, cancelFn func()) {
	if d == nil || cancelFn == nil {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.phase != watchdogRunning || d.generation != gen || d.cancelIssuedGeneration != gen {
		return
	}
	d.cancelCalls.Add(1)
	cancelFn()
}

func (d *tuiDiagnostics) formatDiagLocked(now time.Time, reason string) string {
	age := now.Sub(d.lastHeartbeat)
	if d.lastHeartbeat.IsZero() {
		age = 0
	}
	return fmt.Sprintf(
		"watchdog_diag reason=%s phase=%s gen=%d last_heartbeat_age=%s last_event=%s cancel_requested=%v hard_kill_phase=%v",
		reason,
		d.phase,
		d.generation,
		age.Round(time.Millisecond),
		d.lastHeartbeatSource,
		d.escalatedGeneration == d.generation && d.generation != 0 && !d.cancelDeadline.IsZero(),
		d.hardKillIssued && d.hardKilledGen == d.generation,
	)
}

func (d *tuiDiagnostics) doDump(reason string) {
	if d == nil {
		return
	}
	if d.dumpFn != nil {
		d.dumpFn(reason)
		return
	}
	d.dumpGoroutines(reason)
}

func (d *tuiDiagnostics) doKill() {
	if d == nil {
		return
	}
	if d.killFn != nil {
		d.killFn()
	}
}

func (d *tuiDiagnostics) dumpGoroutines(reason string) {
	if d == nil {
		return
	}
	buf := make([]byte, 1<<20)
	for {
		n := runtime.Stack(buf, true)
		if n < len(buf) {
			buf = buf[:n]
			break
		}
		buf = make([]byte, len(buf)*2)
	}
	_, _ = fmt.Fprintf(d.Writer(), "goroutine_dump reason=%s bytes=%d\n%s\n", reason, len(buf), buf)
}

func (d *tuiDiagnostics) writeLine(line string) {
	if d == nil {
		return
	}
	_, _ = fmt.Fprintln(d.Writer(), line)
}

func (d *tuiDiagnostics) now() time.Time {
	if d != nil && d.nowFn != nil {
		return d.nowFn()
	}
	return time.Now()
}

func (d *tuiDiagnostics) logfLocked(format string, args ...any) {
	if d.logFn != nil {
		d.logFn(format, args...)
		return
	}
	_, _ = fmt.Fprintf(d.Writer(), format+"\n", args...)
}

// phaseForTest returns the current phase under lock (test helper).
func (d *tuiDiagnostics) phaseForTest() tuiWatchdogPhase {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.phase
}

// generationForTest returns the current generation under lock (test helper).
func (d *tuiDiagnostics) generationForTest() uint64 {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.generation
}

func (d *tuiDiagnostics) Close() {
	if d == nil {
		return
	}
	d.close.Do(func() {
		d.mu.Lock()
		d.phase = watchdogClosed
		d.cancelFn = nil
		d.cancelDeadline = time.Time{}
		d.logfLocked("watchdog_state phase=%s gen=%d source=closed", d.phase, d.generation)
		d.mu.Unlock()

		select {
		case <-d.stopWatch:
		default:
			close(d.stopWatch)
		}
		// Wait for the watchdog to fully exit before closing the log. A timed
		// wait left a window where runtime.Stack / Sync / Kill could still write
		// the file after Close returned.
		d.watchWG.Wait()
		// Do not overwrite a logger deliberately installed by another owner
		// after the TUI started.
		if slog.Default() == d.logger && d.previous != nil {
			slog.SetDefault(d.previous)
		}
		if d.file != nil {
			_ = d.file.Sync()
			_ = d.file.Close()
		}
	})
}

func tuiDiagnosticLogDir(reasonixHome string) string {
	if strings.TrimSpace(reasonixHome) == "" {
		return ""
	}
	return filepath.Join(reasonixHome, "logs")
}

func pruneTUIDiagnosticLogs(logDir string, now time.Time) {
	entries, err := os.ReadDir(logDir)
	if err != nil {
		return
	}
	cutoff := now.Add(-tuiDiagnosticLogRetention)
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasPrefix(entry.Name(), "cli-tui-") || !strings.HasSuffix(entry.Name(), ".log") {
			continue
		}
		info, err := entry.Info()
		if err != nil || !info.ModTime().Before(cutoff) {
			continue
		}
		_ = os.Remove(filepath.Join(logDir, entry.Name()))
	}
}

type boundedDiagnosticWriter struct {
	mu        sync.Mutex
	dst       io.Writer
	remaining int64
	truncated bool
}

func (w *boundedDiagnosticWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	total := len(p)
	if total == 0 || w.dst == nil || w.remaining <= 0 {
		return total, nil
	}
	n := total
	if int64(n) > w.remaining {
		n = int(w.remaining)
	}
	written, err := w.dst.Write(p[:n])
	if written > 0 {
		w.remaining -= int64(written)
	}
	if err != nil || written != n {
		w.remaining = 0
		return total, nil
	}
	if n < total && !w.truncated {
		w.truncated = true
		_, _ = io.WriteString(w.dst, "\nreasonix: CLI TUI diagnostic log limit reached; further diagnostics omitted\n")
		w.remaining = 0
	}
	return total, nil
}
