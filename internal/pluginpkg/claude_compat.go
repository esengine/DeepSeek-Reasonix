package pluginpkg

import (
	"encoding/json"
	"fmt"
	"hash/fnv"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	fileencoding "reasonix/internal/fileutil/encoding"
	"reasonix/internal/frontmatter"
)

const (
	claudeHooksPath = "hooks/hooks.json"
	claudeMCPPath   = ".mcp.json"
)

var claudeHookEvents = map[string]bool{
	"PreToolUse": true, "PostToolUse": true, "PostToolUseFailure": true,
	"PermissionRequest": true, "UserPromptSubmit": true, "Stop": true,
	"StopFailure": true, "SessionStart": true, "SessionEnd": true,
	"SubagentStop": true, "Notification": true, "PreCompact": true,
}

type claudeHookDocument struct {
	Hooks map[string][]struct {
		Matcher string `json:"matcher"`
		Match   string `json:"match"`
		Hooks   []struct {
			Type        string            `json:"type"`
			Command     string            `json:"command"`
			Args        []string          `json:"args"`
			Description string            `json:"description"`
			Timeout     int               `json:"timeout"`
			Async       bool              `json:"async"`
			AsyncRewake bool              `json:"asyncRewake"`
			If          string            `json:"if"`
			Env         map[string]string `json:"env"`
		} `json:"hooks"`
	} `json:"hooks"`
}

// claudeStopBlockingEvents are the Claude hook events whose hook can block
// the action (force the turn/subagent to keep going on exit 2 or a top-level
// decision:"block") the way Claude's own contract does
// (https://code.claude.com/docs/en/hooks). Reasonix's Stop/SubagentStop hooks
// are observation-only and cannot force the loop to continue, so an imported
// hook here is a partial mapping, not a full one.
var claudeStopBlockingEvents = map[string]bool{"Stop": true, "SubagentStop": true}

// claudeToolScopedHookEvents are the events whose "matcher" field is
// evaluated against a tool name (see internal/hook's MatchesTool); other
// events ignore matcher entirely, so a matcher tool-name compatibility issue
// doesn't apply to them.
var claudeToolScopedHookEvents = map[string]bool{
	"PreToolUse": true, "PostToolUse": true, "PostToolUseFailure": true,
	"PermissionRequest": true,
}

// claudeUnsupportedToolMatchers are real Claude Code built-in tool names
// (https://code.claude.com/docs/en/tools-reference) that Reasonix never
// passes as a tool_name to a hook (see internal/hook's claudeToolNames /
// claudeToolMatchAliases, the actual dispatch-time mapping — keep the two in
// sync). A tool-scoped hook whose matcher names only entries from this set
// can never fire against a real Reasonix tool call. This is a conservative,
// individually-verified subset of Claude's full tool catalog, not an
// exhaustive enumeration — an unrecognized matcher is left unflagged rather
// than guessed at.
var claudeUnsupportedToolMatchers = map[string]bool{
	"WebSearch":     true, // Reasonix has no web-search tool
	"ExitPlanMode":  true, // plan approval is a controller decision, never a dispatched tool call
	"EnterPlanMode": true, // same as ExitPlanMode
	"Artifact":      true, // Reasonix has no artifact-publishing tool
}

var bareClaudeToolNamePattern = regexp.MustCompile(`^[A-Za-z_]+$`)

// claudeMatcherNeverFires reports whether matcher — a plain tool name or a
// "|"-alternation of plain tool names, the two forms Claude's own docs use —
// names only tools from claudeUnsupportedToolMatchers. A matcher using any
// other regex syntax is left unevaluated: proving a general regex can never
// match Reasonix's tool universe isn't attempted here, so this only catches
// the common, unambiguous case.
func claudeMatcherNeverFires(matcher string) bool {
	matcher = strings.TrimSpace(matcher)
	if matcher == "" || matcher == "*" {
		return false
	}
	for _, part := range strings.Split(matcher, "|") {
		part = strings.TrimSpace(part)
		if !bareClaudeToolNamePattern.MatchString(part) || !claudeUnsupportedToolMatchers[part] {
			return false
		}
	}
	return true
}

