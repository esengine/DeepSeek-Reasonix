// Package installsource: install_source.go is the tool entrypoint. It
// defines the public Options/Execute surface, the JSON Schema, and the
// end-to-end pipeline that turns a request into a plan and (optionally)
// into a series of apply calls.
package installsource

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"reasonix/internal/config"
	"reasonix/internal/skill"
	"reasonix/internal/tool"
)

// MCPConnectResult is what the ConnectMCP callback returns. Disconnect is
// optional; when non-nil, the apply step will call it to undo a connect
// whose persistence (SaveTo) failed — closing the "ghost install" window.
type MCPConnectResult struct {
	ToolCount  int
	Disconnect func() // optional; nil means rollback is not possible
}

// MCPConnector is the host-provided hook that turns a PluginEntry into a
// live MCP connection. The returned Disconnect, if any, is used by the
// install_source tool to roll back a failed persistence step.
type MCPConnector func(config.PluginEntry) (MCPConnectResult, error)

// ApprovalFunc is invoked between plan and apply when apply=true. Return
// nil to allow the install, or a non-nil error to refuse it. The action
// list reflects the exact set the apply step is about to perform; a host
// (e.g. the desktop TUI) can show it to the user and decide synchronously.
type ApprovalFunc func(actions []action) error

// OnDisconnectFunc tells the host to remove a server from the live session and
// drop the corresponding mcp__<name>__ tools from its Registry. It returns true
// when a live server was actually removed, letting replace/rollback restore the
// old connection only when there was one.
type OnDisconnectFunc func(serverName string) bool

// Options configure the install_source tool. ProjectRoot "" and HomeDir
// "" fall back to os.Getwd / os.UserHomeDir at construction time.
type Options struct {
	ProjectRoot  string
	HomeDir      string
	HTTPClient   *http.Client
	ConnectMCP   MCPConnector
	OnDisconnect OnDisconnectFunc
	Approval     ApprovalFunc
}

type installSourceTool struct {
	root         string
	home         string
	httpClient   *http.Client
	connectMCP   MCPConnector
	onDisconnect OnDisconnectFunc
	approval     ApprovalFunc
}

// NewTool returns a tool.Tool that callers register with the agent's
// Registry. The returned tool is safe to call from any goroutine; the
// underlying config/config.SaveTo paths do their own per-file locking.
func NewTool(opts Options) tool.Tool {
	root := opts.ProjectRoot
	if root == "" {
		if wd, err := currentDir(); err == nil {
			root = wd
		}
	}
	if abs, err := filepath.Abs(root); err == nil {
		root = abs
	}
	home := opts.HomeDir
	if home == "" {
		if h, err := userHomeDir(); err == nil {
			home = h
		}
	}
	client := opts.HTTPClient
	if client == nil {
		client = &http.Client{}
	}
	// install_source fetches untrusted URLs (SKILL.md, .mcp.json, GitHub
	// manifests); guard the dial against SSRF the same way web_fetch does, so a
	// prompt-injected source can't reach cloud metadata / internal services.
	client = ssrfGuardClient(client)
	return &installSourceTool{
		root:         root,
		home:         home,
		httpClient:   client,
		connectMCP:   opts.ConnectMCP,
		onDisconnect: opts.OnDisconnect,
		approval:     opts.Approval,
	}
}

func (*installSourceTool) Name() string   { return "install_source" }
func (*installSourceTool) ReadOnly() bool { return false }

func (*installSourceTool) Description() string {
	return "Plan, install, or uninstall a Reasonix skill or MCP server from a URL, local file/folder, .mcp.json, executable, or package name. Two-phase: with apply=false (default) returns a deterministic plan with per-action risk level; with apply=true copies/registers skills or connects and persists MCP servers after validation. op='uninstall' removes a previously installed skill (by name) or MCP server (by name) from the active config and the live session."
}

