package control

import (
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"reasonix/internal/contract/config"
	"reasonix/internal/ext/mcpsetup"
	"reasonix/internal/ext/plugin"
)

// MCPScope is how far an installed server reaches. User follows the person
// across projects; Project follows the repository, which means it also reaches
// everyone who clones it. Local is the person's own server in one project only:
// installing is a global act either way, so it declares the server globally and
// then answers the "is it on" question per layer — off everywhere, on here.
type MCPScope string

const (
	MCPScopeUser    MCPScope = "user"
	MCPScopeProject MCPScope = "project"
	MCPScopeLocal   MCPScope = "local"
)

// InstallMCPServer connects a candidate server and persists it only once the
// handshake proves it works — a saved entry that never connects reads as
// installed while contributing no tools. A server that needs authentication is
// the exception: its config is kept, because completing OAuth and retrying is
// impossible once the entry is gone.
func (c *Controller) InstallMCPServer(e config.PluginEntry, scope MCPScope) (plugin.MCPInstallResult, error) {
	e.Name = strings.TrimSpace(e.Name)
	if e.Name == "" {
		return plugin.InstallResultForError("", fmt.Errorf("需要一个服务名")), nil
	}
	if existing, err := c.configuredMCPServer(e.Name); err == nil {
		return plugin.InstallResultForError(e.Name,
			fmt.Errorf("已经有一个叫 %q 的服务了（来自 %s）", e.Name, existing.Source)), nil
	}
	if scope == MCPScopeProject || scope == MCPScopeLocal {
		if strings.TrimSpace(c.workspaceRoot) == "" {
			return plugin.InstallResultForError(e.Name, fmt.Errorf("没有打开项目，装不了项目级的服务")), nil
		}
	}
	if scope == MCPScopeProject {
		e.Source = config.MCPSourceProjectConfig
	} else {
		e.Source = config.MCPSourceUserConfig
	}

	toolCount, connErr := c.connectMCPServer(e)
	if connErr != nil {
		result := plugin.InstallResultForError(e.Name, connErr)
		if result.State != "action_required" {
			// Nothing was persisted, so the name must not linger in the failure
			// list either — it would show up as a server the user never installed.
			if h := c.mcp.hostRef(); h != nil {
				h.ClearFailure(e.Name)
			}
			c.mcp.disconnect(e.Name)
			c.mcp.removeToolPrefix(e.Name)
			return result, nil
		}
		if err := c.persistMCPServer(e); err != nil {
			return plugin.MCPInstallResult{}, err
		}
		if err := c.confineMCPToThisProject(e, scope); err != nil {
			c.DisconnectMCPServer(e.Name)
			return plugin.MCPInstallResult{}, errors.Join(err, c.rollbackMCPServer(e.Name))
		}
		return result, nil
	}
	if err := c.persistMCPServer(e); err != nil {
		c.DisconnectMCPServer(e.Name)
		return plugin.MCPInstallResult{}, err
	}
	if err := c.confineMCPToThisProject(e, scope); err != nil {
		c.DisconnectMCPServer(e.Name)
		return plugin.MCPInstallResult{}, errors.Join(err, c.rollbackMCPServer(e.Name))
	}
	return plugin.ReadyInstallResult(e.Name, toolCount), nil
}

// confineMCPToThisProject turns a global declaration into a local one by
// answering the activation question per layer. Nothing else stores a
// per-project install, and nothing else needs to.
func (c *Controller) confineMCPToThisProject(e config.PluginEntry, scope MCPScope) error {
	if scope != MCPScopeLocal {
		return nil
	}
	store := config.DefaultActivationStore()
	if err := store.SetServerEnabled(e, c.workspaceRoot, config.ActivationGlobal, false); err != nil {
		return err
	}
	return store.SetServerEnabled(e, c.workspaceRoot, config.ActivationProject, true)
}

