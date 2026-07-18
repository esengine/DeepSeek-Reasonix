package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"reasonix/internal/runtimeapi"
)

const (
	remoteTargetStateEvent = "remote:target-state"
	remoteAskPassEvent     = "remote:askpass"
	remoteLocalTargetID    = "local"
	remoteAppActionTimeout = 2 * time.Minute
	remoteAppShutdownLimit = 5 * time.Second
	remoteConnectionLogMax = 200
)

// RemoteHostInput is the secret-free Host editor contract. ID is omitted when
// creating an entry and retained when updating one; client and lease identities
// are never accepted from the frontend.
type RemoteHostInput struct {
	ID            string                   `json:"id,omitempty"`
	Mode          RemoteHostConnectionMode `json:"mode,omitempty"`
	Destination   string                   `json:"destination,omitempty"`
	Port          int                      `json:"port,omitempty"`
	Alias         string                   `json:"alias,omitempty"`
	Label         string                   `json:"label"`
	SSHConfigPath string                   `json:"sshConfigPath,omitempty"`
}

// RemoteHostView deliberately excludes clientInstanceId, resumeLeaseId and all
// authentication material.
type RemoteHostView struct {
	ID            string                   `json:"id"`
	Mode          RemoteHostConnectionMode `json:"mode"`
	Destination   string                   `json:"destination,omitempty"`
	Port          int                      `json:"port,omitempty"`
	Alias         string                   `json:"alias,omitempty"`
	Label         string                   `json:"label"`
	SSHConfigPath string                   `json:"sshConfigPath,omitempty"`
}

type RemoteTargetStatusView struct {
	State        TargetState `json:"state"`
	HostID       string      `json:"hostId,omitempty"`
	HostLabel    string      `json:"hostLabel,omitempty"`
	Failure      string      `json:"failure,omitempty"`
	CanReconnect bool        `json:"canReconnect"`
}

// RemoteConnectionLogView is a bounded, secret-free lifecycle record. It never
// contains SSH argv, raw stderr, credentials, lease identities, or Host paths.
type RemoteConnectionLogView struct {
	AtMillis  int64       `json:"atMillis"`
	State     TargetState `json:"state"`
	HostID    string      `json:"hostId,omitempty"`
	HostLabel string      `json:"hostLabel,omitempty"`
	Message   string      `json:"message,omitempty"`
}

type RemoteAskPassView struct {
	RequestID string                  `json:"requestId"`
	Kind      RemoteAskPassPromptKind `json:"kind"`
	Prompt    string                  `json:"prompt"`
	HostLabel string                  `json:"hostLabel,omitempty"`
	Secret    bool                    `json:"secret"`
}

type remoteAskPassPending struct {
	answer chan RemoteAskPassAnswer
}

// remoteDesktopState keeps all Remote connection state behind one App field so
// ordinary Local-only tests retain the App zero-value behavior.
type remoteDesktopState struct {
	initOnce sync.Once
	initErr  error
	store    *RemoteHostStore
	manager  *TargetManager

	askMu   sync.Mutex
	pending map[string]remoteAskPassPending

	logMu sync.RWMutex
	logs  []RemoteConnectionLogView

	workbenchMu sync.RWMutex
	workbench   remoteWorkbenchState
	workbenchOp sync.Mutex
}

type appLocalTargetConnector struct{ app *App }

func (c appLocalTargetConnector) Connect(ctx context.Context, target TargetDescriptor) (TargetAdapter, error) {
	if c.app == nil {
		return nil, errors.New("Local target App is unavailable")
	}
	if err := target.Validate(); err != nil {
		return nil, err
	}
	if target.Kind != TargetLocal || target.ID != remoteLocalTargetID {
		return nil, errors.New("Local connector received a non-Local target")
	}
	return ResumeLocalTargetAdapter(ctx, c.app)
}

