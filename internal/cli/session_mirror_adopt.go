package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"time"

	"reasonix/internal/agent"
)

// ObserveOwnedSession enables discovery for an ordinary CLI session. Discovery
// runs off the UI thread and retries when Serve is started after the TUI.
// Handed-off sessions already have a binding and use the same forwarding loop.
func (m *cliTakeoverManager) ObserveOwnedSession() {
	if m == nil || m.leases == nil || m.closed.Load() || m.returned.Load() {
		return
	}
	m.returnMu.Lock()
	defer m.returnMu.Unlock()
	if m.closed.Load() || m.returned.Load() {
		return
	}
	m.mu.Lock()
	m.discoverAfter = time.Time{}
	m.ensureStartedLocked()
	wake := m.wake
	m.mu.Unlock()
	select {
	case wake <- struct{}{}:
	default:
	}
}

// Caller holds sendMu, which also fences ordinary lease acquisition. An
// unregistered writer is never asked to yield on an unrelated Serve's 409.
func (m *cliTakeoverManager) adoptOwnedSessionLocked() {
	if m.leases == nil || m.closed.Load() || m.returned.Load() {
		return
	}
	m.mu.Lock()
	ctrl := m.ctrl
	if ctrl == nil || time.Now().Before(m.discoverAfter) {
		m.mu.Unlock()
		return
	}
	m.discoverAfter = time.Now().Add(cliTakeoverHeartbeat)
	m.mu.Unlock()
	path := agent.CanonicalSessionPath(ctrl.SessionPath())
	if path == "" || m.leases.HeldPath() != path {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	for _, record := range discoverCLIServesForTakeover() {
		if ctx.Err() != nil {
			return
		}
		client, err := cliServeClient(ctx, record)
		if err != nil {
			continue
		}
		payload, _ := json.Marshal(map[string]string{"sessionPath": path, "writerId": agent.SessionWriterID()})
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, record.base+"/adopt", bytes.NewReader(payload))
		if err != nil {
			continue
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err := client.Do(req)
		if err != nil {
			continue
		}
		body, readErr := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
		resp.Body.Close()
		var grant cliTakeoverGrant
		if readErr != nil || resp.StatusCode != http.StatusOK || json.Unmarshal(body, &grant) != nil ||
			grant.MirrorID == "" || grant.ReturnHandoffID == "" || grant.SourceWriterID == "" ||
			grant.TargetWriterID != agent.SessionWriterID() || agent.CanonicalSessionPath(grant.SessionPath) != path {
			continue
		}
		binding := &cliTakeoverBinding{path: path, record: record, client: client, grant: grant}
		m.mu.Lock()
		valid := m.binding == nil && m.ctrl == ctrl && !m.closed.Load() && !m.returned.Load() &&
			agent.CanonicalSessionPath(ctrl.SessionPath()) == path && m.leases.HeldPath() == path
		if valid {
			m.binding = binding
			m.revision++
			m.failures = 0
		}
		m.mu.Unlock()
		if !valid {
			m.mirrorEndLocked(binding)
		}
		return
	}
}
