package usecap

import (
	"encoding/json"

	"reasonix/internal/contract/tool"
	"reasonix/internal/runtime/capability"
)

// These expose the proxy's own judgements to the external tests in this
// directory, which check them against the agent and the delegation tools.

func ContractHint(schema, arguments json.RawMessage) string { return contractHint(schema, arguments) }

func RecordCallFailure(uc *UseCapabilityTool, resolved tool.ResolvedCall, err error) error {
	return uc.recordCallFailure(resolved, err)
}

func CapabilityLead(description string) string { return capabilityLead(description) }

func LedgerOf(uc *UseCapabilityTool) *capability.Ledger { return uc.ledger }
