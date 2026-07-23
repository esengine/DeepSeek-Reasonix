package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"reasonix/internal/boot"
	"reasonix/internal/config"
	"reasonix/internal/provider"
	"reasonix/internal/remote/broker"
	"reasonix/internal/remote/protocol"
	"reasonix/internal/remote/workbench/client"
	"reasonix/internal/remote/workbench/target"
	"reasonix/internal/remote/workbench/transport"
	"reasonix/internal/remote/workbench/trust"
)

// workbenchKernel owns Local + Remote adapters for the main desktop window.
type workbenchKernel struct {
	mu                sync.Mutex
	transitionMu      sync.Mutex
	targets           *target.Manager
	remote            *client.Client
	remoteGen         uint64
	remoteTabID       string
	remoteFingerprint string
	providerAccess    *workbenchProviderAccess
	snapshot          protocol.SessionSnapshot
	catalog           protocol.WorkspaceCatalogResult
	sessionCatalog    protocol.SessionCatalogResult
	pendingTrust      *ProviderTrustPromptView
	trustAnswer       chan bool
}

type workbenchProviderAccess struct {
	mu      sync.RWMutex
	allowed map[string]struct{}
}

func newWorkbenchProviderAccess(allowed map[string]struct{}) *workbenchProviderAccess {
	access := &workbenchProviderAccess{}
	access.replace(allowed)
	return access
}

func (a *workbenchProviderAccess) snapshot() map[string]struct{} {
	if a == nil {
		return map[string]struct{}{}
	}
	a.mu.RLock()
	defer a.mu.RUnlock()
	out := make(map[string]struct{}, len(a.allowed))
	for ref := range a.allowed {
		out[ref] = struct{}{}
	}
	return out
}

func (a *workbenchProviderAccess) replace(allowed map[string]struct{}) {
	if a == nil {
		return
	}
	next := make(map[string]struct{}, len(allowed))
	for ref := range allowed {
		next[ref] = struct{}{}
	}
	a.mu.Lock()
	a.allowed = next
	a.mu.Unlock()
}

const workbenchTargetEvent = "remote:workbench-target"

type WorkbenchTargetStateView struct {
	State       string            `json:"state"`
	Kind        target.Kind       `json:"kind"`
	HostID      string            `json:"hostId,omitempty"`
	Workspace   string            `json:"workspace,omitempty"`
	IdentityGen uint64            `json:"identityGen"`
	RequestSeq  uint64            `json:"requestSeq,omitempty"`
	Error       string            `json:"error,omitempty"`
	Reconnect   target.RemoteHint `json:"reconnect"`
}

// withWorkbenchLocalNavigation serializes a visible Local navigation against
// Remote connect/reactivation. The later caller wins: Remote stays connected in
// the background, while its callbacks stop projecting into the Local surface.
func (a *App) withWorkbenchLocalNavigation(run func() (TabMeta, error)) (meta TabMeta, err error) {
	k := a.workbench()
	k.transitionMu.Lock()
	active, _, _ := k.targets.Active()
	switched := active.Kind == target.KindRemote
	var id target.Identity
	var gen, seq uint64
	if switched {
		id, gen, seq = k.targets.SwitchLocal()
	}
	defer func() {
		if switched {
			tabID := meta.ID
			if tabID == "" {
				tabID = a.workbenchProjectionTabID()
			}
			a.emitWorkbenchTarget("disconnected", id, gen, seq, "")
			a.emitReady(a.ctx, tabID)
			a.emitRuntimeEvent("runtime:rebuilt", tabID)
		}
		k.transitionMu.Unlock()
	}()
	return run()
}

// ProviderTrustPromptView is the Wails-facing Provider Broker authorization UI.
// Never includes secrets, base URLs, or env names.
type ProviderTrustPromptView struct {
	HostID       string   `json:"hostId"`
	Host         string   `json:"host"`
	KeyType      string   `json:"keyType"`
	Fingerprint  string   `json:"fingerprint"`
	Workspace    string   `json:"workspace"`
	ProviderRefs []string `json:"providerRefs"`
	Warning      string   `json:"warning"`
}

