package cli

// Local takeover for the CLI resume paths: when the session lease is held by a
// resident serve process on this machine (left behind by a remote desktop that
// connected over SSH), the user can take the session over instead of exiting
// with a refusal. Serve releases the lease via POST /handoff; the remote tab
// keeps watching read-only through the frame mirror.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"reasonix/internal/agent"
	"reasonix/internal/config"
	"reasonix/internal/control"
	"reasonix/internal/event"
	"reasonix/internal/eventwire"
	"reasonix/internal/remote/bootstrap"
	"reasonix/internal/store"
)

// cliTakeoverTimeout bounds the drain window of a wait-mode takeover.
const cliTakeoverTimeout = 2 * time.Minute

type cliServeRecord struct {
	pid   int
	base  string
	token string
}

type cliTakeoverGrant struct {
	SessionPath     string `json:"sessionPath"`
	MirrorID        string `json:"mirrorId"`
	HandoffID       string `json:"handoffId,omitempty"`
	ReturnHandoffID string `json:"returnHandoffId"`
	SourceWriterID  string `json:"sourceWriterId"`
	TargetWriterID  string `json:"targetWriterId"`
}

type cliTakeoverBinding struct {
	path        string
	record      cliServeRecord
	client      *http.Client
	grant       cliTakeoverGrant
	previous    *control.SessionLeaseKeeper
	priorMirror *cliTakeoverBinding
}

// discoverCLIServes enumerates resident serve processes recorded under
// <Reasonix home>/remote. This machine is the SSH target in the takeover
// scenario, so the bootstrap's SFTP-written state files are local files here.
func discoverCLIServes() []cliServeRecord {
	dir := config.RemoteStateDir()
	if dir == "" {
		return nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []cliServeRecord
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasPrefix(name, "serve-") || !strings.HasSuffix(name, ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			continue
		}
		state, err := bootstrap.UnmarshalState(data)
		if err != nil || state.PID <= 0 {
			continue
		}
		slug := strings.TrimSuffix(strings.TrimPrefix(name, "serve-"), ".json")
		addr := state.Addr
		if port, err := os.ReadFile(filepath.Join(dir, store.RemoteServePortName(slug))); err == nil {
			if trimmed := strings.TrimSpace(string(port)); trimmed != "" {
				addr = trimmed
			}
		}
		if addr == "" {
			continue
		}
		token := ""
		if data, err := os.ReadFile(filepath.Join(dir, store.RemoteServeTokenName(slug))); err == nil {
			token = strings.TrimSpace(string(data))
		}
		if token == "" {
			continue
		}
		out = append(out, cliServeRecord{pid: state.PID, base: "http://" + addr, token: token})
	}
	return out
}

var discoverCLIServesForTakeover = discoverCLIServes

// cliServeForPID finds the resident serve holding the lease by matching the
// holder PID the lease error reported.
func cliServeForPID(pid int) *cliServeRecord {
	records := discoverCLIServes()
	for i := range records {
		if records[i].pid == pid {
			return &records[i]
		}
	}
	return nil
}

func cliServeClient(ctx context.Context, record cliServeRecord) (*http.Client, error) {
	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, err
	}
	client := &http.Client{Jar: jar}
	auth, _ := json.Marshal(map[string]string{"token": record.token})
	authReq, err := http.NewRequestWithContext(ctx, http.MethodPost, record.base+"/auth/token", bytes.NewReader(auth))
	if err != nil {
		return nil, err
	}
	authReq.Header.Set("Content-Type", "application/json")
	authResp, err := client.Do(authReq)
	if err != nil {
		return nil, err
	}
	_, _ = io.Copy(io.Discard, authResp.Body)
	authResp.Body.Close()
	if authResp.StatusCode != http.StatusNoContent {
		return nil, fmt.Errorf("serve auth: status %d", authResp.StatusCode)
	}
	return client, nil
}