func (a *App) ensureRemoteDesktop() (*RemoteHostStore, *TargetManager, error) {
	if a == nil {
		return nil, nil, errors.New("Desktop App is unavailable")
	}
	a.remote.initOnce.Do(func() {
		storePath, err := defaultRemoteHostStorePath()
		if err != nil {
			a.remote.initErr = err
			return
		}
		store, err := NewRemoteHostStore(storePath)
		if err != nil {
			a.remote.initErr = err
			return
		}
		helperPath, err := os.Executable()
		if err != nil {
			a.remote.initErr = fmt.Errorf("locate Desktop AskPass helper: %w", err)
			return
		}
		helperPath, err = filepath.Abs(helperPath)
		if err != nil {
			a.remote.initErr = fmt.Errorf("resolve Desktop AskPass helper: %w", err)
			return
		}
		local, err := NewLocalTargetAdapter(a)
		if err != nil {
			a.remote.initErr = fmt.Errorf("initialize Local RuntimeAPI: %w", err)
			return
		}
		remoteConnector, err := NewRemoteTargetConnector(RemoteTargetConnectorOptions{
			Store: store, AskPassHelperPath: helperPath, AskPassHandler: a.handleRemoteAskPass,
		})
		if err != nil {
			local.closeAdapter()
			a.remote.initErr = fmt.Errorf("initialize Remote connector: %w", err)
			return
		}
		manager, err := NewTargetManager(
			TargetConnectorMux{Local: appLocalTargetConnector{app: a}, Remote: remoteConnector},
			local,
			TargetManagerOptions{
				EventSink: a.handleTargetRuntimeEvent,
				StateSink: a.handleTargetState,
			},
		)
		if err != nil {
			local.closeAdapter()
			a.remote.initErr = fmt.Errorf("initialize TargetManager: %w", err)
			return
		}
		a.remote.store = store
		a.remote.manager = manager
		a.remote.askMu.Lock()
		a.remote.pending = make(map[string]remoteAskPassPending)
		a.remote.askMu.Unlock()
	})
	if a.remote.initErr != nil {
		return nil, nil, a.remote.initErr
	}
	if a.remote.store == nil || a.remote.manager == nil {
		return nil, nil, errors.New("Desktop Remote state did not initialize")
	}
	return a.remote.store, a.remote.manager, nil
}

func (a *App) handleTargetState(snapshot TargetManagerSnapshot) {
	a.recordRemoteConnectionState(snapshot)
	a.applyRemoteTargetState(snapshot)
	a.emitRuntimeEvent(remoteTargetStateEvent, remoteTargetStatusFromSnapshot(snapshot))
}

func (a *App) recordRemoteConnectionState(snapshot TargetManagerSnapshot) {
	entry := RemoteConnectionLogView{AtMillis: time.Now().UnixMilli(), State: snapshot.State}
	if snapshot.Target.Kind == TargetRemote {
		entry.HostID = snapshot.Target.ID
		entry.HostLabel = snapshot.Target.Label
	}
	entry.Message = strings.TrimSpace(snapshot.LastError)
	a.remote.logMu.Lock()
	a.remote.logs = append(a.remote.logs, entry)
	if len(a.remote.logs) > remoteConnectionLogMax {
		a.remote.logs = append([]RemoteConnectionLogView(nil), a.remote.logs[len(a.remote.logs)-remoteConnectionLogMax:]...)
	}
	a.remote.logMu.Unlock()
}

func remoteTargetStatusFromSnapshot(snapshot TargetManagerSnapshot) RemoteTargetStatusView {
	view := RemoteTargetStatusView{
		State: snapshot.State, Failure: snapshot.LastError,
		CanReconnect: snapshot.State == TargetRemoteReconnecting && snapshot.RecoveryAvailable,
	}
	if snapshot.Target.Kind == TargetRemote {
		view.HostID = snapshot.Target.ID
		view.HostLabel = snapshot.Target.Label
	}
	return view
}