// persistMCPServer writes the declaration and its activation together, rolling
// the declaration back if activation fails: a server present in config but
// missing from the activation store resolves by auto_start, which is not what
// the user just chose.
func (c *Controller) persistMCPServer(e config.PluginEntry) error {
	var err error
	if e.Source == config.MCPSourceUserConfig {
		_, err = config.InstallUserPluginForRoot(c.workspaceRoot, e, true)
	} else {
		_, err = config.UpsertPluginInSourceForRoot(c.workspaceRoot, e)
	}
	if err != nil {
		return fmt.Errorf("保存配置: %w", err)
	}
	if e.Source == config.MCPSourceProjectConfig {
		// The decision covers the declaration as the file now holds it.
		if written, loadErr := c.configuredMCPServer(e.Name); loadErr == nil {
			e = written
		}
	}
	store := config.DefaultActivationStore()
	if !e.ShouldAutoStart() {
		if clearErr := store.ClearServer(e, c.workspaceRoot, config.ActivationGlobal); clearErr != nil {
			return errors.Join(clearErr, c.rollbackMCPServer(e.Name))
		}
		return nil
	}
	if setErr := store.SetServerEnabled(e, c.workspaceRoot, config.ActivationGlobal, true); setErr != nil {
		return errors.Join(setErr, c.rollbackMCPServer(e.Name))
	}
	return nil
}

func (c *Controller) rollbackMCPServer(name string) error {
	_, _, _, err := config.RemovePluginFromEffectiveSourceForRoot(c.workspaceRoot, name)
	return err
}

// MCPServerState is one configured server's declaration paired with the
// activation switch that decides whether this session may use it at all.
// LocalOverride marks a switch this project set for itself rather than
// inheriting, which the settings surface shows as a local exception.
type MCPServerState struct {
	Entry         config.PluginEntry
	Enabled       bool
	LocalOverride bool
	// What the server itself said, recovered from the schema cache the last
	// handshake wrote: a server that is off has no live answer, and the
	// alternative is a row that can only repeat its own name back.
	Description string
	Tools       []plugin.ToolInfo
	Stale       bool // the declaration changed since that cache was written
	// Pending marks a repository-declared server awaiting launch approval;
	// Changed, one the user enabled for a declaration it no longer holds.
	Pending bool
	Changed bool
	// AlwaysLoad is what config asks for now; InSchema is whether this
	// session's provider schema carries the server's tools, fixed at its start.
	AlwaysLoad bool
	InSchema   bool
}

// ConfiguredMCPServers lists every configured server with its resolved
// activation state, in config order. One config read and one activation read
// answer the whole list, which the per-name accessors cannot do.
func (c *Controller) ConfiguredMCPServers() []MCPServerState {
	cfg, err := config.LoadForRootReadOnly(c.workspaceRoot)
	if err != nil {
		return nil
	}
	store := config.DefaultActivationStore()
	overrides, _ := store.ProjectOverrides(c.workspaceRoot)
	local := make(map[string]bool, len(overrides))
	for _, row := range overrides {
		if row.Kind == config.CapabilityMCP {
			local[row.Name] = true
		}
	}
	inSchema := c.mcpServersInSchema()
	out := make([]MCPServerState, 0, len(cfg.Plugins))
	for _, p := range cfg.Plugins {
		enabled, err := store.IsEnabled(p, c.workspaceRoot)
		if err != nil {
			enabled = config.DeclaredDefaultOn(p)
		}
		state := MCPServerState{
			Entry: p, Enabled: enabled, LocalOverride: local[p.Name],
			Pending:    store.AwaitingDecision(p, c.workspaceRoot),
			Changed:    store.ServerChanged(p, c.workspaceRoot),
			AlwaysLoad: cfg.MCPAlwaysLoad(p), InSchema: inSchema[p.Name],
		}
		state.Description, state.Tools, state.Stale = mcpCachedFacts(c.mcpSpec(p))
		out = append(out, state)
	}
	return out
}

// MCPCatalogTools counts, per server name, the tools that server currently has
// in this session's catalog. It answers the question a status row is really
// asking — can the model call this now — which "is a process running" does not:
// a cache-hit server stays process-idle until its first call with every one of
// its tools already callable.
func (c *Controller) MCPCatalogTools() map[string]int { return c.mcp.catalogTools() }

// ErrMCPApprovalOwed is a connect asked for a project server whose declaration
// changed since the user enabled it. Connecting by name is not an answer to
// that: the new command has to be shown and approved through enable.
var ErrMCPApprovalOwed = errors.New("project MCP server needs approval again")

