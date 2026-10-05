package capability

import (
	"fmt"
	"sort"
	"strings"

	"reasonix/internal/contract/tool"
	"reasonix/internal/ext/skill"
)

type Kind string

const (
	KindSkill     Kind = "skill"
	KindMCPServer Kind = "mcp-server"
	KindMCPTool   Kind = "mcp-tool"
	KindTool      Kind = "tool"
	KindSource    Kind = "source"
)

type Status string

const (
	StatusReady      Status = "ready"
	StatusConfigured Status = "configured"
	StatusDisabled   Status = "disabled"
	StatusFailed     Status = "failed"
	StatusStale      Status = "stale"
)

type AutoUse string

const (
	AutoUseOff     AutoUse = "off"
	AutoUseSuggest AutoUse = "suggest"
	AutoUsePrefer  AutoUse = "prefer"
	AutoUseRequire AutoUse = "require"
)

type Entry struct {
	ID               string
	Kind             Kind
	Name             string
	Description      string
	Source           string
	Status           Status
	ReadOnly         bool
	Destructive      bool
	Cost             string
	AutoUse          AutoUse
	Triggers         []string
	NegativeTriggers []string
	ToolName         string
	// Aliases are extra capability IDs this entry answers to, declared by the
	// tool itself. Lookup accepts any of them.
	Aliases       []string
	ConnectSource string
	ConnectName   string
	Requires      []string // capability IDs this skill depends on
	Profiles      []string // economy|balanced|delivery; empty = all
	AutoStart     bool     // MCP: configured auto_start
	FailureReason string   // host-proven failure detail
}

type RouteCandidate struct {
	Entry  Entry
	Policy AutoUse
	Reason string
}

type RouteDecision struct {
	Candidates []RouteCandidate
	// Delivery marks a Delivery-profile route: the transient block must direct
	// the model to the stable use_capability proxy — connect_tool_source is not
	// registered in Delivery, so instructing it would dead-end the route.
	Delivery bool
	// CapabilityProxy directs unready MCP candidates to use_capability rather
	// than connect_tool_source. True for Delivery and for dual-model Planner
	// boots that expose the stable proxy without Economy's connector.
	CapabilityProxy bool
}

func SkillEntries(skills []skill.Skill, tools []tool.ContractEntry) []Entry {
	toolNames := map[string]bool{}
	for _, t := range tools {
		toolNames[t.Name] = true
	}
	skillToolReady := toolNames["run_skill"] || toolNames["read_skill"] || toolNames["read_only_skill"]

	out := make([]Entry, 0, len(skills))
	for _, sk := range skills {
		status := StatusReady
		connectSource := ""
		if !skillToolReady {
			status = StatusConfigured
			connectSource = "skills"
		}
		auto := normalizeAutoUse(sk.AutoUse)
		if auto == "" && len(sk.Triggers) > 0 {
			auto = AutoUsePrefer
		} else if auto == "" {
			auto = AutoUseSuggest
		}
		out = append(out, Entry{
			ID:               "skill:" + sk.Name,
			Kind:             KindSkill,
			Name:             sk.Name,
			Description:      sk.Description,
			Source:           string(sk.Scope),
			Status:           status,
			Cost:             strings.TrimSpace(sk.Cost),
			AutoUse:          auto,
			Triggers:         cleanList(sk.Triggers),
			NegativeTriggers: cleanList(sk.NegativeTriggers),
			ToolName:         "run_skill",
			ConnectSource:    connectSource,
			Requires:         cleanList(sk.Requires),
			Profiles:         cleanList(sk.Profiles),
		})
	}
	return out
}

func ToolEntries(tools []tool.ContractEntry) []Entry {
	out := make([]Entry, 0, len(tools))
	for _, t := range tools {
		e := Entry{
			ID:          "tool:" + t.Name,
			Kind:        KindTool,
			Name:        t.Name,
			Description: strings.TrimSpace(t.Description),
			Status:      StatusReady,
			ReadOnly:    t.ReadOnly,
			ToolName:    t.Name,
			Aliases:     cleanList(t.CapabilityAliases),
		}
		if server, raw, ok := tool.SplitMCPName(t.Name); ok {
			e.ID = "mcp-tool:" + server + "/" + raw
			e.Kind = KindMCPTool
			e.Name = server + "/" + raw
			e.Source = server
			e.ConnectName = server
		}
		out = append(out, e)
	}
	return out
}