// handleTargetRuntimeEvent is the single target-neutral bridge into Wails.
// Phase 5 App workbench routing fills the Desktop tab identity; until a Remote
// Session is attached there is intentionally nowhere to deliver the event.
func (a *App) handleTargetRuntimeEvent(value TargetRuntimeEvent) {
	if value.Target.Kind != TargetRemote {
		return // Local already has its lossless existing tabEventSink delivery.
	}
	a.emitRemoteRuntimeEvent(value)
}

func (a *App) emitRemoteRuntimeEvent(value TargetRuntimeEvent) {
	if !value.Event.Session.Valid() {
		return
	}
	if value.Event.Snapshot != nil {
		if a.updateRemoteWorkbenchSnapshot(value) {
			a.publishRemoteWorkbenchReady(a.RemoteWorkbenchStatus())
		}
		return
	}
	if !a.updateRemoteWorkbenchEvent(value) {
		return
	}
	a.emitRuntimeEvent(eventChannel, wireEventTab{
		Event: value.Event.Value,
		TabID: remoteSessionTabID(value.Event.Session),
	})
}

func remoteSessionTabID(session runtimeapi.SessionRef) string {
	sum := sha256.Sum256([]byte(string(session.WorkspaceID) + "\x00" + string(session.SessionID)))
	return "remote_tab_" + hex.EncodeToString(sum[:16])
}

func (a *App) RemoteHosts() ([]RemoteHostView, error) {
	store, _, err := a.ensureRemoteDesktop()
	if err != nil {
		return nil, err
	}
	hosts, err := store.Load()
	if err != nil {
		return nil, err
	}
	out := make([]RemoteHostView, len(hosts))
	for index, host := range hosts {
		out[index] = remoteHostView(host)
	}
	return out, nil
}

func remoteHostView(host RemoteHostEntry) RemoteHostView {
	return RemoteHostView{
		ID: host.ID, Mode: host.Mode, Destination: host.Destination, Port: host.Port,
		Alias: host.Alias, Label: host.Label, SSHConfigPath: host.SSHConfigPath,
	}
}

func (a *App) SaveRemoteHost(input RemoteHostInput) (RemoteHostView, error) {
	store, manager, err := a.ensureRemoteDesktop()
	if err != nil {
		return RemoteHostView{}, err
	}
	input, err = normalizeRemoteHostInput(input)
	if err != nil {
		return RemoteHostView{}, err
	}
	if input.Label == "" {
		if input.Mode == RemoteHostConnectionDirect {
			input.Label = input.Destination
		} else {
			input.Label = input.Alias
		}
	}
	if input.ID == "" {
		var entry RemoteHostEntry
		if input.Mode == RemoteHostConnectionDirect {
			entry, err = NewRemoteDirectHostEntry(input.Destination, input.Port, input.Label)
		} else {
			entry, err = NewRemoteHostEntry(input.Alias, input.Label)
			entry.SSHConfigPath = input.SSHConfigPath
		}
		if err != nil {
			return RemoteHostView{}, err
		}
		if err := store.Upsert(entry); err != nil {
			return RemoteHostView{}, err
		}
		return remoteHostView(entry), nil
	}
	if remoteHostEntryInUse(manager.Snapshot(), input.ID) {
		return RemoteHostView{}, errors.New("disconnect this Remote Host before editing its connection entry")
	}
	entry, found, err := store.Get(input.ID)
	if err != nil {
		return RemoteHostView{}, err
	}
	if !found {
		return RemoteHostView{}, fmt.Errorf("Remote Host entry %q is not saved", input.ID)
	}
	entry.Mode = input.Mode
	entry.Destination = input.Destination
	entry.Port = input.Port
	entry.Alias = input.Alias
	entry.Label = input.Label
	entry.SSHConfigPath = input.SSHConfigPath
	if err := store.Upsert(entry); err != nil {
		return RemoteHostView{}, err
	}
	return remoteHostView(entry), nil
}