func (*installSourceTool) Schema() json.RawMessage {
	return json.RawMessage(`{
"type":"object",
"properties":{
  "op":{"type":"string","enum":["install","uninstall"],"description":"Whether to install (default) or uninstall."},
  "source":{"type":"string","description":"URL, local file/folder path, .mcp.json path, or package name to install from. Ignored when op=uninstall (use name instead)."},
  "kind":{"type":"string","enum":["auto","skill","mcp"],"description":"Capability kind. Defaults to auto."},
  "apply":{"type":"boolean","description":"false (default) only returns an install plan; true performs the planned writes/connects. Ignored for op=uninstall."},
  "scope":{"type":"string","enum":["project","global"],"description":"Where to persist config or copy skills. Defaults to project when a workspace exists, otherwise global."},
  "mode":{"type":"string","enum":["auto","copy","link","register"],"description":"Skill install mode. auto registers multi-skill roots and copies single skills into the canonical <skill-name>/SKILL.md layout; copy copies skill files/folders; link creates symlinks; register adds a skill root to [skills].paths."},
  "name":{"type":"string","description":"Optional override for the installed MCP server or single skill name. Required for op=uninstall when removing by name."},
  "transport":{"type":"string","enum":["auto","stdio","http","sse"],"description":"MCP transport override. URL sources default to http unless --sse-like; package sources default to stdio."},
  "command":{"type":"string","description":"Optional stdio MCP command override for package/local executable installs."},
  "args":{"type":"array","items":{"type":"string"},"description":"Optional stdio MCP args override."},
  "env":{"type":"object","additionalProperties":{"type":"string"},"description":"Environment variables for stdio MCP servers."},
  "headers":{"type":"object","additionalProperties":{"type":"string"},"description":"HTTP headers for remote MCP servers. Prefer ${VAR} placeholders for secrets."},
  "tier":{"type":"string","enum":["lazy","background","eager"],"description":"Persisted MCP startup tier. Defaults to lazy."},
  "replace":{"type":"boolean","description":"Allow replacing an existing MCP config entry with the same name. Skills still refuse to overwrite existing files."},
  "strict":{"type":"boolean","description":"Skill install strictness. true (default) requires name+description frontmatter; false copies the file as-is (use only for files you trust)."},
  "planId":{"type":"string","description":"Optional. Echoed from a previous planned response to confirm the host is approving the same plan."}
},
"required":[]
}`)
}

// Execute parses args, plans, and (if apply=true and Approval allows)
// performs the writes. JSON output is always returned on success even when
// the plan is empty, so the model can read structured `next` hints.
func (t *installSourceTool) Execute(ctx context.Context, raw json.RawMessage) (string, error) {
	var req request
	if err := json.Unmarshal(raw, &req); err != nil {
		return "", fmt.Errorf("install_source: invalid args: %w", err)
	}
	req.Source = strings.TrimSpace(req.Source)
	if req.Op == "" {
		req.Op = "install"
	}
	if req.Op != "install" && req.Op != "uninstall" {
		return "", fmt.Errorf("install_source: op %q is not supported (want install|uninstall)", req.Op)
	}
	if req.Op == "install" && req.Source == "" {
		return "", errors.New("install_source requires a non-empty source")
	}
	if req.Op == "uninstall" && strings.TrimSpace(req.Name) == "" {
		return "", errors.New("install_source: op=uninstall requires a non-empty name")
	}
	req.Kind = normalizeKind(req.Kind)
	req.Scope = t.normalizeScope(req.Scope)
	req.Mode = normalizeMode(req.Mode)
	req.Transport = normalizeTransport(req.Transport)
	if norm, ok := normalizeTier(req.Tier); ok {
		req.Tier = norm
	}

	if req.Op == "uninstall" {
		return t.executeUninstall(req), nil
	}

	actions, warnings, err := t.plan(ctx, req)
	if err != nil {
		return "", err
	}
	planID := computePlanID(req, actions)
	if len(actions) == 0 {
		out := response{
			OK:       false,
			Status:   "blocked",
			Op:       req.Op,
			Applied:  false,
			Source:   req.Source,
			Kind:     "",
			Scope:    req.Scope,
			Mode:     req.Mode,
			PlanID:   planID,
			Warnings: warnings,
			Next:     "No installable Reasonix skill or MCP server was detected. Ask the user for a direct SKILL.md, skill root, .mcp.json, MCP endpoint, or package name.",
		}
		return marshalJSON(out), nil
	}

	if !req.Apply {
		for i := range actions {
			actions[i].Status = "planned"
		}
		out := response{
			OK:       true,
			Status:   "planned",
			Op:       req.Op,
			Applied:  false,
			Source:   req.Source,
			Kind:     summarizeKind(actions),
			Kinds:    kindCounts(actions),
			Scope:    req.Scope,
			Mode:     req.Mode,
			PlanID:   planID,
			Actions:  publicActions(actions),
			Warnings: warnings,
			Next:     "Review the plan (especially each action's riskLevel). Call install_source again with apply=true and the same planId to install.",
		}
		return marshalJSON(out), nil
	}

	if req.PlanID != "" && req.PlanID != planID {
		return "", newErr(ErrApprovalDenied, "planId mismatch (got %s, expected %s); re-plan and re-approve", req.PlanID, planID)
	}
	if t.approval != nil {
		if err := t.approval(publicActions(actions)); err != nil {
			return marshalJSON(response{
				OK:       false,
				Status:   "denied",
				Op:       req.Op,
				Applied:  false,
				Source:   req.Source,
				Kind:     summarizeKind(actions),
				Kinds:    kindCounts(actions),
				Scope:    req.Scope,
				Mode:     req.Mode,
				PlanID:   planID,
				Actions:  publicActions(actions),
				Warnings: append(warnings, "host approval was denied: "+err.Error()),
				Next:     "Ask the user to confirm, or run with a less risky plan (e.g. lower scope, fewer actions).",
			}), nil
		}
	}

	return t.executeApply(ctx, req, actions, warnings, planID), nil
}