func Route(input string, entries []Entry) RouteDecision {
	return RouteDecision{Candidates: limitRouteCandidates(routeCandidates(input, entries))}
}

// RouteDelivery routes against the full matched set before promoting built-in
// playbooks, so candidates that become prefer are never discarded by the
// ordinary suggest budget first.
func RouteDelivery(input string, entries []Entry) RouteDecision {
	return PromoteDelivery(RouteDecision{Candidates: routeCandidates(input, entries)})
}

func routeCandidates(input string, entries []Entry) []RouteCandidate {
	text := normalize(input)
	if text == "" {
		return nil
	}
	var candidates []RouteCandidate
	for _, e := range entries {
		if e.Status == StatusDisabled || e.Status == StatusFailed || triggerMatch(text, e.NegativeTriggers) {
			continue
		}
		if policy, reason, ok := routeEntry(text, e); ok {
			candidates = append(candidates, RouteCandidate{Entry: e, Policy: policy, Reason: reason})
		}
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		if rank(candidates[i].Policy) != rank(candidates[j].Policy) {
			return rank(candidates[i].Policy) > rank(candidates[j].Policy)
		}
		if candidates[i].Entry.Kind != candidates[j].Entry.Kind {
			return candidates[i].Entry.Kind < candidates[j].Entry.Kind
		}
		return candidates[i].Entry.ID < candidates[j].Entry.ID
	})
	return candidates
}

// PromoteDelivery strengthens matched built-in playbooks in Delivery. Custom
// skills keep their authored auto-use policy; only shipped workflows with a
// concrete trigger match move from suggest to prefer.
func PromoteDelivery(decision RouteDecision) RouteDecision {
	decision.Delivery = true
	for i := range decision.Candidates {
		candidate := &decision.Candidates[i]
		if candidate.Policy == AutoUseSuggest && candidate.Entry.Kind == KindSkill && candidate.Entry.Source == string(skill.ScopeBuiltin) {
			candidate.Policy = AutoUsePrefer
			candidate.Reason += "; Delivery prefers matched built-in playbooks"
		}
	}
	sort.SliceStable(decision.Candidates, func(i, j int) bool {
		if rank(decision.Candidates[i].Policy) != rank(decision.Candidates[j].Policy) {
			return rank(decision.Candidates[i].Policy) > rank(decision.Candidates[j].Policy)
		}
		if decision.Candidates[i].Entry.Kind != decision.Candidates[j].Entry.Kind {
			return decision.Candidates[i].Entry.Kind < decision.Candidates[j].Entry.Kind
		}
		return decision.Candidates[i].Entry.ID < decision.Candidates[j].Entry.ID
	})
	return RouteDecision{Candidates: limitRouteCandidates(decision.Candidates), Delivery: true, CapabilityProxy: true}
}

func limitRouteCandidates(candidates []RouteCandidate) []RouteCandidate {
	const targetCandidates = 5
	strong := make([]RouteCandidate, 0, len(candidates))
	suggested := make([]RouteCandidate, 0, targetCandidates)
	for _, candidate := range candidates {
		switch candidate.Policy {
		case AutoUseRequire, AutoUsePrefer:
			strong = append(strong, candidate)
		case AutoUseSuggest:
			suggested = append(suggested, candidate)
		}
	}
	slots := max(targetCandidates-len(strong), 0)
	if len(suggested) > slots {
		suggested = suggested[:slots]
	}
	return append(strong, suggested...)
}