// claudeMatcherIncludesTool reports whether a Claude matcher can select the
// given tool using the same anchored-regex semantics as hook.MatchesTool.
// Empty and "*" matchers select every tool. Malformed regexes select none at
// runtime and therefore do not include the target here.
func claudeMatcherIncludesTool(matcher, toolName string) bool {
	matcher = strings.TrimSpace(matcher)
	if matcher == "" || matcher == "*" {
		return true
	}
	re, err := regexp.Compile("^(?:" + matcher + ")$")
	return err == nil && re.MatchString(toolName)
}

type claudeMCPIdentity struct {
	Type    string            `json:"type"`
	Command string            `json:"command,omitempty"`
	Args    []string          `json:"args,omitempty"`
	Env     map[string]string `json:"env,omitempty"`
	URL     string            `json:"url,omitempty"`
	Headers map[string]string `json:"headers,omitempty"`
}

// appendClaudeCompatibility maps Claude package conventions onto Reasonix's
// normalized manifest. It returns structured issues as well as compatibility
// warnings so frontends do not have to infer severity from English text.
func appendClaudeCompatibility(root string, manifest *Manifest) ([]string, []CompatibilityIssue) {
	var warnings []string
	var issues []CompatibilityIssue
	for _, rel := range []string{claudeSettingsPath, claudeHooksPath} {
		w, i := appendClaudeHooksFile(root, rel, manifest)
		warnings = append(warnings, w...)
		issues = append(issues, i...)
	}
	w, i := appendClaudeMCPFile(root, manifest)
	warnings = append(warnings, w...)
	issues = append(issues, i...)
	return uniqueSorted(warnings), issues
}

