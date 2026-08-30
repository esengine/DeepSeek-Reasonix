package agent

import (
	"context"
	"strings"

	"reasonix/internal/tool"
)

type schemaErrorRecord struct {
	count           int
	inspectAttached bool
}

func (a *Agent) schemaErrorContract(plan *toolCallPlan, result tool.ArgumentValidationResult, sig string) (string, bool) {
	record := a.turn.loop.incrementSchemaError(sig, strings.TrimSpace(plan.resolved.CapabilityID))
	msg := argumentValidationMessage(plan, result)
	switch {
	case record.count == 2 && !record.inspectAttached:
		if summary := a.schemaInspectSummary(plan); summary != "" {
			msg += "\n\nHost attached inspect summary after a repeated schema error:\n" + summary
			a.turn.loop.markSchemaInspectAttached(sig)
		}
	case record.count >= 3:
		msg = fmtSchemaBlockMessage(plan.permName, shortSchemaFingerprint(result.Fingerprint))
		if a.capabilityAudit != nil {
			a.capabilityAudit.RecordLoopGuard("blocked")
		}
		return msg, true
	}
	if a.capabilityAudit != nil {
		a.capabilityAudit.RecordLoopGuard("repeat_failure")
	}
	return msg, false
}

func fmtSchemaBlockMessage(name, fingerprint string) string {
	return "blocked: [loop guard] " + name + " failed argument validation 3 times with the same schema fingerprint " + fingerprint +
		". Do not retry by rewriting arguments. Inspect the exact capability (or wait for a schema change / new user turn) before calling it again."
}

func (a *Agent) schemaInspectSummary(plan *toolCallPlan) string {
	id := strings.TrimSpace(plan.resolved.CapabilityID)
	if id == "" || a.svc.tools == nil {
		return ""
	}
	proxy, ok := a.svc.tools.Get("use_capability")
	if !ok {
		return ""
	}
	uc, ok := proxy.(*UseCapabilityTool)
	if !ok {
		return ""
	}
	out, err := uc.inspect(context.Background(), id)
	if err != nil {
		return ""
	}
	if len(out) > 2048 {
		out = out[:2048] + "\n[inspect truncated]"
	}
	return out
}

func (a *Agent) clearSchemaError(name, fingerprint string) {
	prefix := "argument_validation:" + name + ":" + shortSchemaFingerprint(fingerprint) + ":"
	a.turn.loop.clearSchemaErrors(func(sig string) bool { return strings.HasPrefix(sig, prefix) })
}

func (a *Agent) clearSchemaErrorsAfterInspect(id string) {
	if id == "" {
		return
	}
	a.turn.loop.clearSchemaErrorsForCapability(id)
}