// cliTakeoverHeldSession requests a target-writer reservation and consumes it
// through leases. The previous keeper binding is retained if either step
// fails; callers commit their controller only after this returns a binding.
func cliTakeoverHeldSession(sessionPath string, leaseErr error, leases *control.SessionLeaseKeeper, manager *cliTakeoverManager) (*cliTakeoverBinding, error) {
	if manager != nil && manager.Reclaiming() {
		return nil, fmt.Errorf("the remote side is reclaiming the current session")
	}
	pid := 0
	var leaseError *agent.SessionLeaseError
	if errors.As(leaseErr, &leaseError) && leaseError != nil && leaseError.Info != nil {
		pid = leaseError.Info.PID
	}
	if pid <= 0 {
		return nil, fmt.Errorf("%w; no local serve identity to take over from", agent.ErrSessionLeaseHeld)
	}
	record := cliServeForPID(pid)
	if record == nil {
		return nil, fmt.Errorf("%w; holder pid %d is not a resident serve on this machine", agent.ErrSessionLeaseHeld, pid)
	}
	ctx, cancel := context.WithTimeout(context.Background(), cliTakeoverTimeout+15*time.Second)
	defer cancel()
	client, err := cliServeClient(ctx, *record)
	if err != nil {
		return nil, fmt.Errorf("takeover from local serve (pid %d): %w", pid, err)
	}
	body, _ := json.Marshal(map[string]any{
		"sessionPath": sessionPath, "targetWriterId": agent.SessionWriterID(),
		"force": true, "mode": "wait", "timeoutMs": cliTakeoverTimeout.Milliseconds(),
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, record.base+"/handoff", bytes.NewReader(body))
	if err == nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("takeover from local serve (pid %d): %w", pid, err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("takeover from local serve (pid %d): %s", pid, strings.TrimSpace(string(respBody)))
	}
	var grant cliTakeoverGrant
	if json.Unmarshal(respBody, &grant) != nil || grant.MirrorID == "" || grant.HandoffID == "" ||
		grant.ReturnHandoffID == "" || grant.SourceWriterID == "" || grant.TargetWriterID != agent.SessionWriterID() {
		return nil, fmt.Errorf("takeover from local serve (pid %d): invalid handoff grant", pid)
	}
	binding := &cliTakeoverBinding{path: sessionPath, record: *record, client: client, grant: grant}
	if manager != nil {
		current, _, _, _ := manager.snapshot()
		if current != nil && !manager.Returned() && agent.CanonicalSessionPath(current.path) != agent.CanonicalSessionPath(sessionPath) {
			binding.priorMirror = current
		}
	}
	previous, err := leases.RebindDetachingWithHandoff(sessionPath, grant.SourceWriterID, grant.HandoffID)
	if err != nil {
		cliEndFailedHandoff(binding)
		return nil, err
	}
	binding.previous = previous
	return binding, nil
}

// cliSessionTakeoverCandidate reports whether leaseErr points at a resident
// serve on this machine — the case where a takeover offer makes sense.
func cliSessionTakeoverCandidate(leaseErr error) bool {
	var leaseError *agent.SessionLeaseError
	if !errors.As(leaseErr, &leaseError) || leaseError == nil || leaseError.Info == nil {
		return false
	}
	return cliServeForPID(leaseError.Info.PID) != nil
}

// promptSessionTakeover asks on the terminal (pre-TUI startup) whether to take
// the held session over. Non-interactive sessions answer no.
func promptSessionTakeover(leaseErr error) bool {
	if !isInteractive() {
		return false
	}
	fmt.Fprintf(os.Stderr, "%s\n", sessionLeaseResumeRefusal(leaseErr))
	fmt.Fprint(os.Stderr, "take over the session from this machine's resident serve? [y/N] ")
	answer, err := readCLITakeoverAnswer()
	if err != nil {
		return false
	}
	answer = strings.ToLower(strings.TrimSpace(answer))
	return answer == "y" || answer == "yes"
}

func readCLITakeoverAnswer() (string, error) {
	buf := make([]byte, 64)
	n, err := os.Stdin.Read(buf)
	if n > 0 {
		return string(buf[:n]), nil
	}
	return "", err
}

const (
	cliTakeoverFlushEvery = 120 * time.Millisecond
	cliTakeoverHeartbeat  = 5 * time.Second
	cliTakeoverMaxFrames  = eventwire.MirrorBatchMaxFrames
)

const cliTakeoverRediscoverFailures = 3

type cliPendingReturn struct {
	keeper  *control.SessionLeaseKeeper
	binding *cliTakeoverBinding
	nextTry time.Time
	backoff time.Duration
}

// cliTakeoverManager is the outermost CLI event sink while a handed-off
// session is active. It preserves the terminal sink, mirrors the same typed
// frames to Serve, and cooperatively returns the lease when reclaim is seen.
// One manager survives controller rebuilds; AttachController updates its live
// authority pointer without replacing the sink wired into boot.
type cliTakeoverManager struct {
	event.AuditForwarder
	inner  event.Sink
	leases *control.SessionLeaseKeeper

	// Lock order is returnMu -> sendMu -> mu. Emit only takes mu, so the model
	// event sink never waits for an HTTP request.
	returnMu sync.Mutex
	sendMu   sync.Mutex
	mu       sync.Mutex
	binding  *cliTakeoverBinding
	revision uint64
	failures int
	ctrl     control.SessionAPI
	queue    eventwire.MirrorQueue
	pending  []*cliPendingReturn
	// retirePending is a deterministic failure-injection seam for the pending
	// return retry loop. Production calls RetireDetachedForHandoff directly.
	retirePending func(*control.SessionLeaseKeeper, string, string) error
	wake          chan struct{}
	stop          chan struct{}
	done          chan struct{}
	onYield       func()

	started    bool
	stopOnce   sync.Once
	reclaiming atomic.Bool
	returned   atomic.Bool
	closed     atomic.Bool
}

func newCLITakeoverManager(inner event.Sink, leases *control.SessionLeaseKeeper) *cliTakeoverManager {
	return &cliTakeoverManager{AuditForwarder: event.AuditForwarder{Inner: inner}, inner: inner, leases: leases}
}

func (m *cliTakeoverManager) SetInner(inner event.Sink) {
	if m == nil {
		return
	}
	m.mu.Lock()
	m.inner = inner
	m.Inner = inner
	m.mu.Unlock()
}

func (m *cliTakeoverManager) Emit(e event.Event) {
	if m == nil {
		return
	}
	m.mu.Lock()
	inner := m.inner
	m.mu.Unlock()
	if inner != nil {
		inner.Emit(e)
	}
	m.mu.Lock()
	if m.binding != nil && !m.returned.Load() {
		m.queue.Push(eventwire.ToWire(e))
	}
	wake := m.wake
	m.mu.Unlock()
	if wake != nil {
		select {
		case wake <- struct{}{}:
		default:
		}
	}
}

func (m *cliTakeoverManager) EmitChecked(e event.Event) error {
	m.mu.Lock()
	inner := m.inner
	m.mu.Unlock()
	if checked, ok := inner.(event.CheckedSink); ok {
		if err := checked.EmitChecked(e); err != nil {
			return err
		}
	} else if inner != nil {
		inner.Emit(e)
	}
	m.mu.Lock()
	if m.binding != nil && !m.returned.Load() {
		m.queue.Push(eventwire.ToWire(e))
	}
	wake := m.wake
	m.mu.Unlock()
	if wake != nil {
		select {
		case wake <- struct{}{}:
		default:
		}
	}
	return nil
}

func (m *cliTakeoverManager) AttachController(ctrl control.SessionAPI) {
	if m == nil {
		return
	}
	m.mu.Lock()
	m.ctrl = ctrl
	m.mu.Unlock()
}

func (m *cliTakeoverManager) Activate(binding *cliTakeoverBinding) {
	if m == nil || binding == nil {
		return
	}
	m.returnMu.Lock()
	defer m.returnMu.Unlock()
	m.sendMu.Lock()
	defer m.sendMu.Unlock()
	m.mu.Lock()
	m.binding = binding
	m.revision++
	m.failures = 0
	m.returned.Store(false)
	m.reclaiming.Store(false)
	m.ensureStartedLocked()
	m.mu.Unlock()
}

func (m *cliTakeoverManager) ensureStartedLocked() {
	if m.started {
		return
	}
	m.started = true
	m.wake = make(chan struct{}, 1)
	m.stop = make(chan struct{})
	m.done = make(chan struct{})
	go m.run()
}

func (m *cliTakeoverManager) SetYieldCallback(fn func()) {
	if m == nil {
		return
	}
	m.mu.Lock()
	m.onYield = fn
	m.mu.Unlock()
}

func (m *cliTakeoverManager) Reclaiming() bool { return m != nil && m.reclaiming.Load() }
func (m *cliTakeoverManager) Returned() bool   { return m != nil && m.returned.Load() }

func (m *cliTakeoverManager) snapshot() (*cliTakeoverBinding, control.SessionAPI, func(), uint64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.binding, m.ctrl, m.onYield, m.revision
}

func (m *cliTakeoverManager) run() {
	m.mu.Lock()
	wake, stop, done := m.wake, m.stop, m.done
	m.mu.Unlock()
	defer close(done)
	timer := time.NewTimer(time.Hour)
	if !timer.Stop() {
		<-timer.C
	}
	defer timer.Stop()
	heartbeat := time.NewTicker(cliTakeoverHeartbeat)
	defer heartbeat.Stop()
	retry := time.NewTicker(250 * time.Millisecond)
	defer retry.Stop()
	armed := false
	for {
		select {
		case <-stop:
			m.push(false)
			return
		case <-wake:
			if !armed {
				timer.Reset(cliTakeoverFlushEvery)
				armed = true
			}
		case <-timer.C:
			armed = false
			if !m.push(false) {
				return
			}
		case <-heartbeat.C:
			if !m.push(true) {
				return
			}
		case <-retry.C:
			m.retryPendingReturns(false)
		}
	}
}

func (m *cliTakeoverManager) drain() []eventwire.Event {
	m.mu.Lock()
	frames := m.queue.Take(cliTakeoverMaxFrames)
	m.mu.Unlock()
	return frames
}

func (m *cliTakeoverManager) requeue(frames []eventwire.Event) {
	if len(frames) == 0 {
		return
	}
	m.mu.Lock()
	m.queue.Prepend(frames)
	m.mu.Unlock()
}

func (m *cliTakeoverManager) wakeIfQueued() {
	m.mu.Lock()
	pending, wake := m.queue.Len() > 0, m.wake
	m.mu.Unlock()
	if pending && wake != nil {
		select {
		case wake <- struct{}{}:
		default:
		}
	}
}

func (m *cliTakeoverManager) push(heartbeat bool) bool {
	m.sendMu.Lock()
	defer m.sendMu.Unlock()
	return m.pushLocked(heartbeat)
}

func (m *cliTakeoverManager) pushLocked(heartbeat bool) bool {
	if m.returned.Load() {
		return false
	}
	binding, _, _, revision := m.snapshot()
	if binding == nil || binding.client == nil || binding.grant.MirrorID == "" {
		return true
	}
	frames := m.drain()
	if len(frames) == 0 && !heartbeat {
		return true
	}
	marshal := func(batch []eventwire.Event) ([]byte, error) {
		return json.Marshal(map[string]any{
			"sessionPath": binding.path, "mirrorId": binding.grant.MirrorID, "frames": batch,
		})
	}
	batch, remainder, payload, marshalErr := eventwire.MarshalMirrorBatch(frames, eventwire.MirrorBatchMaxBytes, marshal)
	if marshalErr == nil && len(batch) == 0 && len(frames) > 0 && len(remainder) > 0 {
		remainder = remainder[1:]
	}
	m.requeue(remainder)
	if marshalErr != nil {
		m.requeue(batch)
		return true
	}
	if len(batch) == 0 && len(frames) > 0 {
		// A single frame larger than the HTTP protocol permits cannot ever be
		// delivered. Durable history remains authoritative for its content.
		m.wakeIfQueued()
		if !heartbeat {
			return true
		}
		payload, _ = marshal(nil)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, binding.record.base+"/external/frames", bytes.NewReader(payload))
	if err == nil {
		req.Header.Set("Content-Type", "application/json")
	}
	var resp *http.Response
	if err == nil {
		resp, err = binding.client.Do(req)
	}
	if err != nil {
		cancel()
		if !m.bindingCurrent(binding, revision) {
			return true
		}
		m.requeue(batch)
		return m.readoptLocked(binding, revision)
	}
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
	resp.Body.Close()
	cancel()
	if !m.bindingCurrent(binding, revision) {
		return true
	}
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusConflict {
		m.requeue(batch)
		return m.readoptLocked(binding, revision)
	}
	if resp.StatusCode != http.StatusOK {
		m.requeue(batch)
		m.mu.Lock()
		if m.binding == binding && m.revision == revision {
			m.failures++
		}
		failures := m.failures
		m.mu.Unlock()
		if failures >= cliTakeoverRediscoverFailures {
			return m.readoptLocked(binding, revision)
		}
		return true
	}
	m.mu.Lock()
	if m.binding == binding && m.revision == revision {
		m.failures = 0
	}
	m.mu.Unlock()
	var out struct {
		ReclaimRequested bool   `json:"reclaimRequested"`
		ReclaimMode      string `json:"reclaimMode"`
	}
	if json.Unmarshal(body, &out) == nil && out.ReclaimRequested {
		m.requestYieldFor(binding, revision, out.ReclaimMode == "interrupt")
		return true
	}
	m.wakeIfQueued()
	return true
}

func (m *cliTakeoverManager) bindingCurrent(binding *cliTakeoverBinding, revision uint64) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.binding == binding && m.revision == revision && !m.returned.Load()
}