func normalizeRemoteHostInput(input RemoteHostInput) (RemoteHostInput, error) {
	input.ID = strings.TrimSpace(input.ID)
	input.Destination = strings.TrimSpace(input.Destination)
	input.Alias = strings.TrimSpace(input.Alias)
	input.Label = strings.TrimSpace(input.Label)
	input.SSHConfigPath = strings.TrimSpace(input.SSHConfigPath)

	if input.Mode == "" {
		hasDirect := input.Destination != "" || input.Port != 0
		hasConfig := input.Alias != "" || input.SSHConfigPath != ""
		if hasDirect == hasConfig {
			return RemoteHostInput{}, errors.New("choose either a direct SSH destination or an OpenSSH config alias")
		}
		if hasDirect {
			input.Mode = RemoteHostConnectionDirect
		} else {
			input.Mode = RemoteHostConnectionConfig
		}
	}

	switch input.Mode {
	case RemoteHostConnectionDirect:
		target, err := ParseRemoteSSHDirectDestination(input.Destination)
		if err != nil {
			return RemoteHostInput{}, err
		}
		input.Destination = target.Destination()
		if input.Port == 0 {
			input.Port = defaultRemoteSSHPort
		}
		if err := ValidateRemoteSSHPort(input.Port); err != nil {
			return RemoteHostInput{}, err
		}
		input.Alias = ""
		input.SSHConfigPath = ""
	case RemoteHostConnectionConfig:
		if err := ValidateRemoteHostAlias(input.Alias); err != nil {
			return RemoteHostInput{}, err
		}
		if err := validateRemoteSSHConfigPath(input.SSHConfigPath); err != nil {
			return RemoteHostInput{}, err
		}
		input.Destination = ""
		input.Port = 0
	default:
		return RemoteHostInput{}, errors.New("remote Host connection mode is invalid")
	}
	return input, nil
}

func (a *App) DeleteRemoteHost(id string) error {
	store, manager, err := a.ensureRemoteDesktop()
	if err != nil {
		return err
	}
	if remoteHostEntryInUse(manager.Snapshot(), id) {
		return errors.New("disconnect this Remote Host before deleting its connection entry")
	}
	return store.Delete(id)
}

func remoteHostEntryInUse(snapshot TargetManagerSnapshot, hostID string) bool {
	if snapshot.State == TargetRemoteConnecting || snapshot.State == TargetSwitching {
		return true
	}
	return snapshot.Target.Kind == TargetRemote && snapshot.Target.ID == hostID &&
		(snapshot.State == TargetRemoteConnected || snapshot.State == TargetRemoteReconnecting)
}

func (a *App) RemoteTargetStatus() RemoteTargetStatusView {
	_, manager, err := a.ensureRemoteDesktop()
	if err != nil {
		return RemoteTargetStatusView{State: TargetDisconnected, Failure: err.Error()}
	}
	return remoteTargetStatusFromSnapshot(manager.Snapshot())
}

func (a *App) RemoteConnectionLogs() []RemoteConnectionLogView {
	if _, _, err := a.ensureRemoteDesktop(); err != nil {
		return []RemoteConnectionLogView{{AtMillis: time.Now().UnixMilli(), State: TargetDisconnected, Message: err.Error()}}
	}
	a.remote.logMu.RLock()
	defer a.remote.logMu.RUnlock()
	return append([]RemoteConnectionLogView(nil), a.remote.logs...)
}

func (a *App) ConnectRemoteHost(id string) error {
	store, manager, err := a.ensureRemoteDesktop()
	if err != nil {
		return err
	}
	entry, found, err := store.Get(id)
	if err != nil {
		return err
	}
	if !found {
		return fmt.Errorf("Remote Host entry %q is not saved", id)
	}
	ctx, cancel := a.remoteActionContext()
	defer cancel()
	return manager.Switch(ctx, TargetDescriptor{Kind: TargetRemote, ID: entry.ID, Label: entry.Label}, SwitchTargetOptions{})
}

func (a *App) ReconnectRemoteTarget() error {
	_, manager, err := a.ensureRemoteDesktop()
	if err != nil {
		return err
	}
	ctx, cancel := a.remoteActionContext()
	defer cancel()
	return manager.Reconnect(ctx)
}

