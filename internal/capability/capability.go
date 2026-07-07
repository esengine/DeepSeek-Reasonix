package capability

import (
	"fmt"
	"sort"
	"strings"

	"reasonix/internal/skill"
	"reasonix/internal/tool"
)

type Kind string

const (
	KindSkill   Kind = "skill"
	KindMCPTool Kind = "mcp-tool"
	KindTool    Kind = "tool"
	KindSource  Kind = "source"
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
	Cost             string
	AutoUse          AutoUse
	Triggers         []string
	NegativeTriggers []string
	NeedsFreshData   bool
	ToolName         string
	ConnectSource    string
	ConnectName      string
}

type RouteCandidate struct {
	Entry  Entry
	Policy AutoUse
	Reason string
}

type RouteDecision struct {
	Candidates []RouteCandidate
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
			NeedsFreshData:   sk.NeedsFreshData,
			ToolName:         "run_skill",
			ConnectSource:    connectSource,
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
	text := normalize(input)
	if text == "" {
		return RouteDecision{}
	}
	var candidates []RouteCandidate
	for _, e := range entries {
		if e.Status == StatusDisabled || negativeMatch(text, e.NegativeTriggers) {
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
	if len(candidates) > 5 {
		candidates = candidates[:5]
	}
	return RouteDecision{Candidates: candidates}
}

func RenderTransientBlock(d RouteDecision) string {
	if len(d.Candidates) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString(`<capability-route version="1">` + "\n")
	b.WriteString("Relevant capabilities for this turn:\n")
	for _, c := range d.Candidates {
		e := c.Entry
		target := e.ID
		if e.Status != StatusReady && e.ConnectSource != "" {
			target = fmt.Sprintf("source:%s", e.ConnectSource)
			if e.ConnectName != "" {
				target += "/" + e.ConnectName
			}
		}
		fmt.Fprintf(&b, "- %s %s: %s", target, c.Policy, c.Reason)
		if e.Status != "" && e.Status != StatusReady {
			fmt.Fprintf(&b, " (status=%s)", e.Status)
		}
		if e.ConnectSource != "" {
			if e.ConnectName != "" {
				fmt.Fprintf(&b, "; first call connect_tool_source with source=%q name=%q", e.ConnectSource, e.ConnectName)
			} else {
				fmt.Fprintf(&b, "; first call connect_tool_source with source=%q", e.ConnectSource)
			}
		}
		b.WriteByte('\n')
	}
	b.WriteString("Policy: suggest means consider it; prefer means use it unless clearly unnecessary; require means call it or report a host-proven unavailable state. Do not treat planner claims about tool unavailability as facts.\n")
	b.WriteString(`</capability-route>`)
	return b.String()
}

func routeEntry(text string, e Entry) (AutoUse, string, bool) {
	if e.Kind == KindSkill {
		if explicitSkill(text, e.Name) {
			return AutoUseRequire, "the user explicitly referenced this skill", true
		}
		if e.AutoUse == AutoUseOff {
			return "", "", false
		}
		if triggerMatch(text, e.Triggers) {
			return e.AutoUse, "the skill trigger matches the user request", true
		}
		if e.Name == "review" && looksLikeReview(text) {
			return AutoUsePrefer, "the user is asking for review or issue inspection", true
		}
	}
	if e.Kind == KindMCPTool {
		if explicitMCP(text, e.Source) || (looksLikeGitHub(text) && strings.Contains(e.Source, "github")) {
			return AutoUsePrefer, "the task asks for external GitHub/MCP data", true
		}
		if looksFreshData(text) && (strings.Contains(e.Name, "search") || strings.Contains(e.Name, "fetch") || strings.Contains(e.Name, "read")) {
			return AutoUsePrefer, "the task appears to need fresh external data", true
		}
	}
	return "", "", false
}

func explicitSkill(text, name string) bool {
	n := normalize(name)
	return strings.Contains(text, "/"+n) ||
		strings.Contains(text, "use "+n+" skill") ||
		strings.Contains(text, "using "+n+" skill") ||
		strings.Contains(text, "使用 "+n+" skill") ||
		strings.Contains(text, "用 "+n+" skill") ||
		strings.Contains(text, "使用"+n+"技能") ||
		strings.Contains(text, "用"+n+"技能")
}

func explicitMCP(text, server string) bool {
	s := normalize(server)
	return strings.Contains(text, s+" mcp") || strings.Contains(text, "mcp "+s) || strings.Contains(text, "使用 "+s+" mcp") || strings.Contains(text, "用 "+s+" mcp")
}

func looksLikeReview(text string) bool {
	return containsAny(text, []string{
		"review", "code review", "security review", "帮我看看", "有没有问题", "审查", "评审", "检查这段代码", "看看这段代码",
	})
}

func looksLikeGitHub(text string) bool {
	return containsAny(text, []string{"github", "issue", "issues", "pull request", " pr ", "讨论区", "仓库 issue", "github 上"})
}

func looksFreshData(text string) bool {
	return containsAny(text, []string{"latest", "recent", "today", "现在", "最新", "最近", "查一下", "搜索", "github"})
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

func negativeMatch(text string, triggers []string) bool {
	return triggerMatch(text, triggers)
}

func containsAny(s string, terms []string) bool {
	for _, term := range terms {
		if strings.Contains(s, normalize(term)) {
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