// MCPApprovalOwedError carries what the refused connect would have run.
type MCPApprovalOwedError struct {
	Name, Launch string
	Reason       MCPApprovalReason
}

func (e *MCPApprovalOwedError) Error() string {
	name := mcpsetup.DisplayName(e.Name)
	return fmt.Sprintf("MCP server %q: %s; run `reasonix mcp enable %s` (or switch it on in Settings) to approve: %s",
		name, e.Reason.Text(), name, e.Launch)
}

func (e *MCPApprovalOwedError) Is(target error) bool { return target == ErrMCPApprovalOwed }

// approveOnExplicitConnect answers for a server a connect names. A
// repository-declared server that is not enabled is refused with what it would
// run: a connect by name shows no command, enable does. A waiting server from
// the user's own files is approved by the connect, recorded for next session.
func (c *Controller) approveOnExplicitConnect(entry config.PluginEntry) error {
	store := config.DefaultActivationStore()
	if config.RepositoryDeclared(entry) {
		if enabled, err := store.IsEnabled(entry, c.workspaceRoot); err == nil && enabled {
			return nil
		}
		reason := MCPApprovalAwaiting
		switch {
		case store.ServerChanged(entry, c.workspaceRoot):
			reason = MCPApprovalChanged
		case !store.AwaitingDecision(entry, c.workspaceRoot):
			reason = MCPApprovalDisabled
		}
		return &MCPApprovalOwedError{Name: entry.Name, Launch: mcpsetup.LaunchLine(entry), Reason: reason}
	}
	if !store.AwaitingDecision(entry, c.workspaceRoot) {
		return nil
	}
	if err := store.SetServerEnabled(entry, c.workspaceRoot, config.ActivationProject, true); err != nil {
		slog.Warn("mcp: connect could not record the approval", "server", entry.Name, "err", err)
	}
	return nil
}

// ReconnectMCPServer retries one configured server and re-registers its tools.
// The recorded failure is cleared first: a failed name is absent from Servers()
// until the record goes, so a successful retry would still read as broken.
func (c *Controller) ReconnectMCPServer(name string) (int, error) {
	entry, err := c.configuredMCPServer(strings.TrimSpace(name))
	if err != nil {
		return 0, err
	}
	if err := c.approveOnExplicitConnect(entry); err != nil {
		return 0, err
	}
	if h := c.mcp.hostRef(); h != nil {
		h.ClearFailure(entry.Name)
	}
	c.mcp.disconnect(entry.Name)
	n, connErr := c.connectMCPServer(entry)
	if connErr != nil {
		// Without a record the server falls back to "configured but idle", which
		// reads as never attempted rather than attempted and still broken.
		if h := c.mcp.hostRef(); h != nil {
			h.RecordFailure(c.mcpSpec(entry), connErr)
		}
		return 0, connErr
	}
	return n, nil
}

// MCPServerEnabled resolves one configured server's durable activation state.
func (c *Controller) MCPServerEnabled(name string) (bool, error) {
	entry, err := c.configuredMCPServer(strings.TrimSpace(name))
	if err != nil {
		return false, err
	}
	return config.DefaultActivationStore().IsEnabled(entry, c.workspaceRoot)
}

// The two things that can go wrong once the switch is persisted, kept apart
// because they leave the user in different places: the server would not start
// and the durable state was put back, or it would not start and the state could
// not be put back either — which is persisted state disagreeing with the
// runtime, and the only one of the two worth interrupting anybody about.
var (
	ErrMCPUnavailable  = errors.New("the server would not start")
	ErrSwitchNotUndone = errors.New("the durable switch could not be put back")
)

