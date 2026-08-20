package capdiag

import (
	"sort"
	"strings"

	"reasonix/internal/config"
	"reasonix/internal/permission"
	"reasonix/internal/tool"
	_ "reasonix/internal/tool/builtin"
)

// collectPermissions validates the configured [permissions] rules and derives
// each built-in tool's effective decision for a bare call. Validation findings
// are appended to issues; the report carries per-rule status and per-tool
// decisions for the Diagnostics page. Rule strings are shown verbatim: they are
// user-authored config being diagnosed, not external data needing scrubbing.
func collectPermissions(cfg *config.Config, issues *[]Issue) PermissionsReport {
	rep := PermissionsReport{
		// Echo the effective mode the engine will use, so an invalid or missing
		// mode string (silently treated as ask) cannot contradict the decisions
		// shown in the tools table.
		Mode:  permission.ParseDecision(cfg.Permissions.Mode).String(),
		Allow: []PermissionRuleInfo{},
		Ask:   []PermissionRuleInfo{},
		Deny:  []PermissionRuleInfo{},
		Tools: []PermissionToolInfo{},
	}

	builtinIDs := map[string]bool{}
	for _, t := range tool.Builtins() {
		builtinIDs[t.Name()] = true
	}
	known := func(id string) bool { return builtinIDs[id] }

	ruleInfo := func(rule string) PermissionRuleInfo {
		problems := permission.ValidateRule(rule, known)
		info := PermissionRuleInfo{Rule: rule, Status: "ok"}
		if len(problems) == 0 {
			return info
		}
		worst := problems[0]
		for _, p := range problems {
			if p.Severity == "error" {
				worst = p
				break
			}
		}
		info.Status = permissionStatusToken(worst.Code)
		info.Message = permissionProblemMessage(problems)
		*issues = append(*issues, Issue{
			Severity: worst.Severity, Code: worst.Code, Subsystem: "permissions",
			Name: rule, Message: worst.Message,
			Remediation: "Fix the rule in Settings → Permissions or reasonix.toml, then refresh",
			SettingsTab: "permissions",
		})
		return info
	}
	for _, rule := range cfg.Permissions.Allow {
		rep.Allow = append(rep.Allow, ruleInfo(rule))
	}
	for _, rule := range cfg.Permissions.Ask {
		rep.Ask = append(rep.Ask, ruleInfo(rule))
	}
	for _, rule := range cfg.Permissions.Deny {
		rep.Deny = append(rep.Deny, ruleInfo(rule))
	}

	policy := permission.New(rep.Mode, cfg.Permissions.Allow, cfg.Permissions.Ask, cfg.Permissions.Deny)
	for _, t := range tool.Builtins() {
		readOnly := t.ReadOnly()
		decision := policy.DecideSubject(t.Name(), readOnly, "")
		info := PermissionToolInfo{Tool: t.Name(), ReadOnly: readOnly, Decision: decision.String(), Scope: "fallback"}
		if matched, ok := policy.BareRule(t.Name(), decision); ok {
			info.Matched = matched
			info.Scope = "rule"
		}
		rep.Tools = append(rep.Tools, info)
	}
	return rep
}

// permissionStatusToken shortens a stable permission.* code for the report's
// status field, which the frontend displays as a badge.
func permissionStatusToken(code string) string {
	if i := strings.LastIndexByte(code, '.'); i >= 0 {
		return code[i+1:]
	}
	return code
}

func permissionProblemMessage(problems []permission.Problem) string {
	msgs := make([]string, 0, len(problems))
	for _, p := range problems {
		msgs = append(msgs, p.Message)
	}
	sort.Strings(msgs)
	return strings.Join(msgs, "; ")
}