// readoptLocked discovers the current Serve endpoint and rotates the mirror
// generation. sendMu is held by the caller, so a binding switch cannot race a
// late response from the endpoint being replaced.
func (m *cliTakeoverManager) readoptLocked(binding *cliTakeoverBinding, revision uint64) bool {
	if binding == nil || !m.bindingCurrent(binding, revision) {
		return false
	}
	conflicted := false
	for _, record := range discoverCLIServesForTakeover() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		client, err := cliServeClient(ctx, record)
		if err != nil {
			cancel()
			continue
		}
		payload, _ := json.Marshal(map[string]string{"sessionPath": binding.path, "writerId": agent.SessionWriterID()})
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, record.base+"/adopt", bytes.NewReader(payload))
		if err == nil {
			req.Header.Set("Content-Type", "application/json")
		}
		var resp *http.Response
		if err == nil {
			resp, err = client.Do(req)
		}
		if err != nil {
			cancel()
			continue
		}
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
		resp.Body.Close()
		cancel()
		if resp.StatusCode == http.StatusConflict {
			conflicted = true
			continue
		}
		var grant cliTakeoverGrant
		if resp.StatusCode == http.StatusOK && json.Unmarshal(body, &grant) == nil &&
			grant.MirrorID != "" && grant.ReturnHandoffID != "" && grant.SourceWriterID != "" &&
			grant.TargetWriterID == agent.SessionWriterID() &&
			agent.CanonicalSessionPath(grant.SessionPath) == agent.CanonicalSessionPath(binding.path) {
			m.mu.Lock()
			if m.binding == binding && m.revision == revision && !m.returned.Load() {
				m.binding = &cliTakeoverBinding{path: binding.path, record: record, client: client, grant: grant}
				m.revision++
				m.failures = 0
			}
			m.mu.Unlock()
			return true
		}
	}
	if conflicted {
		m.requestYieldFor(binding, revision, false)
	}
	return true
}