func newWorkbenchKernel() *workbenchKernel {
	return &workbenchKernel{targets: target.New()}
}

func (a *App) workbench() *workbenchKernel {
	a.remoteMu.Lock()
	defer a.remoteMu.Unlock()
	if a.workbenchKernel == nil {
		a.workbenchKernel = newWorkbenchKernel()
	}
	return a.workbenchKernel
}

// WorkbenchActiveTarget returns the current projection for the status bar.
func (a *App) WorkbenchActiveTarget() map[string]any {
	k := a.workbench()
	id, gen, seq := k.targets.Active()
	h := a.workbenchLastRemoteHint()
	return map[string]any{
		"kind": string(id.Kind), "hostId": id.HostID, "workspace": id.Workspace,
		"identityGen": gen, "requestSeq": seq,
		"reconnect": map[string]string{"hostId": h.HostID, "workspace": h.Workspace, "label": h.Label},
	}
}

// WorkbenchLastRemoteHint is the post-restart reconnect entry (no auto-connect).
func (a *App) WorkbenchLastRemoteHint() map[string]string {
	h := a.workbenchLastRemoteHint()
	return map[string]string{"hostId": h.HostID, "workspace": h.Workspace, "label": h.Label}
}

func (a *App) workbenchLastRemoteHint() target.RemoteHint {
	k := a.workbench()
	h := k.targets.LastRemoteHint()
	if h.HostID != "" {
		return h
	}
	remotePrefsMu.Lock()
	prefs := loadRemotePrefs()
	remotePrefsMu.Unlock()
	if prefs.LastHostID == "" {
		return h
	}
	h = target.RemoteHint{HostID: prefs.LastHostID, Workspace: prefs.LastWorkspaceByHost[prefs.LastHostID]}
	if cfg, err := config.Load(); err == nil {
		if entry, ok := cfg.RemoteHost(h.HostID); ok {
			h.Label = entry.Host
		}
	}
	return h
}

// WorkbenchSwitchLocal projects the permanent Local adapter.
func (a *App) WorkbenchSwitchLocal() map[string]any {
	k := a.workbench()
	k.transitionMu.Lock()
	defer k.transitionMu.Unlock()
	id, gen, seq := k.targets.SwitchLocal()
	a.emitWorkbenchTarget("disconnected", id, gen, seq, "")
	tabID := a.workbenchProjectionTabID()
	a.emitReady(a.ctx, tabID)
	a.emitRuntimeEvent("runtime:rebuilt", tabID)
	return map[string]any{"kind": string(id.Kind), "identityGen": gen, "requestSeq": seq}
}

