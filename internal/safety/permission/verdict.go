package permission

import (
	"context"
	"encoding/json"
	"fmt"

	"reasonix/internal/base/textutil"
)

// Refusal codes a Gate attaches to a call it refused. The code is the identity
// a caller reports and counts; the reason beside it is only the wording.
const (
	RefusalDenyRule   = "permission.deny_rule"
	RefusalReadOnly   = "permission.read_only"
	RefusalDeclined   = "permission.declined"
	RefusalUnattended = "permission.unattended"
	// RefusalParked is a call that needed a person, was recorded for one, and
	// did not run. Unlike RefusalUnattended it names a record the model can cite.
	RefusalParked = "permission.parked"
	// RefusalUntrustedFolder is a call that needed approval because the person
	// never trusted this workspace folder, in a run nobody could ask.
	RefusalUntrustedFolder = "permission.untrusted_folder"
)

// IsRefusalCode reports whether code is one a permission gate produces.
func IsRefusalCode(code string) bool {
	switch code {
	case RefusalDenyRule, RefusalReadOnly, RefusalDeclined, RefusalUnattended, RefusalParked, RefusalUntrustedFolder:
		return true
	}
	return false
}

// RefusalStands reports a refusal that holds until the user changes the mode,
// a rule or their answer, so repeating the call cannot succeed. Parked (a record
// awaits a person) and UntrustedFolder (retry after trusting) are not final.
func RefusalStands(code string) bool {
	switch code {
	case RefusalDenyRule, RefusalReadOnly, RefusalDeclined, RefusalUnattended:
		return true
	}
	return false
}

// Verdict is a gate's answer to one call: whether it runs, and when it does
// not, the reason the model reads and the code a caller reports.
type Verdict struct {
	Allow  bool
	Reason string
	Code   string
}

// UnattendedApprover marks an approver that stands in for nobody. Its "no" is
// the absence of a person, which is a different fact from a person declining.
type UnattendedApprover interface {
	Unattended() bool
}

// CodedRefuser is an unattended approver that knows why nobody can approve a
// given call: it returns that call's refusal code, or "" for the generic
// RefusalUnattended. policyReason is the matched rule the gate asked about.
type CodedRefuser interface {
	UnattendedApprover
	RefusalCodeFor(toolName, subject, policyReason string, args json.RawMessage) string
}

const readOnlyRefusal = "read-only mode: this session changes nothing — no file writes, no shell command that is not a known read, no tool that is not declared read-only. Do not retry it or rewrite it into another form; report what you would change, and the user can leave read-only mode to let it run."

// Check decides whether a tool call may run. It is the method the agent's Gate
// interface expects; Verdict is the same answer with the refusal's identity.
func (g *Gate) Check(ctx context.Context, toolName string, args json.RawMessage, readOnly bool) (bool, string, error) {
	v, err := g.Verdict(ctx, toolName, args, readOnly)
	return v.Allow, v.Reason, err
}

// DeniesWriters reports whether this gate refuses every call that is not a
// read, which a caller holding a path around the gate has to honour too.
func (g *Gate) DeniesWriters() bool { return g.Policy.ReadOnly }

