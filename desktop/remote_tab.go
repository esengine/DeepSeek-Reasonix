package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// The remote-tab bridge exchanges its pre-shared token for an HttpOnly
// session cookie over the loopback tunnel. Subsequent API and SSE requests
// use that cookie, keeping the token out of request lines and access logs.

const remoteTabStreamOpenStability = 50 * time.Millisecond

func remoteSessionTransitionBusy(err error) bool {
	if err == nil {
		return false
	}
	message := err.Error()
	return strings.Contains(message, "while a turn is running") ||
		strings.Contains(message, "while another session change is in progress") ||
		strings.Contains(message, "session is finishing background teardown")
}

// attachRemoteTabServe starts the event pump before entering the session so
// /new or /resume frames are not missed. The caller's context owns the pump;
// handshake and session entry use a bounded child context.
func (a *App) attachRemoteTabServe(ctx context.Context, tabID, base, token, instanceID string, opts RemoteTabOpenOptions) (bool, error) {
	callCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	client, err := newServeHTTPClient(base)
	if err != nil {
		return false, err
	}
	if err := serveHandshake(callCtx, client, base, token); err != nil {
		log.Printf("[remote] attachRemoteTabServe: handshake FAILED tab=%s base=%q err=%v", tabID, base, err)
		return false, err
	}
	a.remoteTabMu.Lock()
	tab := a.remoteTabs[tabID]
	a.remoteTabMu.Unlock()
	if tab == nil {
		return false, fmt.Errorf("remote tab %q closed during bootstrap", tabID)
	}
	tab.sessionMu.Lock()
	defer tab.sessionMu.Unlock()

	// Resolve every non-new target before opening the all-session pump. A
	// detached controller may replay pending prompts as soon as /resume starts;
	// publishing its route first keeps those frames on the foreground surface.
	focusOnly := !opts.NewSession && strings.TrimSpace(opts.SessionName) == "" && strings.TrimSpace(opts.SessionPath) == ""
	var target serveSessionEntry
	if !opts.NewSession {
		target, err = preflightRemoteSessionTarget(callCtx, client, base, opts)
		if err != nil {
			return false, err
		}
	}

	a.remoteTabMu.Lock()
	if a.remoteTabs[tabID] != tab {
		a.remoteTabMu.Unlock()
		return false, fmt.Errorf("remote tab %q closed during bootstrap", tabID)
	}
	// Retire any pump installed by a concurrent reconnect so exactly one
	// generation owns the event stream.
	tab.gen++
	if tab.cancel != nil {
		tab.cancel()
	}
	tab.client = client
	tab.base = base
	tab.token = token
	if !opts.NewSession {
		commitRemoteTabAttachRoute(tab, target.Path, false)
	}
	attachPathRevision := tab.routing.pathRevision
	gen := tab.gen
	pumpCtx, cancelPump := context.WithCancel(ctx)
	tab.cancel = cancelPump
	a.remoteTabMu.Unlock()

	opened := make(chan error, 1)
	a.goRemoteTabSafe("remoteTabPump", func() { a.remoteTabPump(pumpCtx, tabID, gen, opened) })
	select {
	case err = <-opened:
		if err != nil {
			a.retireRemoteTabGeneration(tabID, gen)
			return false, err
		}
	case <-callCtx.Done():
		a.retireRemoteTabGeneration(tabID, gen)
		return false, callCtx.Err()
	}
	entered := true
	if !focusOnly {
		enterOpts := opts
		if !opts.NewSession {
			enterOpts.SessionName, enterOpts.SessionPath, enterOpts.SessionTitle = target.Name, target.Path, target.Title
		}
		target, err = enterRemoteSessionTarget(callCtx, client, base, enterOpts)
		entered = err == nil
	}
	if err != nil {
		// A busy serve refuses session transitions with 409 but retains its
		// usable current session. Keep the attach so pending work remains visible.
		if remoteSessionTransitionBusy(err) {
			log.Printf("[remote] attachRemoteTabServe: enterRemoteSession BUSY (attached to current session) tab=%s err=%v", tabID, err)
			entered = false
			target, _ = serveCurrentSession(callCtx, client, base)
		} else {
			log.Printf("[remote] attachRemoteTabServe: enterRemoteSession FAILED tab=%s err=%v", tabID, err)
			a.retireRemoteTabGeneration(tabID, gen)
			return false, err
		}
	}
	if !a.commitRemoteTabAttachResponse(tabID, tab, gen, attachPathRevision, target, opts.NewSession) {
		entered = false
	}
	if !a.waitRemoteTabStreamStable(callCtx, tabID, gen) {
		return false, fmt.Errorf("remote tab %q event stream closed during session attach", tabID)
	}
	a.remoteTabMu.Lock()
	if current := a.remoteTabs[tabID]; current == tab && current.gen == gen {
		current.session.instanceID = instanceID
	}
	a.remoteTabMu.Unlock()
	// A 200 response is only the stream-open barrier. The stream can still die
	// while /new or /resume is in flight; publish readiness only if its pump has
	// not already moved this same generation into reconnecting/error.
	if !a.markRemoteTabAttached(tabID, gen) {
		return false, fmt.Errorf("remote tab %q event stream closed during session attach", tabID)
	}
	return entered, nil
}