func (m *cliTakeoverManager) requestYieldFor(binding *cliTakeoverBinding, revision uint64, interrupt bool) {
	if !m.bindingCurrent(binding, revision) {
		return
	}
	if !m.reclaiming.CompareAndSwap(false, true) {
		return
	}
	current, ctrl, callback, currentRevision := m.snapshot()
	if current != binding || currentRevision != revision {
		m.reclaiming.Store(false)
		return
	}
	if interrupt && ctrl != nil {
		ctrl.Cancel()
	}
	go func() {
		deadline := time.Now().Add(cliTakeoverTimeout)
		for cliControllerHasActiveRuntimeWork(ctrl) && time.Now().Before(deadline) {
			time.Sleep(50 * time.Millisecond)
		}
		if cliControllerHasActiveRuntimeWork(ctrl) {
			m.reclaiming.Store(false)
			return
		}
		if err := m.returnLeaseFor(binding, revision); err != nil {
			m.reclaiming.Store(false)
			return
		}
		if callback != nil {
			callback()
		}
	}()
}

func (m *cliTakeoverManager) returnLease() error {
	return m.returnLeaseFor(nil, 0)
}

func (m *cliTakeoverManager) returnLeaseFor(expected *cliTakeoverBinding, revision uint64) error {
	expectedPath := ""
	if expected != nil {
		if !m.bindingCurrent(expected, revision) {
			current, _, _, _ := m.snapshot()
			if current == nil || agent.CanonicalSessionPath(current.path) != agent.CanonicalSessionPath(expected.path) {
				return fmt.Errorf("takeover mirror changed before reclaim completed")
			}
		}
		expectedPath = expected.path
	}
	return m.returnMirrorTransaction(expectedPath, true, true, func(current *cliTakeoverBinding) error {
		return m.leases.ReleaseForHandoff(current.grant.SourceWriterID, current.grant.ReturnHandoffID)
	})
}