// SetMCPServerEnabled persists the switch at scope and moves this session's
// tool registry with it. Enabling restores the cached tool surface without
// starting a process, so it stays cheap for a lazy server. A registry failure
// rolls the persisted state back: a switch that survives a restart but changed
// nothing now is a lie.
func (c *Controller) SetMCPServerEnabled(name string, scope config.ActivationScope, enabled bool) error {
	entry, err := c.configuredMCPServer(strings.TrimSpace(name))
	if err != nil {
		return err
	}
	store := config.DefaultActivationStore()
	prev, hadPrev, err := store.ServerOverride(entry, c.workspaceRoot, scope)
	if err != nil {
		return err
	}
	if err := store.SetServerEnabled(entry, c.workspaceRoot, scope, enabled); err != nil {
		return err
	}
	if !enabled {
		c.DisconnectMCPServer(entry.Name)
		return nil
	}
	if _, err := c.RegisterMCPServerOnDemand(entry); err != nil {
		unavailable := fmt.Errorf("%w: %w", ErrMCPUnavailable, err)
		undo := store.ClearServer(entry, c.workspaceRoot, scope)
		if hadPrev {
			undo = store.SetOverride(prev)
		}
		if undo != nil {
			return errors.Join(unavailable, fmt.Errorf("%w: %w", ErrSwitchNotUndone, undo))
		}
		return unavailable
	}
	return nil
}

// ClearMCPServerOverride drops this project's exception for name, returning it
// to whatever the global layer and its own declaration say.
func (c *Controller) ClearMCPServerOverride(name string, scope config.ActivationScope) error {
	entry, err := c.configuredMCPServer(strings.TrimSpace(name))
	if err != nil {
		return err
	}
	if err := config.DefaultActivationStore().ClearServer(entry, c.workspaceRoot, scope); err != nil {
		return err
	}
	enabled, err := config.DefaultActivationStore().IsEnabled(entry, c.workspaceRoot)
	if err != nil {
		return err
	}
	if !enabled {
		c.DisconnectMCPServer(entry.Name)
		return nil
	}
	if _, err := c.RegisterMCPServerOnDemand(entry); err != nil {
		return fmt.Errorf("%w: %w", ErrMCPUnavailable, err)
	}
	return nil
}

// mcpIdentitySpec builds the part of a server's spec that decides which server
// it is: what gets launched or dialled, where, and with which named inputs.
// Timeouts and process mode are how this session runs it and change nothing
// about its identity, so the schema cache — which is keyed on exactly these
// fields — can be reached from a project the session is not pointed at.
func mcpIdentitySpec(e config.PluginEntry, root string) plugin.Spec {
	exp := e.ExpandedPluginForRoot(root)
	spec := plugin.ApplyKnownOverrides(plugin.Spec{
		Name:          exp.Name,
		Type:          exp.Type,
		Command:       exp.Command,
		Args:          exp.Args,
		Env:           exp.Env,
		URL:           exp.URL,
		Headers:       exp.Headers,
		DisabledTools: append([]string(nil), exp.DisabledTools...),
		WorkspaceRoot: root,
		ConfigSource:  strings.TrimSpace(string(exp.Source)),
	}, root)
	if exp.Source.ProjectScoped() && strings.TrimSpace(spec.Dir) == "" {
		spec.Dir = root
	}
	return spec
}

// mcpCachedFacts recovers what a server said about itself the last time it
// connected: its own description and the tools it offered. A cache the current
// declaration no longer matches is returned anyway, marked stale — the server's
// own words, one edit out of date, still beat a row that can say nothing.
func mcpCachedFacts(spec plugin.Spec) (description string, tools []plugin.ToolInfo, stale bool) {
	cs, ok, keyOK := plugin.LoadCachedSchemaAny(spec.Name, plugin.SchemaCacheKey(spec))
	if !ok {
		return "", nil, false
	}
	tools = make([]plugin.ToolInfo, 0, len(cs.Tools))
	for _, t := range cs.Tools {
		if !spec.ToolEnabled(t.Name) {
			continue
		}
		tools = append(tools, plugin.ToolInfo{
			Name:            t.Name,
			Description:     t.Description,
			ReadOnlyHint:    t.ReadOnly,
			DestructiveHint: t.Destructive,
		})
	}
	return cs.Instructions, tools, !keyOK
}

// mcpServersInSchema names the servers with at least one tool in this
// session's provider schema.
func (c *Controller) mcpServersInSchema() map[string]bool {
	out := map[string]bool{}
	reg := c.mcp.registry()
	if reg == nil {
		return out
	}
	for _, b := range reg.MCPBindings() {
		if reg.ProviderVisible(b.CallableName) {
			out[b.Server] = true
		}
	}
	return out
}