// commitRemoteTabAttachResponse applies an attach response only while it still
// owns the foreground route. A session_changed frame for a newer adoption is
// authoritative even when the older /new or /resume response arrives later.
func (a *App) commitRemoteTabAttachResponse(tabID string, tab *remoteTab, gen, requestPathRevision uint64, target serveSessionEntry, reset bool) bool {
	tab.routeEventMu.Lock()
	defer tab.routeEventMu.Unlock()
	a.remoteTabMu.Lock()
	defer a.remoteTabMu.Unlock()
	current := a.remoteTabs[tabID]
	if current != tab || current.gen != gen {
		return false
	}
	target.Path = strings.TrimSpace(target.Path)
	if current.routing.pathRevision != requestPathRevision && current.routing.currentPath != target.Path {
		return false
	}
	alreadyAdopted := current.routing.pathRevision != requestPathRevision
	if !alreadyAdopted {
		commitRemoteTabAttachRoute(current, target.Path, reset)
	}
	if name := strings.TrimSpace(target.Name); name != "" {
		current.session.name = name
	}
	if title := strings.TrimSpace(target.Title); title != "" {
		current.topicTitle = title
	}
	if current.routing.running == nil {
		current.routing.running = map[string]bool{}
	}
	return true
}

func (a *App) waitRemoteTabStreamStable(ctx context.Context, tabID string, gen uint64) bool {
	timer := time.NewTimer(remoteTabStreamOpenStability)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return a.remoteTabGenerationCurrent(tabID, gen)
	}
}

func (a *App) markRemoteTabAttached(tabID string, gen uint64) bool {
	a.remoteTabMu.Lock()
	defer a.remoteTabMu.Unlock()
	tab := a.remoteTabs[tabID]
	if tab == nil || tab.gen != gen || tab.state != "connecting" {
		return false
	}
	tab.attachedGen = gen
	return true
}

func (a *App) publishRemoteTabAttachedReady(tabID string, gen uint64) bool {
	a.remoteTabMu.Lock()
	tab := a.remoteTabs[tabID]
	if tab == nil || tab.gen != gen || tab.attachedGen != gen || tab.state != "connecting" {
		a.remoteTabMu.Unlock()
		return false
	}
	tab.attachedGen = 0
	tab.state = "ready"
	tab.err = ""
	a.remoteTabMu.Unlock()
	a.emitRemoteEvent(fmt.Sprintf("remote-tab:%s:state", tabID), RemoteTabStateView{State: "ready"})
	a.applyPendingRemoteTabOpenSelection(tabID)
	return true
}

func (a *App) remoteTabGenerationCurrent(tabID string, gen uint64) bool {
	a.remoteTabMu.Lock()
	defer a.remoteTabMu.Unlock()
	tab := a.remoteTabs[tabID]
	return tab != nil && tab.gen == gen
}

