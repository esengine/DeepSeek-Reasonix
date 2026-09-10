package control

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"reasonix/internal/agent"

	"reasonix/internal/provider"
)

type ToolRecoverySnapshot struct {
	Silent       bool                         `json:"silent"`
	Statistics   agent.ToolRecoveryStatistics `json:"statistics"`
	SessionPath  string                       `json:"sessionPath"`
	RuntimeEpoch string                       `json:"runtimeEpoch"`
	Revision     string                       `json:"revision"`
	Calls        []provider.ToolCallRecord    `json:"calls"`
	RetryEnabled bool                         `json:"retryEnabled"`
}

type ToolRecoveryRequest struct {
	SessionPath  string `json:"sessionPath"`
	RuntimeEpoch string `json:"runtimeEpoch"`
	Revision     string `json:"revision"`
	AttemptID    string `json:"attemptId"`
	InspectionID string `json:"inspectionId"`
	Action       string `json:"action"` // inspect | confirm | reject | retry
}

func (c *Controller) ToolRecoverySnapshot() ToolRecoverySnapshot {
	view := ToolRecoverySnapshot{SessionPath: c.SessionPath(), RuntimeEpoch: c.RuntimeStateSnapshot().RuntimeEpoch, Calls: []provider.ToolCallRecord{}, RetryEnabled: os.Getenv("REASONIX_TOOL_RECOVERY_RETRY") == "1"}
	if c.executor != nil {
		view.Calls = c.executor.PendingToolRecovery()
		view.Statistics = c.executor.ToolRecoveryStatistics()
		view.Silent = c.executor.SilentToolRecovery()
	}
	// Raw parameters stay in the session. Frontends get immutable identities
	// and inspection facts, never an executable payload supplied by the UI.
	for i := range view.Calls {
		view.Calls[i].Arguments = nil
	}
	bytes, _ := json.Marshal(view)
	sum := sha256.Sum256(bytes)
	view.Revision = hex.EncodeToString(sum[:])
	return view
}

// ResolveToolRecovery uses the same admission exclusion and session write
// authority as model turns. No stale tab may resolve a replacement session.
func (c *Controller) ResolveToolRecovery(ctx context.Context, req ToolRecoveryRequest) (ToolRecoverySnapshot, error) {
	if err := c.ensureWriteAuthorityReady(); err != nil {
		return ToolRecoverySnapshot{}, err
	}
	c.mu.Lock()
	if c.running || c.finishing || c.rotating || c.closed {
		c.mu.Unlock()
		return ToolRecoverySnapshot{}, ErrTurnRunning
	}
	c.rotating = true
	c.mu.Unlock()
	defer func() { c.mu.Lock(); c.rotating = false; c.mu.Unlock() }()
	view := c.ToolRecoverySnapshot()
	if req.SessionPath != view.SessionPath || req.RuntimeEpoch == "" || req.RuntimeEpoch != view.RuntimeEpoch || req.Revision == "" || req.Revision != view.Revision {
		return view, fmt.Errorf("recovery snapshot changed; refresh before resolving")
	}
	if c.executor == nil {
		return view, fmt.Errorf("tool recovery unavailable")
	}
	var err error
	switch req.Action {
	case "inspect":
		_, err = c.executor.InspectToolRecovery(ctx, req.AttemptID)
	case "confirm", "reject":
		err = c.executor.ResolveToolRecovery(req.AttemptID, req.InspectionID, req.Action)
	case "retry":
		if !view.RetryEnabled {
			return view, fmt.Errorf("tool recovery retry is disabled")
		}
		err = c.executor.RetryToolRecovery(ctx, req.AttemptID, req.InspectionID)
	default:
		err = fmt.Errorf("unsupported recovery action")
	}
	result := c.ToolRecoverySnapshot()
	if err == nil && req.Action == "inspect" {
		for _, r := range c.executor.PendingToolRecovery() {
			if r.Identity.AttemptID == req.AttemptID {
				for i := range result.Calls {
					if result.Calls[i].Identity.AttemptID == req.AttemptID {
						result.Calls[i].Arguments = r.Arguments
					}
				}
			}
		}
	}
	return result, err
}