func (a *App) SwitchToLocalTarget(confirmed bool) error {
	_, manager, err := a.ensureRemoteDesktop()
	if err != nil {
		return err
	}
	ctx, cancel := a.remoteActionContext()
	defer cancel()
	return manager.Switch(ctx, TargetDescriptor{Kind: TargetLocal, ID: remoteLocalTargetID, Label: "This computer"}, SwitchTargetOptions{
		ConfirmRemoteDetach: confirmed,
	})
}

func (a *App) remoteActionContext() (context.Context, context.CancelFunc) {
	parent := a.bootContext()
	return context.WithTimeout(parent, remoteAppActionTimeout)
}

func (a *App) handleRemoteAskPass(ctx context.Context, prompt RemoteAskPassPrompt) (RemoteAskPassAnswer, error) {
	requestID, err := newRemoteAskPassRequestID()
	if err != nil {
		return RemoteAskPassAnswer{}, err
	}
	pending := remoteAskPassPending{answer: make(chan RemoteAskPassAnswer, 1)}
	a.remote.askMu.Lock()
	if a.remote.pending == nil {
		a.remote.pending = make(map[string]remoteAskPassPending)
	}
	a.remote.pending[requestID] = pending
	a.remote.askMu.Unlock()
	defer func() {
		a.remote.askMu.Lock()
		delete(a.remote.pending, requestID)
		a.remote.askMu.Unlock()
	}()
	a.emitRuntimeEvent(remoteAskPassEvent, RemoteAskPassView{
		RequestID: requestID, Kind: prompt.Kind, Prompt: prompt.Message, HostLabel: prompt.HostLabel,
		Secret: remoteAskPassPromptIsSecret(prompt.Kind),
	})
	select {
	case <-ctx.Done():
		return RemoteAskPassAnswer{}, ctx.Err()
	case answer := <-pending.answer:
		return answer, nil
	}
}

func newRemoteAskPassRequestID() (string, error) {
	var entropy [32]byte
	if _, err := rand.Read(entropy[:]); err != nil {
		return "", fmt.Errorf("generate AskPass request identity: %w", err)
	}
	return "askpass_" + hex.EncodeToString(entropy[:]), nil
}

func remoteAskPassPromptIsSecret(kind RemoteAskPassPromptKind) bool {
	return kind != RemoteAskPassHostKeyConfirm && kind != RemoteAskPassHostKeyChanged
}

func (a *App) RespondRemoteAskPass(requestID, value string, cancelled bool) error {
	if !strings.HasPrefix(requestID, "askpass_") || len(requestID) != len("askpass_")+64 {
		return errors.New("invalid AskPass request identity")
	}
	if len(value) > remoteAskPassMaxAnswerBytes || strings.ContainsAny(value, "\x00\r\n") {
		return errors.New("invalid AskPass response")
	}
	a.remote.askMu.Lock()
	pending, ok := a.remote.pending[requestID]
	if ok {
		delete(a.remote.pending, requestID)
	}
	a.remote.askMu.Unlock()
	if !ok {
		return errors.New("AskPass request is no longer pending")
	}
	answer := RemoteAskPassAnswer{Accepted: !cancelled, Value: value}
	select {
	case pending.answer <- answer:
		return nil
	default:
		return errors.New("AskPass request was already answered")
	}
}

func (a *App) cancelRemoteAskPassPrompts() {
	a.remote.askMu.Lock()
	pending := a.remote.pending
	a.remote.pending = make(map[string]remoteAskPassPending)
	a.remote.askMu.Unlock()
	for _, request := range pending {
		select {
		case request.answer <- RemoteAskPassAnswer{}:
		default:
		}
	}
}

func (a *App) shutdownRemoteDesktop() error {
	if a == nil {
		return nil
	}
	a.cancelRemoteAskPassPrompts()
	manager := a.remote.manager
	if manager == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), remoteAppShutdownLimit)
	defer cancel()
	return manager.Shutdown(ctx)
}