func (a *App) retireRemoteTabGeneration(tabID string, gen uint64) {
	a.remoteTabMu.Lock()
	tab := a.remoteTabs[tabID]
	if tab == nil || tab.gen != gen {
		a.remoteTabMu.Unlock()
		return
	}
	cancel := tab.cancel
	tab.gen++
	tab.attachedGen = 0
	tab.cancel = nil
	tab.client = nil
	tab.base = ""
	tab.token = ""
	a.remoteTabMu.Unlock()
	if cancel != nil {
		cancel()
	}
}

// reconnectRemoteTabGeneration retires a dead pump and atomically parks its
// tab in reconnecting. The bool reports whether this pump should start the
// retry loop; a pump opened by an existing retry loop leaves retries to its
// caller so two loops cannot race each other.
func (a *App) reconnectRemoteTabGeneration(tabID string, gen uint64) bool {
	a.remoteTabMu.Lock()
	tab := a.remoteTabs[tabID]
	if tab == nil || tab.gen != gen {
		a.remoteTabMu.Unlock()
		return false
	}
	startRetry := tab.state != "reconnecting"
	cancel := tab.cancel
	tab.gen++
	tab.attachedGen = 0
	tab.cancel = nil
	tab.client = nil
	tab.base = ""
	tab.token = ""
	tab.state = "reconnecting"
	tab.err = ""
	a.remoteTabMu.Unlock()
	if cancel != nil {
		cancel()
	}
	a.emitRemoteEvent(fmt.Sprintf("remote-tab:%s:state", tabID), RemoteTabStateView{State: "reconnecting"})
	return startRetry
}

func (a *App) emitRemoteTabStateForGeneration(tabID string, gen uint64, state, errMsg string) bool {
	a.remoteTabMu.Lock()
	tab := a.remoteTabs[tabID]
	if tab == nil || tab.gen != gen {
		a.remoteTabMu.Unlock()
		return false
	}
	tab.state = state
	tab.err = errMsg
	a.remoteTabMu.Unlock()
	a.emitRemoteEvent(fmt.Sprintf("remote-tab:%s:state", tabID), RemoteTabStateView{State: state, Error: errMsg})
	return true
}

func (a *App) transitionRemoteTabState(tabID string, gen uint64, from, state, errMsg string) bool {
	a.remoteTabMu.Lock()
	tab := a.remoteTabs[tabID]
	if tab == nil || tab.gen != gen || tab.state != from {
		a.remoteTabMu.Unlock()
		return false
	}
	tab.state = state
	tab.err = errMsg
	a.remoteTabMu.Unlock()
	a.emitRemoteEvent(fmt.Sprintf("remote-tab:%s:state", tabID), RemoteTabStateView{State: state, Error: errMsg})
	return true
}