// WorkbenchConnectRemote opens SSH stdio workbench + local Provider Broker.
func (a *App) WorkbenchConnectRemote(hostID, workspace string) error {
	hostID = strings.TrimSpace(hostID)
	workspace = strings.TrimSpace(workspace)
	if hostID == "" || workspace == "" {
		return fmt.Errorf("host and workspace are required")
	}
	k := a.workbench()
	k.transitionMu.Lock()
	defer k.transitionMu.Unlock()
	if remote := k.targets.Remote(); remote != nil && remote.Connected &&
		remote.Identity.HostID == hostID && remote.Identity.Workspace == workspace {
		tabID := a.workbenchProjectionTabID()
		k.mu.Lock()
		cli, previousTabID := k.remote, k.remoteTabID
		if cli != nil && cli.Generation() == remote.Generation {
			k.remoteTabID = tabID
		}
		k.mu.Unlock()
		if cli == nil || cli.Generation() != remote.Generation {
			return fmt.Errorf("Remote adapter is unavailable; reconnect the host")
		}
		// Rebind the projection before activation. Until ActivateRemote succeeds,
		// callbacks still observe Local and cannot leak into the previous tab.
		cli.SetCallbacks(a.workbenchClientCallbacks(remote.Generation, tabID))
		activeID, activeGen, requestSeq, err := k.targets.ActivateRemote(remote.Generation)
		if err != nil {
			k.mu.Lock()
			if k.remote == cli && k.remoteGen == remote.Generation && k.remoteTabID == tabID {
				k.remoteTabID = previousTabID
			}
			k.mu.Unlock()
			cli.SetCallbacks(a.workbenchClientCallbacks(remote.Generation, previousTabID))
			return err
		}
		go a.workbenchRefreshSnapshot(remote.Generation, tabID)
		go a.workbenchRefreshCatalog(remote.Generation)
		a.emitWorkbenchTarget("connected", activeID, activeGen, requestSeq, "")
		a.emitReady(a.ctx, tabID)
		a.emitRuntimeEvent("runtime:rebuilt", tabID)
		return nil
	}
	_, gen, err := k.targets.BeginRemoteConnect(hostID, workspace)
	if err != nil {
		return err
	}
	committedID, committedGen, committedSeq := k.targets.Active()
	a.emitWorkbenchTarget("connecting", committedID, committedGen, committedSeq, "")
	committed := false
	failureText := ""
	defer func() {
		if !committed {
			if k.targets.AbortRemoteConnect(gen) {
				active, identityGen, requestSeq := k.targets.Active()
				state := "disconnected"
				if active.Kind == target.KindRemote {
					state = "connected"
				}
				a.emitWorkbenchTarget(state, active, identityGen, requestSeq, failureText)
			}
		}
	}()
	fail := func(err error) error {
		failureText = err.Error()
		return err
	}

	cfg, err := config.Load()
	if err != nil {
		return fail(err)
	}
	entry, ok := cfg.RemoteHost(hostID)
	if !ok {
		return fail(fmt.Errorf("unknown remote host %q", hostID))
	}
	fp, keyType, hostLabel, err := a.workbenchHostIdentity(hostID)
	if err != nil {
		return fail(err)
	}
	refs := localProviderRefs(cfg)
	if len(refs) == 0 {
		return fail(fmt.Errorf("no configured local chat model is available for Remote Workbench"))
	}
	store := trust.DefaultStore()
	missing, err := store.MissingRefs(hostID, fp, refs)
	if err != nil {
		return fail(err)
	}
	if len(missing) > 0 {
		if err := a.workbenchRequestTrust(hostID, hostLabel, keyType, fp, workspace, missing); err != nil {
			return fail(err)
		}
		if err := store.AuthorizeAll(hostID, keyType, fp, missing); err != nil {
			return fail(err)
		}
	}
	rec, found, err := store.Get(hostID, fp)
	if err != nil {
		return fail(err)
	}
	if !found {
		return fail(fmt.Errorf("provider authorization was not persisted for host %q", hostID))
	}
	allowed := map[string]struct{}{}
	for _, r := range rec.AllowedProviderRefs {
		allowed[r] = struct{}{}
	}
	if len(allowed) == 0 {
		return fail(fmt.Errorf("no provider model is authorized for host %q", hostID))
	}
	providerAccess := newWorkbenchProviderAccess(allowed)

	// Bind the attach transport to the workspace selected for this connection,
	// not a possibly stale default from the persisted host entry.
	entry.Workspace = workspace
	factory, err := a.workbenchTransportFactory(hostID, entry)
	if err != nil {
		return fail(err)
	}
	brokerOpts := broker.Options{
		Authorize: func() error {
			return authorizeWorkbenchPeer(factory, fp)
		},
		Catalog: func(ctx context.Context, filter map[string]struct{}) ([]protocol.BrokerProviderDescriptor, error) {
			current, err := config.Load()
			if err != nil {
				return nil, err
			}
			return catalogDescriptors(current, providerAccess.snapshot(), filter)
		},
		Open: func(ctx context.Context, ref, effort string, req provider.Request) (<-chan provider.Chunk, error) {
			if _, ok := providerAccess.snapshot()[ref]; !ok {
				return nil, fmt.Errorf("provider %q not authorized for this host", ref)
			}
			current, err := config.Load()
			if err != nil {
				return nil, err
			}
			return openLocalProviderStream(ctx, current, ref, effort, req)
		},
	}
	currentBuild := protocol.CurrentBuildID(version)
	buildID := map[string]any{
		"productVersion": currentBuild.ProductVersion, "sourceRevision": currentBuild.SourceRevision,
		"protocolVersion": currentBuild.ProtocolVersion, "schemaHash": currentBuild.SchemaHash,
	}
	tabID := a.workbenchProjectionTabID()
	cli, err := client.Connect(a.bootContext(), factory, gen, brokerOpts, buildID, workspace, a.workbenchClientCallbacks(gen, tabID))
	if err != nil {
		return fail(err)
	}
	keepClient := false
	defer func() {
		if !keepClient {
			cli.Close()
		}
	}()
	model := strings.TrimSpace(cfg.DefaultModel)
	if entry, ok := cfg.ResolveModel(model); ok {
		model = entry.Name + "/" + entry.Model
	}
	if _, authorized := allowed[model]; !authorized {
		model = ""
	}
	if model == "" && len(refs) > 0 {
		model = refs[0]
	}
	listRaw, err := cli.Request(a.bootContext(), string(protocol.MethodSessionList), protocol.SessionListParams{})
	if err != nil {
		return fail(fmt.Errorf("list Remote sessions: %w", err))
	}
	listDecoded, err := protocol.DecodeResult(protocol.MethodSessionList, listRaw)
	if err != nil {
		return fail(fmt.Errorf("decode Remote sessions: %w", err))
	}
	sessions := listDecoded.(protocol.SessionListResult).Items
	resumed := false
	for _, session := range sessions {
		if session.Runtime == nil || session.Runtime.RuntimeEpoch == "" {
			continue
		}
		if err := cli.SelectSession(session.Target, session.Runtime.RuntimeEpoch); err != nil {
			continue
		}
		resumed = true
		break
	}
	if !resumed {
		if _, err := cli.CreateSession(a.bootContext(), model, ""); err != nil {
			return fail(fmt.Errorf("create Remote session: %w", err))
		}
	}
	var activeID target.Identity
	var activeGen, requestSeq uint64
	var previous *client.Client
	subscribed, err := cli.SubscribeCommitted(a.bootContext(), protocol.HistoryMaxTurns, func(result protocol.SessionSubscribeResult) error {
		catalog, err := workbenchLoadCatalog(a.bootContext(), cli)
		if err != nil {
			return fmt.Errorf("load Remote model catalog: %w", err)
		}
		sessionCatalog, err := workbenchLoadSessionCatalog(a.bootContext(), cli)
		if err != nil {
			return fmt.Errorf("load Remote session catalog: %w", err)
		}
		if err := k.targets.MarkRemoteConnected(gen); err != nil {
			return err
		}
		activeID, activeGen, requestSeq, err = k.targets.ActivateRemote(gen)
		if err != nil {
			return err
		}
		k.mu.Lock()
		previous = k.remote
		k.remote = cli
		k.remoteGen = gen
		k.remoteTabID = tabID
		k.remoteFingerprint = fp
		k.providerAccess = providerAccess
		k.snapshot = result.Snapshot
		k.catalog = catalog
		k.sessionCatalog = sessionCatalog
		k.mu.Unlock()
		k.targets.SetRemoteBusy(result.Snapshot.Runtime.Running || result.Snapshot.Runtime.CurrentOperation != nil)
		return nil
	})
	if err != nil {
		return fail(fmt.Errorf("subscribe Remote session: %w", err))
	}
	go a.workbenchMirrorSnapshot(cli, subscribed.Snapshot)
	keepClient = true
	if previous != nil {
		previous.Close()
	}
	k.targets.RememberRemote(target.RemoteHint{HostID: hostID, Workspace: workspace, Label: hostLabel})
	a.saveLastRemoteWorkspace(hostID, workspace)
	committed = true
	a.emitWorkbenchTarget("connected", activeID, activeGen, requestSeq, "")
	a.emitReady(a.ctx, tabID)
	a.emitRuntimeEvent("runtime:rebuilt", tabID)
	return nil
}