// executeApply runs the apply phase. The first failed action short-circuits
// the rest only when a single failure implies the plan is unusable; for
// MCP installs in particular, partial completion is reported honestly.
func (t *installSourceTool) executeApply(ctx context.Context, req request, actions []action, warnings []string, planID string) string {
	ok := true
	anySucceeded := false
	for i := range actions {
		if err := t.apply(ctx, req, &actions[i]); err != nil {
			ok = false
			actions[i].Status = "failed"
			actions[i].Error = err.Error()
			if actions[i].Next == "" {
				actions[i].Next = nextForError(err)
			}
			continue
		}
		actions[i].Status = "done"
		anySucceeded = true
		warnings = append(warnings, actions[i].Warnings...)
	}
	status := "done"
	next := "Installed and verified."
	if !ok {
		if anySucceeded {
			status = "partial"
			next = "Some actions succeeded; the failed ones are listed in actions[].status=failed. Re-plan those and retry."
		} else {
			status = "failed"
			next = "No action succeeded. Fix the first failed action[] entry and retry install_source with apply=true."
		}
	}
	return marshalJSON(response{
		OK:       ok,
		Status:   status,
		Op:       req.Op,
		Applied:  true,
		Source:   req.Source,
		Kind:     summarizeKind(actions),
		Kinds:    kindCounts(actions),
		Scope:    req.Scope,
		Mode:     req.Mode,
		PlanID:   planID,
		Actions:  publicActions(actions),
		Warnings: warnings,
		Next:     next,
	})
}

