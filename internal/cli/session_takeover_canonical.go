package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"reasonix/internal/agent"
	"reasonix/internal/control"
	"reasonix/internal/i18n"
	"reasonix/internal/session"
)

// cliCanonicalRoutePrefix marks a takeover/resume target as a final-format
// identity instead of a legacy transcript path. It matches the serve and
// desktop routing prefix for exclusive identity sessions.
const cliCanonicalRoutePrefix = "session-id:"

func isCLICanonicalRoute(target string) bool {
	return strings.HasPrefix(strings.TrimSpace(target), cliCanonicalRoutePrefix)
}

func cliCanonicalRouteID(target string) (string, bool) {
	id, ok := strings.CutPrefix(strings.TrimSpace(target), cliCanonicalRoutePrefix)
	return id, ok && id != ""
}

func cliCanonicalRoute(sessionID string) string {
	return cliCanonicalRoutePrefix + strings.TrimSpace(sessionID)
}

// sessionWriterHeldNotice is the friendly refusal for a final-format session
// whose writer another runtime owns.
func sessionWriterHeldNotice() string {
	return "this session is written by another Reasonix runtime on this machine; run /takeover to take it over"
}

// cliTakeoverIdentityHeldSession asks every resident serve to hand the
// final-format identity over. The writer lock carries no PID, so discovery is
// exhaustive rather than PID-matched; the serve that actually holds the
// identity grants, the others refuse.
func cliTakeoverIdentityHeldSession(route string, manager *cliTakeoverManager) (*cliTakeoverBinding, error) {
	if manager != nil && manager.Reclaiming() {
		return nil, fmt.Errorf("the remote side is reclaiming the current session")
	}
	records := discoverCLIServesForTakeover()
	if len(records) == 0 {
		return nil, fmt.Errorf("no resident serve on this machine holds this session")
	}
	var lastErr error
	for i := range records {
		binding, err := cliTakeoverIdentityFromServe(route, &records[i])
		if err == nil {
			return binding, nil
		}
		lastErr = err
	}
	return nil, lastErr
}

// cliTakeoverIdentityFromServe performs one serve's identity handoff. The
// grant must name the exact route this process asked for.
func cliTakeoverIdentityFromServe(route string, record *cliServeRecord) (*cliTakeoverBinding, error) {
	ctx, cancel := context.WithTimeout(context.Background(), cliTakeoverTimeout+15*time.Second)
	defer cancel()
	client, err := cliServeClient(ctx, *record)
	if err != nil {
		return nil, fmt.Errorf("takeover from local serve: %w", err)
	}
	body, _ := json.Marshal(map[string]any{
		"sessionPath": route, "targetWriterId": agent.SessionWriterID(),
		"force": true, "mode": "wait", "timeoutMs": cliTakeoverTimeout.Milliseconds(),
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, record.base+"/handoff", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("takeover from local serve: %w", err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("takeover from local serve: %s", strings.TrimSpace(string(respBody)))
	}
	var grant cliTakeoverGrant
	if json.Unmarshal(respBody, &grant) != nil || grant.MirrorID == "" || grant.ReturnHandoffID == "" ||
		grant.SourceWriterID == "" || grant.TargetWriterID != agent.SessionWriterID() ||
		strings.TrimSpace(grant.SessionPath) != route {
		return nil, fmt.Errorf("invalid handoff grant")
	}
	return &cliTakeoverBinding{path: route, canonical: true, record: *record, client: client, grant: grant}, nil
}

// ctrlOpenCanonicalSession attaches the controller to a final-format identity.
func ctrlOpenCanonicalSession(ctrl control.SessionAPI, ref session.SessionRef) error {
	identity, ok := ctrl.(control.IdentityLifecycle)
	if !ok || !identity.UsesExclusiveSession() {
		return errors.New("final-format session resume requires the session engine")
	}
	if _, err := identity.OpenSession(context.Background(), ref); err != nil {
		return err
	}
	return nil
}

// cliCanonicalRouteRef resolves the identity a route names against the
// workspace's session service, so the takeover opens the same identity the
// serve released.
func cliCanonicalRouteRef(sessionDir, route string) (session.SessionRef, error) {
	id, ok := cliCanonicalRouteID(route)
	if !ok {
		return session.SessionRef{}, errors.New("invalid session identity")
	}
	service := cliSessionService(sessionDir)
	if service == nil {
		return session.SessionRef{}, errors.New("session service unavailable")
	}
	ref := session.SessionRef{HostID: service.HostID(), SessionID: id}
	if _, err := service.SessionDir(context.Background(), ref); err != nil {
		return session.SessionRef{}, fmt.Errorf("unknown session: %w", err)
	}
	return ref, nil
}

// runCanonicalTakeoverCommand handles "/takeover" for a final-format identity:
// every resident serve is asked to hand the writer over; on grant the
// controller attaches through OpenSession and the mirror manager forwards
// frames so the remote tab keeps rendering read-only.
func (m *chatTUI) runCanonicalTakeoverCommand(route string) {
	if m.ctrl.Running() {
		m.notice(i18n.M.ResumeBusy)
		return
	}
	ref, err := cliCanonicalRouteRef(m.ctrl.SessionDir(), route)
	if err != nil {
		m.notice("takeover: " + err.Error())
		return
	}
	if err := m.ctrl.Snapshot(); err != nil {
		m.notice("takeover: snapshot current session: " + err.Error())
		return
	}
	m.followSessionLease()
	binding, err := cliTakeoverIdentityHeldSession(route, m.takeover)
	if err != nil {
		m.restoreSessionLease()
		m.notice("takeover: " + err.Error())
		return
	}
	if err := m.commitCanonicalSessionSwitch(ref); err != nil {
		cliEndFailedHandoff(binding)
		m.restoreSessionLease()
		m.notice("takeover: " + err.Error())
		return
	}
	m.pendingTakeoverPath = ""
	if m.takeover != nil {
		m.takeover.AttachController(m.ctrl)
		m.takeover.Activate(binding)
	}
	m.replayActiveBranch(i18n.M.ResumedTitle)
	m.notice("session taken over; the remote side is now read-only and can take it back")
}

// cliStartupCanonicalTakeover is the startup counterpart: after a confirmed
// prompt (or --takeover), the resident serve releases the identity and this
// process attaches as the new writer.
func cliStartupCanonicalTakeover(ctrl control.SessionAPI, manager *cliTakeoverManager, target cliResumeTarget) error {
	route := cliCanonicalRoute(target.ref.SessionID)
	binding, err := cliTakeoverIdentityHeldSession(route, manager)
	if err != nil {
		return err
	}
	if err := ctrlOpenCanonicalSession(ctrl, target.ref); err != nil {
		cliEndFailedHandoff(binding)
		return err
	}
	if manager != nil {
		manager.AttachController(ctrl)
		manager.Activate(binding)
	}
	return nil
}