type workbenchPeerIdentitySource interface {
	PeerIdentity() (workbenchPeerIdentity, bool)
}

func authorizeWorkbenchPeer(factory transport.Factory, expectedFingerprint string) error {
	source, ok := factory.(workbenchPeerIdentitySource)
	if !ok {
		return fmt.Errorf("workbench transport cannot report its authenticated peer identity")
	}
	peer, havePeer := source.PeerIdentity()
	if !havePeer || strings.TrimSpace(peer.Fingerprint) == "" || peer.Fingerprint != expectedFingerprint {
		return fmt.Errorf("authenticated workbench peer identity changed during connection")
	}
	return nil
}

// WorkbenchDisconnectRemote detaches when idle and revokes the Broker channel.
func (a *App) WorkbenchDisconnectRemote() error {
	k := a.workbench()
	k.transitionMu.Lock()
	defer k.transitionMu.Unlock()
	if err := k.targets.DetachRemote(); err != nil {
		return err
	}
	k.mu.Lock()
	if k.remote != nil {
		k.remote.Close()
		k.remote = nil
	}
	k.remoteGen = 0
	k.remoteTabID = ""
	k.remoteFingerprint = ""
	k.providerAccess = nil
	k.snapshot = protocol.SessionSnapshot{}
	k.catalog = protocol.WorkspaceCatalogResult{}
	k.sessionCatalog = protocol.SessionCatalogResult{}
	k.mu.Unlock()
	id, gen, seq := k.targets.Active()
	a.emitWorkbenchTarget("disconnected", id, gen, seq, "")
	return nil
}