// executeUninstall handles op=uninstall. It locates the named entry in the
// active config (skills via the on-disk layout, MCP via cfg.Plugins) and
// asks the host to disconnect. We do not consult the approval hook for
// uninstall: the user already named the entry, and removal is the inverse
// of the install they authorized.
func (t *installSourceTool) executeUninstall(req request) string {
	scope := req.Scope
	actions := []action{}
	cfgPath := t.configPath(scope)
	cfg := config.LoadForEdit(cfgPath)

	// Skills: try the flat file, then the directory layout, in the chosen
	// scope. We don't require a kind — "name" disambiguates.
	if path, ok := t.resolveSkillPath(req.Name, scope); ok {
		actions = append(actions, action{
			Kind:       "skill",
			Action:     "remove_skill",
			Name:       req.Name,
			Target:     path,
			Scope:      scope,
			ConfigPath: cfgPath,
			RiskLevel:  RiskLow,
		})
	} else if rootAction, ok := t.resolveRegisteredSkillRoot(req.Name, scope, cfgPath, cfg); ok {
		actions = append(actions, rootAction)
	}

	// MCP: scan the chosen config for the named plugin.
	for _, p := range cfg.Plugins {
		if p.Name == req.Name {
			actions = append(actions, action{
				Kind:       "mcp",
				Action:     "remove_mcp_server",
				Name:       p.Name,
				Target:     p.URL,
				Scope:      scope,
				Transport:  pluginTransport(p),
				ConfigPath: cfgPath,
				RiskLevel:  RiskMedium,
				RiskReasons: []string{
					"disconnects a running server and drops its tools from the active session",
				},
			})
			break
		}
	}

	if len(actions) == 0 {
		return marshalJSON(response{
			OK:      false,
			Status:  "blocked",
			Op:      req.Op,
			Applied: false,
			Source:  req.Source,
			Name:    req.Name,
			Scope:   scope,
			Next:    "No installed skill or MCP server matched that name in the chosen scope.",
		})
	}

	// Uninstall is destructive but symmetric with a previously approved
	// install, so we apply directly. Each action is independent.
	ok := true
	anySucceeded := false
	for i := range actions {
		if err := t.apply(context.Background(), req, &actions[i]); err != nil {
			ok = false
			actions[i].Status = "failed"
			actions[i].Error = err.Error()
			actions[i].Next = "Inspect the error, then retry op=uninstall."
			continue
		}
		actions[i].Status = "done"
		anySucceeded = true
	}
	status := "done"
	if !ok {
		status = "partial"
		if !anySucceeded {
			status = "failed"
		}
	}
	return marshalJSON(response{
		OK:      ok,
		Status:  status,
		Op:      req.Op,
		Applied: true,
		Source:  req.Source,
		Name:    req.Name,
		Kind:    summarizeKind(actions),
		Kinds:   kindCounts(actions),
		Scope:   scope,
		Actions: publicActions(actions),
		Next:    "Removed.",
	})
}

// resolveSkillPath finds the on-disk location of a previously installed
// skill of the given name in the chosen scope. The bool reports whether
// the path is a real install (Lstat succeeded). Both flat (<name>.md) and
// directory (<name>/) layouts are checked.
func (t *installSourceTool) resolveSkillPath(name, scope string) (string, bool) {
	if !config.IsValidSkillName(name) {
		return "", false
	}
	var root string
	if scope == "global" {
		if t.home == "" {
			return "", false
		}
		root = filepath.Join(t.home, ".reasonix", skill.SkillsDirname)
	} else {
		root = filepath.Join(t.root, ".reasonix", skill.SkillsDirname)
	}
	flat := filepath.Join(root, name+".md")
	if _, err := lstat(flat); err == nil {
		return flat, true
	}
	dir := filepath.Join(root, name)
	if _, err := lstat(filepath.Join(dir, skill.SkillFile)); err == nil {
		return dir, true
	}
	return "", false
}

func (t *installSourceTool) resolveRegisteredSkillRoot(name, scope, cfgPath string, cfg *config.Config) (action, bool) {
	if !config.IsValidSkillName(name) {
		return action{}, false
	}
	for _, rawPath := range cfg.Skills.Paths {
		path := t.resolvePath(config.ExpandVars(rawPath))
		cands, err := scanSkillRoot(path, false)
		if err != nil {
			continue
		}
		var names []string
		found := false
		for _, cand := range cands {
			names = append(names, cand.Name)
			if cand.Name == name {
				found = true
			}
		}
		if !found {
			continue
		}
		sort.Strings(names)
		return action{
			Kind:       "skill",
			Action:     "remove_skill_root",
			Name:       name,
			Target:     rawPath,
			Scope:      scope,
			ConfigPath: cfgPath,
			Skills:     names,
			SkillCount: len(names),
			RiskLevel:  RiskMedium,
			RiskReasons: []string{
				"removes a registered skill root from [skills].paths and may hide every skill in that folder",
			},
		}, true
	}
	return action{}, false
}

func (t *installSourceTool) configPath(scope string) string {
	if scope == "global" {
		if p := config.UserConfigPath(); p != "" {
			return p
		}
	}
	return filepath.Join(t.root, "reasonix.toml")
}

func (t *installSourceTool) normalizeScope(scope string) string {
	switch strings.ToLower(strings.TrimSpace(scope)) {
	case "global":
		return "global"
	default:
		if strings.TrimSpace(t.root) != "" {
			return "project"
		}
		return "global"
	}
}