func appendClaudeHooksFile(root, rel string, manifest *Manifest) ([]string, []CompatibilityIssue) {
	path := filepath.Join(root, filepath.FromSlash(rel))
	body, err := fileencoding.ReadFileUTF8(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return compatibilityFailure("hooks", rel, err)
	}
	var raw claudeHookDocument
	if err := json.Unmarshal(body, &raw); err != nil {
		return compatibilityFailure("hooks", rel, err)
	}
	if len(raw.Hooks) == 0 {
		return nil, nil
	}
	if manifest.Hooks == nil {
		manifest.Hooks = map[string][]Hook{}
	}
	var warnings []string
	var issues []CompatibilityIssue
	var gapWebFetch, gapNotebook, gapTaskOutput bool
	for event, blocks := range raw.Hooks {
		event = strings.TrimSpace(event)
		if !claudeHookEvents[event] {
			reason := fmt.Sprintf("unsupported Claude hook event %q", event)
			warnings = append(warnings, rel+": "+reason)
			issues = append(issues, CompatibilityIssue{Capability: "hooks", Path: rel, Reason: reason})
			continue
		}
		for _, block := range blocks {
			match := firstNonEmpty(strings.TrimSpace(block.Matcher), strings.TrimSpace(block.Match))
			for _, item := range block.Hooks {
				typ := strings.TrimSpace(item.Type)
				if typ != "" && typ != "command" {
					reason := fmt.Sprintf("unsupported hook type %q for %s", typ, event)
					warnings = append(warnings, rel+": "+reason)
					issues = append(issues, CompatibilityIssue{Capability: "hooks", Path: rel, Reason: reason})
					continue
				}
				command := strings.TrimSpace(item.Command)
				if command == "" {
					continue
				}
				if ifExpr := strings.TrimSpace(item.If); ifExpr != "" {
					reason := fmt.Sprintf("%s hook %q has a conditional \"if\": %q that Reasonix does not evaluate — it runs unconditionally instead of only for the matching case", event, command, ifExpr)
					warnings = append(warnings, rel+": "+reason)
					issues = append(issues, CompatibilityIssue{Capability: "hooks", Path: rel, Reason: reason})
				}
				if item.AsyncRewake {
					mode := "synchronously"
					if item.Async {
						mode = "async, but without a wake-on-exit-2 callback"
					}
					reason := fmt.Sprintf("%s hook %q uses \"asyncRewake\", which Reasonix does not support — it runs %s instead", event, command, mode)
					warnings = append(warnings, rel+": "+reason)
					issues = append(issues, CompatibilityIssue{Capability: "hooks", Path: rel, Reason: reason})
				}
				if claudeStopBlockingEvents[event] {
					reason := fmt.Sprintf("%s hook %q cannot block the turn the way Claude's contract does — Reasonix's %s hook is observation-only and never forces the loop to continue", event, command, event)
					warnings = append(warnings, rel+": "+reason)
					issues = append(issues, CompatibilityIssue{Capability: "hooks", Path: rel, Reason: reason})
				}
				if claudeToolScopedHookEvents[event] && claudeMatcherNeverFires(match) {
					reason := fmt.Sprintf("%s hook %q matcher %q names a Claude tool Reasonix has no equivalent for, so it will never fire", event, command, match)
					warnings = append(warnings, rel+": "+reason)
					issues = append(issues, CompatibilityIssue{Capability: "hooks", Path: rel, Reason: reason})
				}
				if claudeToolScopedHookEvents[event] {
					gapWebFetch = gapWebFetch || claudeMatcherIncludesTool(match, "WebFetch")
					gapNotebook = gapNotebook || claudeMatcherIncludesTool(match, "NotebookEdit")
					gapTaskOutput = gapTaskOutput || claudeMatcherIncludesTool(match, "TaskOutput") || claudeMatcherIncludesTool(match, "BashOutput")
				}
				manifest.Hooks[event] = appendUniqueHook(manifest.Hooks[event], Hook{
					Match:         match,
					Command:       command,
					Args:          cleanStringList(item.Args),
					ShellCommand:  true,
					Async:         item.Async,
					PayloadFormat: "claude",
					Description:   firstNonEmpty(strings.TrimSpace(item.Description), "Claude-compatible hook from "+rel),
					Timeout:       claudeTimeoutMillis(item.Timeout),
					Cwd:           ".",
					Env:           cloneHookEnv(item.Env),
				})
			}
		}
	}
	// Structural input gaps (fields Reasonix cannot losslessly express) are
	// reported once per hooks file: every additional matching hook repeats
	// the same information, and a wildcard-matcher plugin would otherwise
	// collect one copy per hook. File-level wording also keeps the emitted
	// set deterministic despite the random event-map iteration order above.
	for _, gap := range []struct {
		hit    bool
		reason string
	}{
		{gapWebFetch, `a tool-scoped hook matcher includes Claude WebFetch, but Reasonix web_fetch cannot supply Claude's required "prompt" input; such hooks receive only "url" for that tool`},
		{gapNotebook, "a tool-scoped hook matcher includes Claude NotebookEdit, but Reasonix notebook_edit may target a cell by cell_number, which cannot be converted to Claude's opaque cell_id; such hooks receive cell_number as an extra field for those calls"},
		{gapTaskOutput, "a tool-scoped hook matcher includes Claude TaskOutput, but Reasonix wait may cover multiple or all background jobs in one call, which cannot be represented by Claude's single task_id; such hooks receive job_ids as an extra field for those calls"},
	} {
		if !gap.hit {
			continue
		}
		warnings = append(warnings, rel+": "+gap.reason)
		issues = append(issues, CompatibilityIssue{Capability: "hooks", Path: rel, Reason: gap.reason})
	}
	return uniqueSorted(warnings), issues
}

func appendUniqueHook(hooks []Hook, candidate Hook) []Hook {
	for _, existing := range hooks {
		if hooksEqual(existing, candidate) {
			return hooks
		}
	}
	return append(hooks, candidate)
}

// hooksEqual reports whether two hooks are duplicate declarations of the same
// invocation. Two hooks that run the same command with the same matcher and
// args but different Env, Timeout, Async, or Cwd are distinct configurations,
// not duplicates — comparing only match/command/args/payload format would
// silently drop the second one.
func hooksEqual(a, b Hook) bool {
	return a.Match == b.Match && a.Command == b.Command &&
		strings.Join(a.Args, "\x00") == strings.Join(b.Args, "\x00") &&
		a.PayloadFormat == b.PayloadFormat &&
		a.Async == b.Async && a.Timeout == b.Timeout && a.Cwd == b.Cwd &&
		maps.Equal(a.Env, b.Env)
}

