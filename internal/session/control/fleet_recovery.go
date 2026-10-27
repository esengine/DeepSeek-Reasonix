package control

import (
	"encoding/json"
	"fmt"
	"reasonix/internal/runtime/delegation"
)

type fleetRecoveryReader interface {
	FleetRecoveryPlans(path, parent, workspace, ref string) ([]delegation.FleetRecoveryPlan, error)
}

func (c *Controller) FleetRecoveryPlans(ref string) ([]delegation.FleetRecoveryPlan, error) {
	reader, ok := c.subagents.(fleetRecoveryReader)
	if !ok {
		return []delegation.FleetRecoveryPlan{}, nil
	}
	return reader.FleetRecoveryPlans(c.SessionPath(), c.parentSessionID(), c.WorkspaceRoot(), ref)
}

// RequestFleetRecovery uses ordinary turn admission and tool permissions.
func (c *Controller) RequestFleetRecovery(ref, id, snapshot string) error {
	parent := c.parentSessionID()
	if err := c.ensureWriteAuthorityReady(); err != nil {
		return err
	}
	plans, err := c.FleetRecoveryPlans(ref)
	if err != nil {
		return err
	}
	for _, plan := range plans {
		if plan.ID != id {
			continue
		}
		if plan.Snapshot != snapshot {
			return fmt.Errorf("recovery plan changed; inspect it again")
		}
		if !plan.Ready {
			return fmt.Errorf("recovery is not available: %s", plan.Reason)
		}
		args, _ := json.Marshal(map[string]any{"recovery": map[string]string{"action": "resume", "id": id}})
		if plan.Legacy {
			args = plan.Arguments
		}
		_, err := c.EnqueueHostContinuation(InboxRequest{expectedSession: parent, Display: "Recover selected task graph", Raw: "Recover selected task graph", Submit: "The user explicitly requested recovery of the inspected task graph. Invoke fleet with these arguments, preserving current permission checks: " + string(args), Source: "host:fleet-recovery", Idempotency: "fleet-recovery:" + id + ":" + snapshot})
		return err
	}
	return delegation.ErrSubagentNotFound
}