// remoteTabPump forwards Serve events for one tab generation. Cancellation,
// stream death, or a generation mismatch retires the pump.
func (a *App) remoteTabPump(ctx context.Context, tabID string, gen uint64, opened chan<- error) {
	signalOpened := func(err error) {
		if opened == nil {
			return
		}
		select {
		case opened <- err:
		default:
		}
		opened = nil
	}
	a.remoteTabMu.Lock()
	tab := a.remoteTabs[tabID]
	var client *http.Client
	var base string
	if tab != nil && tab.gen == gen {
		client, base = tab.client, tab.base
	}
	a.remoteTabMu.Unlock()
	if client == nil || base == "" {
		if opened != nil {
			opened <- fmt.Errorf("remote tab %q event stream was retired before opening", tabID)
		}
		return
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, serveURL(base, "/events?all=1"), nil)
	if err != nil {
		signalOpened(err)
		a.emitRemoteTabStateForGeneration(tabID, gen, "error", err.Error())
		return
	}
	req.Header.Set("Accept", "text/event-stream")
	resp, err := client.Do(req)
	if err != nil {
		signalOpened(err)
		if ctx.Err() == nil {
			log.Printf("[remote] remoteTabPump: /events DO-FAILED tab=%s err=%v", tabID, err)
			a.emitRemoteTabStateForGeneration(tabID, gen, "error", err.Error())
		}
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		err = fmt.Errorf("serve /events: status %d", resp.StatusCode)
		signalOpened(err)
		log.Printf("[remote] remoteTabPump: /events BAD-STATUS tab=%s status=%d", tabID, resp.StatusCode)
		a.emitRemoteTabStateForGeneration(tabID, gen, "error", err.Error())
		return
	}
	signalOpened(nil)
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 64<<10), serveEventMaxBytes)
	for scanner.Scan() {
		line := scanner.Text()
		line = strings.TrimRight(line, "\r\n")
		if !strings.HasPrefix(line, "data:") {
			continue // ": ping" keepalives and other SSE fields
		}
		frame := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if frame == "" {
			continue
		}
		if !a.remoteTabGenerationCurrent(tabID, gen) {
			return
		}
		kind, framePath, current, reset := probeRemoteTabFrame(frame)
		if !a.routeRemoteTabWireFrame(tabID, gen, framePath, kind, current, reset) {
			continue
		}
		if a.bufferRemoteTabResumeFrame(tabID, gen, framePath, kind, json.RawMessage(frame)) {
			continue
		}
		a.publishRemoteTabFrame(tabID, gen, framePath, kind, json.RawMessage(frame))
	}
	if err := scanner.Err(); err != nil {
		log.Printf("[remote] remoteTabPump: READ-EXIT tab=%s gen=%d err=%v ctxErr=%v", tabID, gen, err, ctx.Err())
	}
	// Only the current generation reacts to an unexpected stream death.
	// Reattach now; the host status hook also retries on connection recovery.
	if ctx.Err() == nil {
		if startRetry := a.reconnectRemoteTabGeneration(tabID, gen); startRetry {
			a.goRemoteTabSafe("remoteTabReattach", func() { a.reattachRemoteTab(tabID) })
		}
	}
}

func (a *App) completeRemoteTabTurn(tabID string, gen uint64) {
	a.remoteTabMu.Lock()
	tab := a.remoteTabs[tabID]
	if tab == nil || tab.gen != gen {
		a.remoteTabMu.Unlock()
		return
	}
	tab.runtime.revision++
	tab.pendingEvents = nil
	tab.runtime.running = false
	tab.runtime.turnStartedAt = 0
	tab.runtime.pendingPrompt = false
	tab.runtime.cancelRequested = false
	tab.runtime.cancellable = tab.runtime.backgroundJobs > 0
	// A completed turn makes the fresh session non-blank even when the
	// best-effort /sessions title lookup fails. New Topic must never reuse
	// a conversation that already has a completed turn.
	tab.session.reset = false
	meta := remoteTabMetaLocked(tab)
	a.remoteTabMu.Unlock()
	a.emitRemoteEvent("remote-tab:updated", meta)
}

func (a *App) recordRemoteTabTurnStarted(tabID string, gen uint64, frame json.RawMessage) {
	var payload struct {
		TurnStartedAt int64 `json:"turnStartedAt"`
	}
	_ = json.Unmarshal(frame, &payload)
	if payload.TurnStartedAt <= 0 {
		payload.TurnStartedAt = time.Now().UnixMilli()
	}
	a.remoteTabMu.Lock()
	tab := a.remoteTabs[tabID]
	if tab == nil || tab.gen != gen {
		a.remoteTabMu.Unlock()
		return
	}
	tab.runtime.revision++
	tab.runtime.running = true
	tab.runtime.turnStartedAt = payload.TurnStartedAt
	tab.runtime.pendingPrompt = false
	tab.runtime.cancelRequested = false
	tab.runtime.cancellable = true
	meta := remoteTabMetaLocked(tab)
	a.remoteTabMu.Unlock()
	a.emitRemoteEvent("remote-tab:updated", meta)
}