func appendClaudeMCPFile(root string, manifest *Manifest) ([]string, []CompatibilityIssue) {
	path := filepath.Join(root, claudeMCPPath)
	body, err := fileencoding.ReadFileUTF8(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return compatibilityFailure("mcp", claudeMCPPath, err)
	}
	var raw struct {
		MCPServers map[string]struct {
			Type        string            `json:"type"`
			Command     string            `json:"command"`
			Args        []string          `json:"args"`
			Env         map[string]string `json:"env"`
			URL         string            `json:"url"`
			Headers     map[string]string `json:"headers"`
			Title       string            `json:"title"`
			Description string            `json:"description"`
		} `json:"mcpServers"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return compatibilityFailure("mcp", claudeMCPPath, err)
	}
	if len(raw.MCPServers) == 0 {
		return nil, nil
	}
	if manifest.MCPServers == nil {
		manifest.MCPServers = map[string]MCPServer{}
	}
	names := make([]string, 0, len(raw.MCPServers))
	for name := range raw.MCPServers {
		names = append(names, name)
	}
	sort.Strings(names)
	var warnings []string
	var issues []CompatibilityIssue
	for _, displayName := range names {
		spec := raw.MCPServers[displayName]
		typ := strings.ToLower(strings.TrimSpace(spec.Type))
		switch typ {
		case "streamable-http":
			typ = "http"
		case "local":
			typ = "stdio"
		}
		if typ == "" {
			if strings.TrimSpace(spec.URL) != "" {
				typ = "http"
			} else {
				typ = "stdio"
			}
		}
		var reason string
		switch {
		case typ != "stdio" && typ != "http" && typ != "sse":
			reason = fmt.Sprintf("MCP server %q has unsupported transport %q", displayName, spec.Type)
		case typ == "stdio" && strings.TrimSpace(spec.Command) == "":
			reason = fmt.Sprintf("MCP server %q has no command", displayName)
		case (typ == "http" || typ == "sse") && strings.TrimSpace(spec.URL) == "":
			reason = fmt.Sprintf("MCP server %q has no URL", displayName)
		}
		if reason != "" {
			warnings = append(warnings, claudeMCPPath+": "+reason)
			issues = append(issues, CompatibilityIssue{Capability: "mcp", Path: claudeMCPPath, Reason: reason})
			continue
		}
		identity := claudeMCPIdentity{
			Type: typ, Command: strings.TrimSpace(spec.Command), Args: cleanStringList(spec.Args),
			Env: cloneHookEnv(spec.Env), URL: strings.TrimSpace(spec.URL), Headers: cloneHookEnv(spec.Headers),
		}
		id := claudeMCPServerID(displayName, identity)
		if _, exists := manifest.MCPServers[id]; exists {
			reason := fmt.Sprintf("MCP server %q maps to duplicate internal name %q", displayName, id)
			warnings = append(warnings, claudeMCPPath+": "+reason)
			issues = append(issues, CompatibilityIssue{Capability: "mcp", Path: claudeMCPPath, Reason: reason})
			continue
		}
		autoStart := false
		manifest.MCPServers[id] = MCPServer{
			Type:        typ,
			Command:     strings.TrimSpace(spec.Command),
			Args:        cleanStringList(spec.Args),
			Env:         cloneHookEnv(spec.Env),
			URL:         strings.TrimSpace(spec.URL),
			Headers:     cloneHookEnv(spec.Headers),
			AutoStart:   &autoStart,
			DisplayName: firstNonEmpty(strings.TrimSpace(spec.Title), strings.TrimSpace(displayName)),
			Description: strings.TrimSpace(spec.Description),
			Imported:    true,
		}
	}
	return uniqueSorted(warnings), issues
}

func claudeMCPServerID(name string, identity claudeMCPIdentity) string {
	trimmed := strings.TrimSpace(name)
	if IsValidName(trimmed) {
		return trimmed
	}
	var b strings.Builder
	lastDash := false
	for _, r := range trimmed {
		valid := r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '-'
		if valid {
			b.WriteRune(r)
			lastDash = false
		} else if b.Len() > 0 && !lastDash {
			b.WriteByte('-')
			lastDash = true
		}
	}
	base := strings.Trim(b.String(), "-_")
	if base == "" {
		base = "server"
	}
	body, _ := json.Marshal(identity)
	h := fnv.New32a()
	_, _ = h.Write([]byte(trimmed))
	_, _ = h.Write(body)
	suffix := fmt.Sprintf("_%08x", h.Sum32())
	maxBase := 64 - len(suffix)
	if len(base) > maxBase {
		base = base[:maxBase]
	}
	return base + suffix
}

func compatibilityFailure(capability, path string, err error) ([]string, []CompatibilityIssue) {
	reason := err.Error()
	return []string{path + ": " + reason}, []CompatibilityIssue{{Capability: capability, Path: path, Reason: reason}}
}

// compatibilityFor reports what appendClaudeCompatibility could determine
// statically at install time: whether every declared capability in the
// manifest parsed and mapped to a Reasonix construct ("full"), some did not
// ("partial", see issues), or none did ("none"). It is not a guarantee that
// every runtime decision path an imported hook can take is honored — in
// particular, PreToolUse's "ask"/"defer" permissionDecision values and any
// hookSpecificOutput.updatedInput are decided by the hook script's stdout at
// call time, not by anything in the manifest, so they can't be flagged here.
// PreToolUse/PermissionRequest's "deny" and PermissionRequest's "allow" are
// the two runtime decisions Reasonix does implement (see claudeJSONDeny/
// claudeJSONAllow in internal/hook). Statically detectable gaps
// (if/asyncRewake, Stop/
// SubagentStop's inability to block the turn, WebFetch's unavailable required
// prompt input, NotebookEdit's untranslatable cell_number, and multi-job
// TaskOutput calls) already downgrade to "partial" via issues appended in
// appendClaudeHooksFile.
func compatibilityFor(pkg Package, issues []CompatibilityIssue) Compatibility {
	mapped := make([]string, 0, 5)
	skills, commands, hooks, mcp := pkg.CapabilityCounts()
	if skills > 0 {
		mapped = append(mapped, "skills")
	}
	if commands > 0 {
		mapped = append(mapped, "commands")
	}
	if pkg.AgentCount() > 0 {
		mapped = append(mapped, "agents")
	}
	if hooks > 0 {
		mapped = append(mapped, "hooks")
	}
	if mcp > 0 {
		mapped = append(mapped, "mcp")
	}
	status := "full"
	if len(mapped) == 0 && pkg.ManifestKind != "reasonix" {
		status = "none"
	} else if len(issues) > 0 {
		status = "partial"
	}
	return Compatibility{Status: status, Mapped: mapped, Skipped: issues}
}

func dirContainsAgentMd(dir string) bool { return len(loadAgentRefs(dir)) > 0 }

func (p Package) agentRefs() []AgentRef {
	var out []AgentRef
	for _, root := range p.AgentRoots() {
		out = append(out, loadAgentRefs(root)...)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func loadAgentRefs(dir string) []AgentRef {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []AgentRef
	for _, entry := range entries {
		if entry.IsDir() || !strings.EqualFold(filepath.Ext(entry.Name()), ".md") {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		body, err := fileencoding.ReadFileUTF8(path)
		if err != nil {
			continue
		}
		fm, _ := frontmatter.Split(string(body))
		name := strings.TrimSuffix(entry.Name(), filepath.Ext(entry.Name()))
		if declared := strings.TrimSpace(fm["name"]); IsValidName(declared) {
			name = declared
		}
		if !IsValidName(name) {
			continue
		}
		out = append(out, AgentRef{
			Name:         name,
			Description:  strings.TrimSpace(fm["description"]),
			Path:         path,
			Invocation:   "/" + name,
			Model:        strings.TrimSpace(fm["model"]),
			AllowedTools: splitCSV(fm["tools"]),
		})
	}
	return out
}

func splitCSV(raw string) []string {
	raw = strings.TrimSpace(raw)
	if strings.HasPrefix(raw, "[") && strings.HasSuffix(raw, "]") {
		raw = strings.TrimSpace(raw[1 : len(raw)-1])
	}
	var out []string
	for _, item := range strings.Split(raw, ",") {
		if item = strings.Trim(strings.TrimSpace(item), `"'`); item != "" {
			out = append(out, item)
		}
	}
	return out
}

func cleanStringList(in []string) []string {
	out := make([]string, 0, len(in))
	for _, value := range in {
		if value = strings.TrimSpace(value); value != "" {
			out = append(out, value)
		}
	}
	return out
}

func uniqueSorted(in []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(in))
	for _, value := range in {
		if value = strings.TrimSpace(value); value != "" && !seen[value] {
			seen[value] = true
			out = append(out, value)
		}
	}
	sort.Strings(out)
	return out
}