// refreshWorkbenchProviderBroker applies the current Desktop provider config to
// an existing Remote capability. Removed providers stop opening immediately;
// newly added refs require an explicit trust decision before the Host sees them.
func (a *App) refreshWorkbenchProviderBroker() error {
	k := a.workbench()
	k.transitionMu.Lock()
	defer k.transitionMu.Unlock()
	remote := k.targets.Remote()
	if remote == nil || !remote.Connected {
		return nil
	}
	k.mu.Lock()
	cli, access, fp := k.remote, k.providerAccess, k.remoteFingerprint
	gen := k.remoteGen
	k.mu.Unlock()
	if cli == nil || access == nil || cli.Generation() != remote.Generation || gen != remote.Generation {
		return nil
	}
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	refs := localProviderRefs(cfg)
	store := trust.DefaultStore()
	missing, err := store.MissingRefs(remote.Identity.HostID, fp, refs)
	if err != nil {
		return err
	}
	var trustErr error
	if len(missing) > 0 {
		liveFingerprint, keyType, hostLabel, identityErr := a.workbenchHostIdentity(remote.Identity.HostID)
		if identityErr != nil {
			trustErr = identityErr
		} else if liveFingerprint != fp {
			trustErr = fmt.Errorf("authenticated workbench peer identity changed during provider refresh")
		} else if requestErr := a.workbenchRequestTrust(remote.Identity.HostID, hostLabel, keyType, fp, remote.Identity.Workspace, missing); requestErr != nil {
			trustErr = requestErr
		} else if authorizeErr := store.AuthorizeAll(remote.Identity.HostID, keyType, fp, missing); authorizeErr != nil {
			trustErr = authorizeErr
		}
	}
	record, _, getErr := store.Get(remote.Identity.HostID, fp)
	if getErr != nil {
		return getErr
	}
	configured := make(map[string]struct{}, len(refs))
	for _, ref := range refs {
		configured[ref] = struct{}{}
	}
	allowed := make(map[string]struct{}, len(record.AllowedProviderRefs))
	for _, ref := range record.AllowedProviderRefs {
		if _, ok := configured[ref]; ok {
			allowed[ref] = struct{}{}
		}
	}
	access.replace(allowed)
	if notifyErr := cli.NotifyProviderCatalogChanged(); notifyErr != nil {
		return notifyErr
	}
	go a.workbenchRefreshCatalog(gen)
	return trustErr
}

