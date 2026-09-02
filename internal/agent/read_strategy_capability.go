package agent

import (
	"context"
	"encoding/json"
	"fmt"

	"reasonix/internal/tool"
)

const sessionReadStrategyReceiptCapabilityID = "session:read_strategy_receipt"

type readStrategyStateBinder interface {
	bindReadStrategyState(func() *incompleteReadState)
}

type sessionReadStrategyReceiptTool struct {
	state func() *incompleteReadState
}

func (*sessionReadStrategyReceiptTool) Name() string { return "session_read_strategy_receipt" }

func (*sessionReadStrategyReceiptTool) Description() string {
	return "Validate search and exact read_file evidence for one host-restricted incomplete read."
}

func (*sessionReadStrategyReceiptTool) ReadOnly() bool { return true }

func (*sessionReadStrategyReceiptTool) Schema() json.RawMessage {
	return json.RawMessage(`{
		"type":"object",
		"properties":{
			"read_id":{"type":"string"},
			"search_tool_call_ids":{"type":"array","items":{"type":"string"},"minItems":1},
			"read_tool_call_ids":{"type":"array","items":{"type":"string"},"minItems":1},
			"conclusion":{"type":"string"}
		},
		"required":["read_id","search_tool_call_ids","read_tool_call_ids","conclusion"]
	}`)
}

func (t *sessionReadStrategyReceiptTool) Execute(ctx context.Context, raw json.RawMessage) (string, error) {
	args, ok := parseReadStrategyReceiptArgs(raw)
	if !ok {
		return "", fmt.Errorf("read strategy receipt: invalid arguments")
	}
	if t == nil || t.state == nil || t.state() == nil {
		return "", fmt.Errorf("read strategy receipt: current agent state is unavailable")
	}
	return t.state().submitStrategyReceipt(ctx, args)
}

func (a *Agent) bindReadStrategyCapability() {
	if a == nil || a.svc.tools == nil {
		return
	}
	proxy, ok := a.svc.tools.Get("use_capability")
	if !ok {
		return
	}
	binder, ok := proxy.(readStrategyStateBinder)
	if !ok {
		return
	}
	binder.bindReadStrategyState(func() *incompleteReadState {
		return &a.turn.incompleteReads
	})
}

func (t *UseCapabilityTool) bindReadStrategyState(state func() *incompleteReadState) {
	if t == nil {
		return
	}
	t.toolResultMu.Lock()
	t.readStrategyState = state
	t.toolResultMu.Unlock()
}

func (t *UseCapabilityTool) currentReadStrategyReceiptTarget() *sessionReadStrategyReceiptTool {
	if t == nil {
		return nil
	}
	t.toolResultMu.RLock()
	stateFn := t.readStrategyState
	t.toolResultMu.RUnlock()
	if stateFn == nil {
		return nil
	}
	state := stateFn()
	if state == nil || !state.hasStrategy() {
		return nil
	}
	return &sessionReadStrategyReceiptTool{state: stateFn}
}

func (t *UseCapabilityTool) resolveSessionReadStrategyReceipt(args json.RawMessage, base tool.ResolvedCall) (tool.ResolvedCall, error) {
	target := t.currentReadStrategyReceiptTarget()
	if target == nil {
		return tool.ResolvedCall{}, fmt.Errorf("capability %q is available only while a restricted read strategy is active", sessionReadStrategyReceiptCapabilityID)
	}
	base.TargetName = target.Name()
	base.Target = target
	base.Args = args
	base.ReadOnly = true
	return base, nil
}

func (t *UseCapabilityTool) resolveSessionCapability(id string, args json.RawMessage, base tool.ResolvedCall) (tool.ResolvedCall, error) {
	if id == sessionToolResultCapabilityID {
		return t.resolveSessionToolResult(args, base)
	}
	return t.resolveSessionReadStrategyReceipt(args, base)
}

func (t *UseCapabilityTool) inspectSessionReadStrategyReceipt() (string, error) {
	target := t.currentReadStrategyReceiptTarget()
	if target == nil {
		return "", fmt.Errorf("capability %q is not active", sessionReadStrategyReceiptCapabilityID)
	}
	payload := map[string]any{
		"id":          sessionReadStrategyReceiptCapabilityID,
		"kind":        "session",
		"name":        "read_strategy_receipt",
		"status":      "ready",
		"read_only":   true,
		"description": target.Description(),
		"arguments":   json.RawMessage(target.Schema()),
	}
	b, err := json.MarshalIndent(payload, "", "  ")
	return string(b), err
}