// RebindAway acquires a new ordinary session before returning the mirrored
// one. It lets /resume and related TUI switches keep their original failure
// atomicity while still honoring Serve's reverse reservation.
func (m *cliTakeoverManager) RebindAway(path string) (bool, error) {
	if m == nil {
		return false, nil
	}
	binding, _, _, _ := m.snapshot()
	if binding == nil || m.returned.Load() || agent.CanonicalSessionPath(binding.path) == agent.CanonicalSessionPath(path) {
		return false, nil
	}
	err := m.returnCurrentMirror(binding.path, func(current *cliTakeoverBinding) error {
		return m.leases.RebindReturningCurrent(path, current.grant.SourceWriterID, current.grant.ReturnHandoffID)
	})
	return true, err
}

// cliAcquireFreeSession starts an ordinary failure-atomic switch. The newly
// acquired target stays in leases while the source binding remains detached
// and live until the caller has loaded and authorized the candidate session.
func cliAcquireFreeSession(path string, leases *control.SessionLeaseKeeper, manager *cliTakeoverManager) (*cliTakeoverBinding, error) {
	if leases == nil {
		return &cliTakeoverBinding{path: path}, nil
	}
	if manager != nil && manager.Reclaiming() {
		return nil, fmt.Errorf("the remote side is reclaiming the current session")
	}
	binding := &cliTakeoverBinding{path: path}
	if manager != nil {
		current, _, _, _ := manager.snapshot()
		if current != nil && !manager.Returned() && agent.CanonicalSessionPath(current.path) != agent.CanonicalSessionPath(path) {
			binding.priorMirror = current
		}
	}
	previous, err := leases.RebindDetaching(path)
	if err != nil {
		return nil, err
	}
	binding.previous = previous
	return binding, nil
}