func (a *App) refreshWorkbenchProviderBrokerAsync() {
	if a == nil || a.ctx == nil {
		return
	}
	go func() {
		if err := a.refreshWorkbenchProviderBroker(); err != nil {
			a.warnForTab(a.workbenchProjectionTabID(), "Remote provider access was not refreshed: "+err.Error())
		}
	}()
}

// WorkbenchRemoteRequest proxies a RuntimeAPI method to the connected remote.
func (a *App) WorkbenchRemoteRequest(method string, paramsJSON string) (string, error) {
	k := a.workbench()
	k.transitionMu.Lock()
	defer k.transitionMu.Unlock()
	id, _, _ := k.targets.Active()
	if id.Kind != target.KindRemote {
		return "", fmt.Errorf("CAPABILITY_UNAVAILABLE: active target is local")
	}
	k.mu.Lock()
	cli := k.remote
	cliGen := k.remoteGen
	k.mu.Unlock()
	remoteState := k.targets.Remote()
	if cli == nil || remoteState == nil || !remoteState.Connected || cliGen != remoteState.Generation {
		return "", fmt.Errorf("CAPABILITY_UNAVAILABLE: remote not connected")
	}
	var params any
	if strings.TrimSpace(paramsJSON) != "" {
		if err := json.Unmarshal([]byte(paramsJSON), &params); err != nil {
			return "", err
		}
	} else {
		params = map[string]any{}
	}
	raw, err := cli.Request(a.bootContext(), method, params)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

// WorkbenchResolveProviderTrust answers a pending trust prompt.
func (a *App) WorkbenchResolveProviderTrust(accept bool) error {
	k := a.workbench()
	k.mu.Lock()
	ch := k.trustAnswer
	k.trustAnswer = nil
	k.pendingTrust = nil
	k.mu.Unlock()
	if ch == nil {
		return fmt.Errorf("no pending provider trust prompt")
	}
	select {
	case ch <- accept:
	default:
	}
	return nil
}

// WorkbenchPendingProviderTrust returns the current prompt or nil.
func (a *App) WorkbenchPendingProviderTrust() *ProviderTrustPromptView {
	k := a.workbench()
	k.mu.Lock()
	defer k.mu.Unlock()
	return k.pendingTrust
}

func (a *App) workbenchRequestTrust(hostID, hostLabel, keyType, fp, workspace string, refs []string) error {
	k := a.workbench()
	answer := make(chan bool, 1)
	k.mu.Lock()
	if k.trustAnswer != nil {
		k.mu.Unlock()
		return fmt.Errorf("provider trust prompt already pending")
	}
	k.trustAnswer = answer
	k.pendingTrust = &ProviderTrustPromptView{
		HostID: hostID, Host: hostLabel, KeyType: keyType, Fingerprint: fp,
		Workspace: workspace, ProviderRefs: append([]string(nil), refs...),
		Warning: "This remote host will consume your local model API quota through the Provider Broker until disconnect. API keys never leave this machine.",
	}
	prompt := *k.pendingTrust
	k.mu.Unlock()
	if a.ctx != nil {
		a.runtimeEvents.Emit(a.ctx, "remote:provider-trust", prompt)
	}
	select {
	case accept := <-answer:
		if !accept {
			return fmt.Errorf("provider trust declined for host %q", hostID)
		}
		return nil
	case <-a.bootContext().Done():
		return fmt.Errorf("connection closed while waiting for provider trust")
	}
}

func (a *App) workbenchHostIdentity(hostID string) (fingerprint, keyType, hostLabel string, err error) {
	rt, rerr := a.remoteRT()
	if rerr != nil {
		return "", "", "", rerr
	}
	manager, ok := rt.(*desktopRemoteManager)
	if !ok {
		return "", "", "", fmt.Errorf("remote manager cannot provide an authenticated peer identity")
	}
	peer, ok := manager.workbenchPeerIdentity(hostID)
	if !ok {
		return "", "", "", fmt.Errorf("host %q must be connected and host-key verified before opening a workbench", hostID)
	}
	fingerprint, keyType, hostLabel = peer.SHA256, peer.KeyType, peer.Address
	cfg, _ := config.Load()
	if entry, ok := cfg.RemoteHost(hostID); ok {
		if hostLabel == "" {
			hostLabel = entry.Host
			if entry.User != "" {
				hostLabel = entry.User + "@" + entry.Host
			}
		}
	}
	return fingerprint, keyType, hostLabel, nil
}

func (a *App) workbenchTransportFactory(hostID string, entry config.RemoteHostEntry) (transport.Factory, error) {
	// Windows: system OpenSSH. Other platforms: Go SSH stdio session.
	rt, err := a.remoteRT()
	if err != nil {
		return nil, err
	}
	manager, ok := rt.(*desktopRemoteManager)
	if !ok {
		return nil, fmt.Errorf("remote manager cannot service SSH authentication prompts")
	}
	return newWorkbenchSSHFactory(entry, manager.workbenchAskPassHandler(hostID, entry))
}

func (a *App) emitWorkbenchTarget(state string, id target.Identity, gen, seq uint64, errText string) {
	if a == nil || a.ctx == nil {
		return
	}
	a.runtimeEvents.Emit(a.ctx, workbenchTargetEvent, WorkbenchTargetStateView{
		State: state, Kind: id.Kind, HostID: id.HostID, Workspace: id.Workspace,
		IdentityGen: gen, RequestSeq: seq, Error: errText, Reconnect: a.workbenchLastRemoteHint(),
	})
}

func localProviderRefs(cfg *config.Config) []string {
	if cfg == nil {
		return nil
	}
	access := providerAccessSet(cfg.Desktop.ProviderAccess)
	refs := make([]string, 0, len(cfg.Providers))
	for i := range cfg.Providers {
		pe := &cfg.Providers[i]
		if !modelProviderAccessAllowed(access, pe.Name) || !pe.Configured() {
			continue
		}
		models := pe.ChatModelList()
		if len(models) == 0 {
			if model := pe.DefaultModel(); model != "" {
				models = []string{model}
			}
		}
		for _, model := range models {
			refs = append(refs, pe.Name+"/"+model)
		}
	}
	return refs
}

func catalogDescriptors(cfg *config.Config, allowed, filter map[string]struct{}) ([]protocol.BrokerProviderDescriptor, error) {
	var out []protocol.BrokerProviderDescriptor
	for _, ref := range localProviderRefs(cfg) {
		if _, ok := allowed[ref]; !ok {
			continue
		}
		if len(filter) > 0 {
			if _, ok := filter[ref]; !ok {
				continue
			}
		}
		pe, ok := cfg.ResolveModel(ref)
		if !ok {
			continue
		}
		p, err := boot.NewProviderWithProxy(pe, cfg.NetworkProxySpec())
		if err != nil {
			out = append(out, protocol.BrokerProviderDescriptor{Ref: ref, DisplayName: pe.Name, Model: pe.Model})
			continue
		}
		out = append(out, broker.DescriptorFromProvider(ref, pe.Name, pe.Model, p, pe.SupportedEfforts, config.EffectiveEffort(pe), config.EffectiveVision(pe), pe.ContextWindow, pe.Price))
	}
	return out, nil
}

func openLocalProviderStream(ctx context.Context, cfg *config.Config, ref, effort string, req provider.Request) (<-chan provider.Chunk, error) {
	pe, ok := cfg.ResolveModel(ref)
	if !ok || !pe.Configured() {
		return nil, fmt.Errorf("provider model %q is not configured", ref)
	}
	if strings.TrimSpace(effort) != "" {
		normalized, err := config.NormalizeEffort(pe, effort)
		if err != nil {
			return nil, err
		}
		pe.Effort = normalized
	}
	p, err := boot.NewProviderWithProxy(pe, cfg.NetworkProxySpec())
	if err != nil {
		return nil, err
	}
	return p.Stream(ctx, req)
}