func (a *App) cacheRemotePendingEvent(tabID string, gen uint64, kind string, frame json.RawMessage) {
	key := remotePendingEventKey(kind, frame)
	a.remoteTabMu.Lock()
	tab := a.remoteTabs[tabID]
	if tab == nil || tab.gen != gen {
		a.remoteTabMu.Unlock()
		return
	}
	tab.runtime.revision++
	if tab.pendingEvents == nil {
		tab.pendingEvents = make(map[string]json.RawMessage)
	}
	tab.pendingEvents[key] = append(json.RawMessage(nil), frame...)
	tab.runtime.pendingPrompt = true
	tab.runtime.cancellable = true
	meta := remoteTabMetaLocked(tab)
	a.remoteTabMu.Unlock()
	a.emitRemoteEvent("remote-tab:updated", meta)
}

func (a *App) clearRemotePendingEvent(tabID, kind, callID string) {
	a.remoteTabMu.Lock()
	var meta TabMeta
	changed := false
	if tab := a.remoteTabs[tabID]; tab != nil {
		tab.runtime.revision++
		delete(tab.pendingEvents, kind+":"+strings.TrimSpace(callID))
		pending := len(tab.pendingEvents) > 0
		changed = tab.runtime.pendingPrompt != pending
		tab.runtime.pendingPrompt = pending
		meta = remoteTabMetaLocked(tab)
	}
	a.remoteTabMu.Unlock()
	if changed {
		a.emitRemoteEvent("remote-tab:updated", meta)
	}
}

// serveGet fetches a JSON member of the tab snapshot, returning the raw
// payload for verbatim passthrough.
func serveGet(ctx context.Context, client *http.Client, url string) (json.RawMessage, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, serveSnapshotMaxBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > serveSnapshotMaxBytes {
		return nil, fmt.Errorf("%s: response exceeds %d bytes", url, serveSnapshotMaxBytes)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s: status %d", url, resp.StatusCode)
	}
	return json.RawMessage(data), nil
}

// commandContext bounds one proxied command. Boot context when available;
// the timeout keeps a wedged tunnel from hanging the binding call.
func commandContext(a *App) (context.Context, context.CancelFunc) {
	ctx := a.bootContext()
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithTimeout(ctx, 15*time.Second)
}

// remoteTabCommandClient resolves a tabID to its live serve client. A tab
// that has not finished bootstrap, is reconnecting, or has failed is an
// error, not a silent no-op.
func (a *App) remoteTabCommandClient(tabID string) (*http.Client, string, error) {
	client, base, _, err := a.remoteTabCommandTarget(tabID)
	return client, base, err
}

func (a *App) remoteTabCommandTarget(tabID string) (*http.Client, string, string, error) {
	a.remoteTabMu.Lock()
	tab := a.remoteTabs[tabID]
	var client *http.Client
	var base, expectedPath string
	usable := tab != nil && tab.client != nil && tab.state == "ready"
	if usable {
		client, base = tab.client, tab.base
		expectedPath = tab.routing.currentPath
	}
	a.remoteTabMu.Unlock()
	if !usable {
		log.Printf("[remote] remoteTabCommandClient: REFUSED tab=%q (tab=%v client=%v)", tabID, tab != nil, tab != nil && tab.client != nil)
		return nil, "", "", fmt.Errorf("remote tab %q is not connected", tabID)
	}
	return client, base, expectedPath, nil
}

func (a *App) isRemoteTab(tabID string) bool {
	if strings.TrimSpace(tabID) == "" {
		return false
	}
	a.remoteTabMu.Lock()
	_, ok := a.remoteTabs[tabID]
	a.remoteTabMu.Unlock()
	return ok
}

// remoteTabRefFor returns the host+workspace ref when tabID belongs to a
// remote tab; view builders use it to mark remote-shaped metas.
func (a *App) remoteTabRefFor(tabID string) (RemoteTabRef, bool) {
	a.remoteTabMu.Lock()
	defer a.remoteTabMu.Unlock()
	if tab := a.remoteTabs[tabID]; tab != nil {
		return tab.ref, true
	}
	return RemoteTabRef{}, false
}

func (a *App) remoteTabCurrentModel(tabID string) (string, bool) {
	if !a.isRemoteTab(tabID) {
		return "", false
	}
	a.remoteTabMu.Lock()
	tab := a.remoteTabs[tabID]
	cur := ""
	if tab != nil {
		cur = tab.model
	}
	a.remoteTabMu.Unlock()
	return cur, true
}