// Verdict decides whether a tool call may run.
func (g *Gate) Verdict(ctx context.Context, toolName string, args json.RawMessage, readOnly bool) (Verdict, error) {
	if toolName == "bash" && !readOnly && BashCommandIsReadOnly(args) {
		readOnly = true
	}
	programKeys := ""
	if toolName == "bash" && readOnly {
		if keys := g.Policy.gitReadPrograms(ctx, args); keys != "" {
			readOnly, programKeys = false, keys
		}
	}
	// Producing an install plan reads the source and writes nothing. Treating
	// the preview as the write it precedes would train the user to approve the
	// question that carries no information, ahead of the one that does.
	if toolName == "install_source" && !readOnly && InstallSourceIsPlanOnly(args) {
		readOnly = true
	}
	if g.Policy.ReadOnly && !readOnly && !g.Policy.ExplicitlyDenies(toolName, args) {
		return Verdict{Reason: readOnlyRefusal, Code: RefusalReadOnly}, nil
	}
	decision := g.Policy.Decide(toolName, readOnly, args)
	ruleReason := ""
	if rule, ok := g.Policy.MatchedRule(toolName, decision, args); ok {
		ruleReason = fmt.Sprintf("Matched permission rule: %s %s", decision, rule)
	}
	if ruleReason == "" && programKeys != "" {
		ruleReason = "This git command reads a repository whose own configuration runs programs (" + programKeys + "), so it is not treated as a plain read."
		if decision == Allow && g.Policy.Mode != Allow {
			decision = Ask
		}
	}
	switch decision {
	case Deny:
		return g.denial(ruleReason), nil
	case Ask:
		return g.ask(ctx, toolName, args, ruleReason)
	default:
		if label := OriginFrom(ctx); label != "" && !readOnly && g.Policy.AskOnOrigin && g.Policy.allowedOnlyByMode(toolName, readOnly, args) {
			return g.ConfirmOrigin(ctx, toolName, args, label)
		}
		return Verdict{Allow: true}, nil
	}
}

func (g *Gate) ask(ctx context.Context, toolName string, args json.RawMessage, ruleReason string) (Verdict, error) {
	if g.Approver == nil {
		allow, reason, err := unattendedAsk(toolName, args)
		if !allow {
			return Verdict{Reason: reason, Code: RefusalUnattended}, err
		}
		return Verdict{Allow: true}, err
	}
	allow, remember, approverReason, err := g.approve(ctx, toolName, Subject(args), args, ruleReason)
	if err != nil {
		return Verdict{Reason: "approval aborted"}, err
	}
	if !allow {
		v := Verdict{Reason: "the user declined this tool call — do not retry it; ask how they would like to proceed or choose another approach.", Code: RefusalDeclined}
		if approverReason != "" {
			v.Reason = approverReason
		}
		if u, ok := g.Approver.(UnattendedApprover); ok && u.Unattended() {
			v.Code = RefusalUnattended
			if c, ok := u.(CodedRefuser); ok {
				if code := c.RefusalCodeFor(toolName, Subject(args), ruleReason, args); code != "" {
					v.Code = code
				}
			}
		}
		return v, nil
	}
	if remember && g.OnRemember != nil {
		// "Always allow" is tool-wide and deny rules still win on every call.
		// The rule also joins this Policy so a direct Decide sees it before
		// the next controller build.
		g.OnRemember(toolName)
		if rule, ok := ParseRule(toolName); ok {
			g.Policy.Allow = append(g.Policy.Allow, rule)
		}
	}
	return Verdict{Allow: true}, nil
}

const postureRefusal = "this session's permission mode refuses anything that would need approval, and this call would. Nobody declined it and no deny rule matched. Do not retry this call or a rewording of it unless the user changes the mode or approves it; do the part that needs no approval, or report what was refused."

// denial names what refused a call: a matched deny rule, the read-only
// posture, or a mode that refuses whatever it would otherwise ask about.
func (g *Gate) denial(ruleReason string) Verdict {
	switch {
	case ruleReason != "":
		return Verdict{Reason: ruleReason + "\ndenied by permission policy — this tool/command is on the deny list. Do not retry it; choose another approach or stop and explain.", Code: RefusalDenyRule}
	case g.Policy.ReadOnly:
		return Verdict{Reason: readOnlyRefusal, Code: RefusalReadOnly}
	default:
		return Verdict{Reason: postureRefusal, Code: RefusalUnattended}
	}
}

// ConfirmTool names the approval a stricter party opens on a call the policy
// allowed. It is a fresh decision: a person answers it, never a posture such as
// YOLO or auto, and the answer covers this one call.
const ConfirmTool = "mod_check"