// cliPrepareTakeoverCandidate reloads after acquisition. For a Serve handoff,
// this observes the Snapshot completed by /handoff rather than the stale
// preflight view. Authority is bound to the private candidate before the
// controller publishes it through Resume.
func cliPrepareTakeoverCandidate(binding *cliTakeoverBinding, leases *control.SessionLeaseKeeper) (*agent.Session, error) {
	if binding == nil {
		return nil, fmt.Errorf("takeover binding unavailable")
	}
	loaded, err := loadResumableSession(binding.path)
	if err != nil {
		return nil, err
	}
	if leases != nil {
		if err := leases.BindSessionAuthority(loaded); err != nil {
			return nil, err
		}
	}
	return loaded, nil
}

// commitPrevious retires the source keeper only after the handed-off target
// has been loaded successfully. A mirrored source is returned through its
// reverse reservation; an ordinary source is simply released.
func (b *cliTakeoverBinding) commitPrevious(manager *cliTakeoverManager) error {
	if b == nil || b.previous == nil {
		return nil
	}
	if b.priorMirror == nil {
		b.previous.RetireDetached()
		b.previous = nil
		return nil
	}
	if manager == nil {
		return fmt.Errorf("takeover manager unavailable for mirrored source")
	}
	return manager.commitPriorMirror(b)
}