func RenderTransientBlock(d RouteDecision) string {
	if len(d.Candidates) == 0 {
		return ""
	}
	var b strings.Builder
	seenLines := make(map[string]struct{}, len(d.Candidates))
	b.WriteString(`<capability-route version="1">` + "\n")
	b.WriteString("Relevant capabilities for this turn:\n")
	for _, c := range d.Candidates {
		e := c.Entry
		proxyMCP := d.CapabilityProxy && (e.Kind == KindMCPTool || e.Kind == KindMCPServer)
		target := e.ID
		if !d.Delivery && !proxyMCP && e.Status != StatusReady && e.ConnectSource != "" {
			target = fmt.Sprintf("source:%s", e.ConnectSource)
			if e.ConnectName != "" {
				target += "/" + e.ConnectName
			}
		}
		var line strings.Builder
		fmt.Fprintf(&line, "- %s %s: %s", target, c.Policy, c.Reason)
		if e.Status != "" && e.Status != StatusReady {
			fmt.Fprintf(&line, " (status=%s)", e.Status)
		}
		switch {
		case d.Delivery || proxyMCP:
			// Delivery and dual-model Planner have no connect_tool_source for
			// MCP; the stable proxy both connects and calls on demand, keeping
			// the concrete capability id.
			if e.Status != StatusReady {
				switch e.Kind {
				case KindMCPTool:
					fmt.Fprintf(&line, "; call use_capability(action=\"call\", capability_id=%q, arguments={...}) — it connects the server on demand after approval", e.ID)
				case KindMCPServer:
					fmt.Fprintf(&line, "; call use_capability(action=\"call\", capability_id=%q) to connect it (after approval) and list its tools, then call a listed mcp-tool id", e.ID)
				}
			}
		case e.ConnectSource != "":
			if e.ConnectName != "" {
				fmt.Fprintf(&line, "; first call connect_tool_source with source=%q name=%q", e.ConnectSource, e.ConnectName)
			} else {
				fmt.Fprintf(&line, "; first call connect_tool_source with source=%q", e.ConnectSource)
			}
		}
		rendered := line.String()
		if _, duplicate := seenLines[rendered]; duplicate {
			continue
		}
		seenLines[rendered] = struct{}{}
		b.WriteString(rendered)
		b.WriteByte('\n')
	}
	b.WriteString("Policy: suggest means consider it; prefer means use it unless clearly unnecessary; require means call it or report a host-proven unavailable state. Do not treat planner claims about tool unavailability as facts.\n")
	b.WriteString(`</capability-route>`)
	return b.String()
}

// routeEntry uses authored skill triggers and host-minted MCP tool names.
// Resolved inline skill invocations are handled structurally by the controller.
func routeEntry(text string, e Entry) (AutoUse, string, bool) {
	if e.Kind == KindSkill {
		if e.AutoUse == AutoUseOff {
			return "", "", false
		}
		if triggerMatch(text, e.Triggers) {
			return e.AutoUse, "the skill trigger matches the user request", true
		}
	}
	if e.Kind == KindMCPTool && namesMCPTool(text, e) {
		return AutoUsePrefer, "the user named this MCP tool", true
	}
	return "", "", false
}

// namesMCPTool matches the identifier the host mints for the tool. Matching
// "<server> mcp" instead fired on every sentence that mentioned the server,
// including "don't use the acme mcp server" and "别用 acme mcp", which then
// preferred the server the user had just declined.
func namesMCPTool(text string, e Entry) bool {
	tool := normalize(e.ToolName)
	return tool != "" && strings.Contains(text, tool)
}

func triggerMatch(text string, triggers []string) bool {
	for _, trig := range triggers {
		t := normalize(trig)
		if t != "" && strings.Contains(text, t) {
			return true
		}
	}
	return false
}

func normalize(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}

func cleanList(in []string) []string {
	var out []string
	seen := map[string]bool{}
	for _, v := range in {
		v = strings.TrimSpace(v)
		if v == "" || seen[v] {
			continue
		}
		seen[v] = true
		out = append(out, v)
	}
	return out
}

func normalizeAutoUse(raw string) AutoUse {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "off":
		return AutoUseOff
	case "suggest":
		return AutoUseSuggest
	case "prefer":
		return AutoUsePrefer
	case "require":
		return AutoUseRequire
	default:
		return ""
	}
}

func rank(a AutoUse) int {
	switch a {
	case AutoUseRequire:
		return 3
	case AutoUsePrefer:
		return 2
	case AutoUseSuggest:
		return 1
	default:
		return 0
	}
}