func (t *installSourceTool) resolvePath(p string) string {
	p = strings.TrimSpace(p)
	if strings.HasPrefix(p, "~/") || strings.HasPrefix(p, `~\`) {
		p = filepath.Join(t.home, p[2:])
	} else if p == "~" {
		p = t.home
	}
	if !filepath.IsAbs(p) {
		p = filepath.Join(t.root, p)
	}
	if abs, err := filepath.Abs(p); err == nil {
		p = abs
	}
	return filepath.Clean(p)
}

// computePlanID hashes the request plus the full public action set so a later
// apply call with the same planId can be verified to be approving exactly the
// same plan. It intentionally excludes Apply and PlanID; everything that changes
// what will be written/connected must live either in req's planning fields or in
// the action DTO.
func computePlanID(req request, actions []action) string {
	public := publicActions(actions)
	sort.Slice(public, func(i, j int) bool {
		return actionPlanKey(public[i]) < actionPlanKey(public[j])
	})
	payload := struct {
		Op        string            `json:"op"`
		Source    string            `json:"source"`
		Kind      string            `json:"kind"`
		Scope     string            `json:"scope"`
		Mode      string            `json:"mode"`
		Name      string            `json:"name"`
		Transport string            `json:"transport"`
		Command   string            `json:"command"`
		Args      []string          `json:"args,omitempty"`
		Env       map[string]string `json:"env,omitempty"`
		Headers   map[string]string `json:"headers,omitempty"`
		Tier      string            `json:"tier"`
		Replace   bool              `json:"replace"`
		Strict    bool              `json:"strict"`
		Actions   []action          `json:"actions"`
	}{
		Op:        req.Op,
		Source:    req.Source,
		Kind:      req.Kind,
		Scope:     req.Scope,
		Mode:      req.Mode,
		Name:      req.Name,
		Transport: req.Transport,
		Command:   req.Command,
		Args:      req.Args,
		Env:       req.Env,
		Headers:   req.Headers,
		Tier:      req.Tier,
		Replace:   req.Replace,
		Strict:    req.strict(),
		Actions:   public,
	}
	body, _ := json.Marshal(payload)
	h := sha256.New()
	h.Write(body)
	return "sha256:" + hex.EncodeToString(h.Sum(nil)[:16])
}

// kindCounts tallies the per-kind action count for the response. Skill
// skills and MCP servers in the same plan get separate counts so the
// caller can summarize accurately.
func kindCounts(actions []action) kindTally {
	var out kindTally
	for _, a := range actions {
		switch a.Kind {
		case "skill":
			out.Skill++
		case "mcp":
			out.MCP++
		}
	}
	return out
}

// nextForError maps a sentinel error to a short remediation hint. Callers
// use it as the default `next` value when a plan step fails.
func nextForError(err error) string {
	switch {
	case errors.Is(err, ErrAuthRequired):
		return "Authentication is required. Add the needed token as an environment variable or header placeholder, then retry."
	case errors.Is(err, ErrBinaryMissing):
		return "Install the missing local runtime or use an absolute command path, then retry."
	case errors.Is(err, ErrAlreadyExists):
		return "Choose another name, remove the existing entry, or retry MCP installs with replace=true."
	case errors.Is(err, ErrUnsafeLinkTarget):
		return "The link target escapes the project/home root. Pick a source path inside the workspace or home directory."
	case errors.Is(err, ErrApprovalDenied):
		return "Host denied the install. Re-run without apply=true to revise the plan, or ask the user to confirm."
	case errors.Is(err, ErrManifestMissing):
		return "No installable manifest was found at the source. Provide a direct SKILL.md, .mcp.json, executable, or package name."
	case errors.Is(err, ErrInvalidManifest):
		return "The manifest was found but did not validate. Check required fields (command/url/tier)."
	case errors.Is(err, ErrSourceUnreadable):
		return "The source could not be read. Check the URL/path and try again."
	default:
		return "Inspect the error, fix the source or environment, then retry."
	}
}

// currentDir / userHomeDir / lstat are tiny wrappers that exist so tests
// can stub them; the wrappers today just call the stdlib versions.
var (
	currentDir  = defaultCurrentDir
	userHomeDir = defaultUserHomeDir
	lstat       = defaultLstat
)

func defaultCurrentDir() (string, error)            { return os.Getwd() }
func defaultUserHomeDir() (string, error)           { return os.UserHomeDir() }
func defaultLstat(path string) (os.FileInfo, error) { return os.Lstat(path) }