func (m *cliTakeoverManager) commitPriorMirror(next *cliTakeoverBinding) error {
	if m == nil || next == nil || next.previous == nil || next.priorMirror == nil {
		return nil
	}
	err := m.returnCurrentMirror(next.priorMirror.path, func(current *cliTakeoverBinding) error {
		return next.previous.RetireDetachedForHandoff(current.grant.SourceWriterID, current.grant.ReturnHandoffID)
	})
	if err == nil {
		next.previous = nil
	}
	return err
}

func (m *cliTakeoverManager) mirrorEnd(binding *cliTakeoverBinding) {
	if m == nil {
		return
	}
	m.sendMu.Lock()
	defer m.sendMu.Unlock()
	m.mirrorEndLocked(binding)
}

func (m *cliTakeoverManager) mirrorEndLocked(binding *cliTakeoverBinding) {
	if binding == nil || binding.client == nil {
		return
	}
	payload, _ := json.Marshal(map[string]string{"sessionPath": binding.path, "mirrorId": binding.grant.MirrorID})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, binding.record.base+"/mirror-end", bytes.NewReader(payload))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := binding.client.Do(req)
	if err == nil {
		_, _ = io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
	}
}

func (m *cliTakeoverManager) holdPendingReturn(keeper *control.SessionLeaseKeeper, binding *cliTakeoverBinding) {
	if m == nil || keeper == nil || binding == nil {
		return
	}
	m.mu.Lock()
	m.pending = append(m.pending, &cliPendingReturn{
		keeper: keeper, binding: binding, nextTry: time.Now().Add(200 * time.Millisecond), backoff: 200 * time.Millisecond,
	})
	m.ensureStartedLocked()
	wake := m.wake
	m.mu.Unlock()
	if wake != nil {
		select {
		case wake <- struct{}{}:
		default:
		}
	}
}

func (m *cliTakeoverManager) retryPendingReturns(force bool) {
	if m == nil {
		return
	}
	if force {
		m.returnMu.Lock()
	} else if !m.returnMu.TryLock() {
		// The active mirror return transaction owns the forwarding loop. Do not
		// strand that transaction while it joins this loop; the next retry tick
		// will pick these detached keepers up.
		return
	}
	defer m.returnMu.Unlock()
	now := time.Now()
	m.mu.Lock()
	pending := append([]*cliPendingReturn(nil), m.pending...)
	m.mu.Unlock()
	for _, item := range pending {
		if item == nil || item.keeper == nil || item.binding == nil || (!force && now.Before(item.nextTry)) {
			continue
		}
		var err error
		if m.retirePending != nil {
			err = m.retirePending(item.keeper, item.binding.grant.SourceWriterID, item.binding.grant.ReturnHandoffID)
		} else {
			err = item.keeper.RetireDetachedForHandoff(item.binding.grant.SourceWriterID, item.binding.grant.ReturnHandoffID)
		}
		if err == nil {
			m.mirrorEnd(item.binding)
			m.mu.Lock()
			for i, candidate := range m.pending {
				if candidate == item {
					m.pending = append(m.pending[:i], m.pending[i+1:]...)
					break
				}
			}
			m.mu.Unlock()
			continue
		}
		m.mu.Lock()
		item.backoff = min(item.backoff*2, 5*time.Second)
		item.nextTry = now.Add(item.backoff)
		m.mu.Unlock()
	}
}