func (a *App) SubmitRemoteTab(tabID, text string) error {
	client, base, expectedPath, err := a.remoteTabCommandTarget(tabID)
	if err != nil {
		return err
	}
	ctx, cancel := commandContext(a)
	defer cancel()
	body, _ := json.Marshal(map[string]string{"input": text})
	started := time.Now()
	err = servePostForSession(ctx, client, serveURL(base, "/submit"), body, expectedPath)
	if err != nil {
		log.Printf("[remote] submit failed tab=%s dur=%s err=%v", tabID, time.Since(started).Round(time.Millisecond), err)
	}
	return err
}

func (a *App) CancelRemoteTab(tabID string) error {
	client, base, expectedPath, err := a.remoteTabCommandTarget(tabID)
	if err != nil {
		return err
	}
	ctx, cancel := commandContext(a)
	defer cancel()
	return servePostForSession(ctx, client, serveURL(base, "/cancel"), nil, expectedPath)
}

// ApproveRemoteTab answers a tool-approval request. Serve takes
// {id, allow, session, persist}; preserve the same once/session/persistent
// scopes exposed by the local approval surface.
func (a *App) ApproveRemoteTab(tabID, callID, decision string) error {
	client, base, expectedPath, err := a.remoteTabCommandTarget(tabID)
	if err != nil {
		return err
	}
	ctx, cancel := commandContext(a)
	defer cancel()
	decision = strings.ToLower(strings.TrimSpace(decision))
	allow, session, persist := false, false, false
	switch decision {
	case "allow", "once":
		allow = true
	case "session":
		allow, session = true, true
	case "persist", "persistent", "project":
		allow, session, persist = true, true, true
	case "deny":
	default:
		return fmt.Errorf("invalid remote approval decision %q", decision)
	}
	body, _ := json.Marshal(map[string]any{"id": callID, "allow": allow, "session": session, "persist": persist})
	if err := servePostForSession(ctx, client, serveURL(base, "/approve"), body, expectedPath); err != nil {
		return err
	}
	a.clearRemotePendingEvent(tabID, "approval_request", callID)
	return nil
}

// ResolveRemoteTabPlanDecision preserves the three distinct exit_plan_mode
// outcomes that the generic approval boolean cannot represent. Revision text
// travels in the same Serve request so the controller can durably stage it
// before resolving the approval; a tunnel failure can no longer split the
// decision from the requested revision.
func (a *App) ResolveRemoteTabPlanDecision(tabID, callID, action, feedback string) error {
	client, base, expectedPath, err := a.remoteTabCommandTarget(tabID)
	if err != nil {
		return err
	}
	action = strings.ToLower(strings.TrimSpace(action))
	switch action {
	case "start_execution", "revise_plan", "exit_plan":
	default:
		return fmt.Errorf("invalid remote plan decision %q", action)
	}
	ctx, cancel := commandContext(a)
	defer cancel()
	body, _ := json.Marshal(map[string]string{"id": callID, "action": action, "feedback": strings.TrimSpace(feedback)})
	if err := servePostForSession(ctx, client, serveURL(base, "/plan-decision"), body, expectedPath); err != nil {
		return err
	}
	a.clearRemotePendingEvent(tabID, "approval_request", callID)
	return nil
}

type RemoteAskAnswer struct {
	QuestionID string   `json:"QuestionID"`
	Selected   []string `json:"Selected"`
}

// AnswerRemoteTab preserves the batch ask id at the top level and sends every
// question's own id/selections in the Serve AskAnswer wire shape.
func (a *App) AnswerRemoteTab(tabID, callID string, answers []RemoteAskAnswer) error {
	client, base, expectedPath, err := a.remoteTabCommandTarget(tabID)
	if err != nil {
		return err
	}
	ctx, cancel := commandContext(a)
	defer cancel()
	body, _ := json.Marshal(map[string]any{
		"id":      callID,
		"answers": answers,
	})
	if err := servePostForSession(ctx, client, serveURL(base, "/answer"), body, expectedPath); err != nil {
		return err
	}
	a.clearRemotePendingEvent(tabID, "ask_request", callID)
	return nil
}