// Confirm puts a call the policy allowed to the user anyway, on behalf of who
// (a Claude Code mod that may only tighten). It cannot loosen anything: the
// verdict is Allow only when a person approved, and a gate with nobody to ask
// refuses.
func (g *Gate) Confirm(ctx context.Context, toolName string, args json.RawMessage, who string) (Verdict, error) {
	if g.Approver == nil {
		return Verdict{Reason: who + " asked for approval of this call and nobody can give it, so it was refused.", Code: RefusalUnattended}, nil
	}
	subject := who + " asks you to approve: " + toolName + " " + Subject(args)
	allow, _, reason, err := g.approve(ctx, ConfirmTool, subject, args, who+" requires approval for this call")
	if err != nil {
		return Verdict{Reason: "approval aborted"}, err
	}
	if !allow {
		v := Verdict{Reason: "the user declined this tool call — do not retry it; ask how they would like to proceed or choose another approach.", Code: RefusalDeclined}
		if reason != "" {
			v.Reason = reason
		}
		return v, nil
	}
	return Verdict{Allow: true}, nil
}

// OriginConfirmTool names the approval opened on a call that repeats text from an
// external result. Like ConfirmTool it is a fresh decision: a person answers it,
// never a posture such as YOLO or auto, and the answer covers this one call.
const OriginConfirmTool = "origin_check"

// ConfirmOrigin puts a call the policy allowed to the user because its arguments
// carry text from origin (for example "web:example.com"). A gate with nobody to
// ask refuses; the verdict is Allow only when a person approved.
func (g *Gate) ConfirmOrigin(ctx context.Context, toolName string, args json.RawMessage, origin string) (Verdict, error) {
	if g.Approver == nil {
		return Verdict{Reason: "this call repeats text from " + origin + ", the session is set to ask about that, and nobody can answer, so it was refused.", Code: RefusalUnattended}, nil
	}
	// The call text is the model's own: shown on one line with controls removed, so
	// it cannot pass for another line of the card or hide behind an escape.
	call, cleaned := textutil.ScrubLaunch(toolName + " " + Subject(args))
	subject := "text from " + origin + " reached this call: " + call
	why := "this call repeats text from " + origin
	if cleaned {
		why += "; line breaks or control characters in the call are hidden here"
	}
	allow, _, reason, err := g.approve(ctx, OriginConfirmTool, subject, args, why)
	if err != nil {
		return Verdict{Reason: "approval aborted"}, err
	}
	if !allow {
		v := Verdict{Reason: "the user declined this tool call — do not retry it; ask how they would like to proceed or choose another approach.", Code: RefusalDeclined}
		if reason != "" {
			v.Reason = reason
		}
		return v, nil
	}
	return Verdict{Allow: true}, nil
}

// Checker is any gate a tool call is put to.
type Checker interface {
	Check(ctx context.Context, toolName string, args json.RawMessage, readOnly bool) (bool, string, error)
}

// VerdictOf asks g for its verdict, falling back to Check for a gate that
// names no refusal code.
func VerdictOf(ctx context.Context, g Checker, toolName string, args json.RawMessage, readOnly bool) (Verdict, error) {
	if vg, ok := g.(interface {
		Verdict(context.Context, string, json.RawMessage, bool) (Verdict, error)
	}); ok {
		return vg.Verdict(ctx, toolName, args, readOnly)
	}
	allow, reason, err := g.Check(ctx, toolName, args, readOnly)
	return Verdict{Allow: allow, Reason: reason}, err
}

// PostureRefusal is g's refusal of a writer that skips the ordinary gate, such
// as an authorized MCP server, while g's posture is read-only. Authorizing the
// server answered whether it may be called, not whether this session writes.
func PostureRefusal(ctx context.Context, g Checker, toolName string, args json.RawMessage, readOnly bool) (Verdict, bool) {
	dg, ok := g.(interface{ DeniesWriters() bool })
	if readOnly || !ok || !dg.DeniesWriters() {
		return Verdict{}, false
	}
	v, err := VerdictOf(ctx, g, toolName, args, readOnly)
	if err == nil && v.Allow {
		return Verdict{}, false
	}
	return v, true
}