// cliReturnFailedTakeover restores the source binding after a candidate load
// or commit failure. A failed reverse-reservation write leaves the target in a
// manager-owned detached keeper; mirror-end is withheld until a retry succeeds.
func cliReturnFailedTakeover(binding *cliTakeoverBinding, leases *control.SessionLeaseKeeper, manager *cliTakeoverManager) error {
	if binding == nil || leases == nil {
		return nil
	}
	if binding.grant.ReturnHandoffID != "" && binding.grant.SourceWriterID != "" {
		var pending *control.SessionLeaseKeeper
		var err error
		if binding.previous != nil {
			pending, err = leases.RestoreDetachedReturningCurrent(
				binding.previous, binding.grant.SourceWriterID, binding.grant.ReturnHandoffID,
			)
			binding.previous = nil
		} else {
			pending = leases.Split()
			if pending == nil {
				return fmt.Errorf("failed takeover target lease is unavailable")
			}
			err = pending.RetireDetachedForHandoff(binding.grant.SourceWriterID, binding.grant.ReturnHandoffID)
			if err == nil {
				pending = nil
			}
		}
		if err != nil {
			if manager == nil || pending == nil {
				return fmt.Errorf("return failed takeover lease: %w", err)
			}
			manager.holdPendingReturn(pending, binding)
			return fmt.Errorf("return failed takeover lease (retrying): %w", err)
		}
	} else {
		current := leases.Split()
		if binding.previous != nil {
			leases.Adopt(binding.previous)
			binding.previous = nil
		}
		if current != nil {
			current.RetireDetached()
		}
	}
	if binding.grant.MirrorID == "" {
		return nil
	}
	if manager != nil {
		manager.mirrorEnd(binding)
	} else {
		(&cliTakeoverManager{}).mirrorEnd(binding)
	}
	return nil
}

func cliEndFailedHandoff(binding *cliTakeoverBinding) {
	if binding == nil {
		return
	}
	m := &cliTakeoverManager{}
	m.mirrorEnd(binding)
}

// Close returns an active mirrored session on ordinary CLI exit. A concurrent
// reclaim owns the same transaction; wait for it rather than publishing a
// second reservation.
func (m *cliTakeoverManager) Close() error {
	if m == nil {
		return nil
	}
	if !m.closed.CompareAndSwap(false, true) {
		return nil
	}
	var closeErr error
	if m.reclaiming.Load() {
		_, ctrl, _, _ := m.snapshot()
		deadline := time.Now().Add(cliTakeoverTimeout)
		for cliControllerHasActiveRuntimeWork(ctrl) && time.Now().Before(deadline) {
			time.Sleep(50 * time.Millisecond)
		}
		if cliControllerHasActiveRuntimeWork(ctrl) {
			return fmt.Errorf("timed out waiting for the active turn to yield its session")
		}
	}
	if err := m.returnLease(); err != nil {
		closeErr = err
	}
	m.mu.Lock()
	started, stop, done := m.started, m.stop, m.done
	m.mu.Unlock()
	if started {
		m.stopOnce.Do(func() { close(stop) })
		<-done
	}
	m.retryPendingReturns(true)
	m.mu.Lock()
	remaining := append([]*cliPendingReturn(nil), m.pending...)
	m.pending = nil
	m.mu.Unlock()
	for _, item := range remaining {
		if item != nil && item.keeper != nil {
			// Close is the final CLI teardown. Keep the target fenced until this
			// point, then let process cleanup release an unreturnable OS lease.
			item.keeper.Release()
		}
	}
	if len(remaining) > 0 && closeErr == nil {
		closeErr = fmt.Errorf("unable to publish %d pending session return reservation(s)", len(remaining))
	}
	return closeErr
}