func (a *App) SubmitRemoteTabExtensionForm(tabID, pluginID, surfaceID string, values map[string]any) error {
	if err := a.remoteTabPost(tabID, "/extension-form", map[string]any{
		"pluginId": pluginID, "surfaceId": surfaceID, "values": values,
	}); err != nil {
		return err
	}
	a.clearRemotePendingExtensionForm(tabID, pluginID, surfaceID)
	return nil
}

// RewindRemoteTab rewinds to a checkpoint. Serve identifies checkpoints by
// TURN index and takes {turn, scope}; the checkpointID string is that turn.
func (a *App) RewindRemoteTab(tabID, checkpointID, scope string) error {
	client, base, expectedPath, err := a.remoteTabCommandTarget(tabID)
	if err != nil {
		return err
	}
	ctx, cancel := commandContext(a)
	defer cancel()
	turn, convErr := strconv.Atoi(strings.TrimSpace(checkpointID))
	if convErr != nil {
		return fmt.Errorf("invalid checkpoint id %q: want the turn index", checkpointID)
	}
	scope = strings.TrimSpace(scope)
	switch scope {
	case "code", "conversation", "both":
	default:
		return fmt.Errorf("invalid rewind scope %q", scope)
	}
	body, _ := json.Marshal(map[string]any{"turn": turn, "scope": scope})
	return servePostForSession(ctx, client, serveURL(base, "/rewind"), body, expectedPath)
}

func (a *App) SetRemoteTabToolApprovalMode(tabID, mode string) error {
	client, base, expectedPath, err := a.remoteTabCommandTarget(tabID)
	if err != nil {
		return err
	}
	ctx, cancel := commandContext(a)
	defer cancel()
	body, _ := json.Marshal(map[string]string{"mode": mode})
	return servePostForSession(ctx, client, serveURL(base, "/tool-approval-mode"), body, expectedPath)
}

func (a *App) SetRemoteTabComposerProfile(tabID, collaborationMode, toolApprovalMode, goal string) ([]string, error) {
	client, base, expectedPath, err := a.remoteTabCommandTarget(tabID)
	if err != nil {
		return nil, err
	}
	body, _ := json.Marshal(map[string]any{
		"collaborationMode": collaborationMode,
		"toolApprovalMode":  toolApprovalMode,
		"goal":              goal,
	})
	ctx, cancel := commandContext(a)
	defer cancel()
	resp, err := serveDoForSession(ctx, client, http.MethodPost, serveURL(base, "/composer-profile"), body, expectedPath)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<10))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		if message := strings.TrimSpace(string(data)); message != "" {
			return nil, fmt.Errorf("%s: status %d: %s", serveURL(base, "/composer-profile"), resp.StatusCode, message)
		}
		return nil, fmt.Errorf("%s: status %d", serveURL(base, "/composer-profile"), resp.StatusCode)
	}
	if len(bytes.TrimSpace(data)) == 0 {
		return []string{}, nil
	}
	var result struct {
		DrainedApprovalIDs []string `json:"drainedApprovalIDs"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("decode remote composer profile response: %w", err)
	}
	return result.DrainedApprovalIDs, nil
}

func (a *App) SetRemoteTabGoal(tabID, goal string) error {
	client, base, expectedPath, err := a.remoteTabCommandTarget(tabID)
	if err != nil {
		return err
	}
	ctx, cancel := commandContext(a)
	defer cancel()
	body, _ := json.Marshal(map[string]string{"goal": goal})
	return servePostForSession(ctx, client, serveURL(base, "/goal"), body, expectedPath)
}

func (a *App) SetRemoteTabQualityFloor(tabID, floor string) error {
	return a.remoteTabPost(tabID, "/quality-floor", map[string]any{"floor": floor})
}

// RemoteTabSnapshot mirrors the frontend shape: raw serve payloads passed
// through verbatim so the surface decides how to consume them.
