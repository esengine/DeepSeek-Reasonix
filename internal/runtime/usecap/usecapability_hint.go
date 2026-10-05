package usecap

import (
	"encoding/json"
	"fmt"

	"reasonix/internal/contract/tool"
)

// misplacedArgumentHint reports that a required top-level field was supplied one
// level too deep, inside "arguments". Answering "field is required" to a caller
// that did supply it starts a retry loop: the value looks present, so the next
// attempt only rearranges the nesting.
func misplacedArgumentHint(arguments json.RawMessage, field string) string {
	var nested map[string]json.RawMessage
	if len(arguments) == 0 || json.Unmarshal(arguments, &nested) != nil {
		return ""
	}
	if _, ok := nested[field]; !ok {
		return ""
	}
	return fmt.Sprintf(" — it is nested inside \"arguments\"; move %q up beside \"action\" and leave only the capability's own arguments inside", field)
}

// contractHint carries the contract a rejected call broke. A rejection teaches
// nothing on its own: "unknown field \"items\"" never names "tasks", and the
// model pays round trips to inspect and to the docs to learn one name.
func contractHint(schema, arguments json.RawMessage) string {
	contract, ok := ReadArgumentContract(schema, arguments)
	if !ok {
		return ""
	}
	return contract.Hint()
}

func quoted(names []string) []string {
	out := make([]string, len(names))
	for i, name := range names {
		out[i] = fmt.Sprintf("%q", name)
	}
	return out
}

// recordCallFailure books a failed capability call and returns what the model
// reads, carrying the contract when the arguments were what broke it. The
// ledger keeps the capability's own error, unextended.
func (t *UseCapabilityTool) recordCallFailure(resolved tool.ResolvedCall, err error) error {
	if t.ledger != nil {
		t.ledger.MarkFailed(resolved.CapabilityID, err.Error())
	}
	if t.audit != nil {
		t.audit.RecordMCPProxy(false, true, true)
	}
	return fmt.Errorf("%w%s", err, contractHint(resolved.Target.Schema(), resolved.Args))
}

// WithContractHint attaches the contract a rejected call broke. A rejection
// teaches nothing on its own: `unknown field "items"` never names "tasks", and
// the model pays round trips to inspect and to the docs to learn it.
func WithContractHint(err error, target tool.Tool, args json.RawMessage) error {
	if err == nil || target == nil {
		return err
	}
	return fmt.Errorf("%w%s", err, contractHint(target.Schema(), args))
}
