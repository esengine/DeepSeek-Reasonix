//go:build windows && reasonix_legacy_remote_integration

// This fixture targets the removed pre-workbench Remote client/protocol. Keep
// it source-available for historical environments, but do not compile it into
// the current Windows suite; the active workbench path is covered by the
// transport, AskPass, process-lifecycle and target-transition tests.

package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
	"reasonix/internal/eventwire"
	remoteclient "reasonix/internal/remote/client"
	"reasonix/internal/remote/protocol"
)

const (
	remoteSSHIntegrationGateEnv       = "REASONIX_REMOTE_SSH_INTEGRATION"
	remoteSSHIntegrationSSHPathEnv    = "REASONIX_REMOTE_SSH_INTEGRATION_SSH_PATH"
	remoteSSHIntegrationConfigPathEnv = "REASONIX_REMOTE_SSH_INTEGRATION_CONFIG_PATH"
	remoteSSHIntegrationKnownHostsEnv = "REASONIX_REMOTE_SSH_INTEGRATION_KNOWN_HOSTS_PATH"
	remoteSSHIntegrationHostAliasEnv  = "REASONIX_REMOTE_SSH_INTEGRATION_HOST_ALIAS"
	remoteSSHIntegrationTimeout       = 75 * time.Second
	remoteSSHIntegrationGapWait       = 7 * time.Second
	remoteSSHIntegrationTopicPrefix   = "Reasonix Remote SSH reconnect integration"
	remoteSSHIntegrationBeforeMarker  = "reasonix-remote-event-before"
	remoteSSHIntegrationGapMarker     = "reasonix-remote-event-gap"
	remoteSSHIntegrationAfterMarker   = "reasonix-remote-event-after"
	remoteSSHIntegrationShellCommand  = "printf 'reasonix-remote-event-before\\n'; sleep 5; printf 'reasonix-remote-event-gap\\n'; sleep 30; printf 'reasonix-remote-event-after\\n'"
)

type remoteSSHIntegrationFixture struct {
	workspaceID            protocol.WorkspaceID
	topicTitle             string
	topicID                protocol.TopicID
	target                 protocol.RuntimeTarget
	runtimeEpoch           protocol.RuntimeEpoch
	subscriptionID         protocol.SubscriptionID
	operationID            protocol.OperationID
	topicCreateRequestID   protocol.RequestID
	sessionCreateRequestID protocol.RequestID
	shellRequestID         protocol.RequestID
	cancelRequestID        protocol.RequestID
	closeRequestID         protocol.RequestID
	trashRequestID         protocol.RequestID
	purgeRequestID         protocol.RequestID
	deleteRequestID        protocol.RequestID
	topicCreateSent        bool
	sessionCreateSent      bool
	shellSent              bool
}

type remoteSSHIntegrationFailure struct {
	stage string
	err   error
}

type remoteSSHIntegrationACLTrustee struct {
	sid *windows.SID
}

type remoteSSHIntegrationACLValidationError uint8

const (
	remoteSSHIntegrationACLDescriptorMissing remoteSSHIntegrationACLValidationError = iota + 1
	remoteSSHIntegrationACLNotProtected
	remoteSSHIntegrationACLDACLMissing
	remoteSSHIntegrationACLDACLDefaulted
	remoteSSHIntegrationACLTrusteeCountMismatch
	remoteSSHIntegrationACLEntryTypeInvalid
	remoteSSHIntegrationACLPermissionInvalid
	remoteSSHIntegrationACLFlagsInvalid
	remoteSSHIntegrationACLTrusteeUnexpected
	remoteSSHIntegrationACLTrusteeMissing
	remoteSSHIntegrationACLHandleNotDirectory
	remoteSSHIntegrationACLHandleIsReparsePoint
)

func (remoteSSHIntegrationACLValidationError) Error() string {
	return "protected directory ACL validation failed"
}

// TestRemoteSSHConfigAttachReconnectIntegration is deliberately opt-in: the
// skip gate is checked before config paths, aliases, or production build
// identity are inspected. When enabled it exercises the real Windows system
// ssh.exe -> sshd -> reasonix remote attach-workspace --stdio -> daemon path. OpenSSH,
// rather than the test, reads any IdentityFile referenced by the supplied
// config; credential material and raw stderr are never loaded or logged here.
// Topic/Session/snapshot assertions below cover the transport-neutral Remote
// client and frozen protocol only; native Desktop workbench/UI evidence belongs
// to separate tests.
func TestRemoteSSHConfigAttachReconnectIntegration(t *testing.T) {
	if os.Getenv(remoteSSHIntegrationGateEnv) != "1" {
		t.Skip("set REASONIX_REMOTE_SSH_INTEGRATION=1 to run the real SSH integration test")
	}

	buildID, err := currentDesktopRemoteBuildID()
	if err != nil {
		remoteSSHIntegrationFail(t, "resolve coordinated Desktop build identity", err)
	}
	sshPath := remoteSSHIntegrationAbsolutePath(t, remoteSSHIntegrationSSHPathEnv)
	configPath := remoteSSHIntegrationAbsolutePath(t, remoteSSHIntegrationConfigPathEnv)
	knownHostsPath := remoteSSHIntegrationAbsolutePath(t, remoteSSHIntegrationKnownHostsEnv)
	alias := strings.TrimSpace(os.Getenv(remoteSSHIntegrationHostAliasEnv))
	if alias == "" || alias != os.Getenv(remoteSSHIntegrationHostAliasEnv) {
		t.Fatalf("%s must contain one exact SSH Host alias", remoteSSHIntegrationHostAliasEnv)
	}
	if err := ValidateRemoteHostAlias(alias); err != nil {
		remoteSSHIntegrationFail(t, "validate SSH Host alias", err)
	}
	isolatedConfigPath, err := remoteSSHIntegrationIsolatedSSHConfig(t, alias, configPath, knownHostsPath)
	if err != nil {
		remoteSSHIntegrationFail(t, "create isolated SSH config and known_hosts", err)
	}

	entry, err := NewRemoteHostEntry(alias, "Opt-in real SSH integration")
	if err != nil {
		remoteSSHIntegrationFail(t, "create secret-free Host entry", err)
	}
	entry.SSHConfigPath = isolatedConfigPath
	productionFactory, err := NewRemoteSSHHostTransportFactory(
		&RemoteSSHTransportFactory{SSHPath: sshPath},
		entry,
	)
	if err != nil {
		remoteSSHIntegrationFail(t, "bind production SSH transport factory", err)
	}
	remoteSSHIntegrationAssertBuildMismatch(t, productionFactory, entry, buildID)

	client, err := remoteclient.New(remoteclient.Options{
		Factory:          productionFactory,
		BuildID:          buildID,
		ClientInstanceID: protocol.ClientInstanceID(entry.ClientInstanceID),
	})
	if err != nil {
		remoteSSHIntegrationFail(t, "create production Remote client", err)
	}

	detached := false
	var fixture *remoteSSHIntegrationFixture
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cleanupCancel()
		defer func() {
			remoteSSHIntegrationReport(t, "close production Remote client during cleanup", client.Close())
		}()
		if detached {
			return
		}

		// Keep the lease attached while a fixture is still discoverable. A
		// connected cleanup gets one immediate idempotent retry; if either attempt
		// drops the transport, the outer loop resumes the lease and runs the same
		// cleanup again before considering Detach.
		cleanupFixture := func() bool {
			if fixture == nil {
				return true
			}
			for attempt := 0; attempt < 2 && client.Status().State == remoteclient.StateConnected; attempt++ {
				failures := remoteSSHIntegrationCleanupFixture(cleanupCtx, client, fixture)
				for _, failure := range failures {
					remoteSSHIntegrationReport(t, failure.stage, failure.err)
				}
				if len(failures) == 0 {
					fixture = nil
					return true
				}
			}
			return false
		}
		reconnectAndCleanup := func(stage string) (bool, bool) {
			_, reconnectErr := client.Connect(cleanupCtx)
			if reconnectErr != nil {
				remoteSSHIntegrationReport(t, stage, reconnectErr)
				return false, false
			}
			return true, cleanupFixture()
		}

		fixtureClean := cleanupFixture()
		for attempt := 0; !fixtureClean && client.Status().State != remoteclient.StateConnected &&
			client.RecoveryState().ResumeLeaseID != "" && attempt < 2; attempt++ {
			stage := "reconnect production Remote client during fixture cleanup"
			if attempt > 0 {
				stage = "retry reconnect production Remote client during fixture cleanup"
			}
			connected, clean := reconnectAndCleanup(stage)
			if connected {
				fixtureClean = clean
			}
		}
		if !fixtureClean {
			remoteSSHIntegrationReport(t, "finish disposable Remote fixture cleanup before detach", errors.New("fixture cleanup remained incomplete"))
			return
		}

		detachConnected := func(stage string) bool {
			if client.Status().State != remoteclient.StateConnected {
				return false
			}
			if _, detachErr := client.Detach(cleanupCtx); detachErr != nil {
				remoteSSHIntegrationReport(t, stage, detachErr)
				return false
			}
			detached = true
			return true
		}
		if !detachConnected("detach production Remote lease during cleanup") && client.Status().State == remoteclient.StateConnected {
			detachConnected("retry production Remote detach on current transport")
		}

		// A failed detach can drop the stream without releasing the lease. Every
		// successful reconnect still passes through fixture cleanup (a no-op after
		// success) before another detach attempt.
		for attempt := 0; !detached && client.Status().State != remoteclient.StateConnected &&
			client.RecoveryState().ResumeLeaseID != "" && attempt < 2; attempt++ {
			stage := "reconnect production Remote client after detach failure"
			if attempt > 0 {
				stage = "retry reconnect production Remote client after detach failure"
			}
			connected, clean := reconnectAndCleanup(stage)
			if !connected || !clean {
				continue
			}
			if !detachConnected("retry production Remote detach during cleanup") && client.Status().State == remoteclient.StateConnected {
				detachConnected("final production Remote detach retry on current transport")
			}
		}
	})

	testCtx, cancelTest := context.WithTimeout(context.Background(), remoteSSHIntegrationTimeout)
	defer cancelTest()
	firstTransportCtx, dropFirstTransport := context.WithCancel(testCtx)
	first, err := client.Connect(firstTransportCtx)
	if err != nil {
		dropFirstTransport()
		remoteSSHIntegrationFail(t, "initialize over production SSH attach", err)
	}
	if first.Generation == 0 || first.Initialize.HostEpoch == "" || first.Initialize.Lease.LeaseID == "" {
		dropFirstTransport()
		t.Fatal("initialize omitted required connection identity")
	}
	if first.Initialize.Host.OS != "linux" || first.Initialize.Host.Arch != "amd64" {
		dropFirstTransport()
		t.Fatal("real SSH integration Host is not Linux amd64")
	}
	if err := protocol.CompareBuildID(buildID, first.Initialize.BuildID); err != nil {
		dropFirstTransport()
		remoteSSHIntegrationFail(t, "validate initialized Host build identity", err)
	}
	firstPing, err := client.Ping(testCtx)
	if err != nil {
		dropFirstTransport()
		remoteSSHIntegrationFail(t, "ping initialized Host", err)
	}
	if firstPing.HostEpoch != first.Initialize.HostEpoch || firstPing.LeaseTTL != protocol.LeaseTTLMillis {
		dropFirstTransport()
		t.Fatal("ping changed the initialized Host epoch or frozen lease TTL")
	}

	workspaces, err := remoteSSHIntegrationRequest[protocol.WorkspaceListResult](
		testCtx,
		client,
		protocol.MethodWorkspaceList,
		protocol.WorkspaceListParams{ExpectedHostEpoch: first.Initialize.HostEpoch},
	)
	if err != nil {
		dropFirstTransport()
		remoteSSHIntegrationFail(t, "list Host workspaces for reconnect fixture", err)
	}
	if len(workspaces.Items) == 0 {
		dropFirstTransport()
		t.Fatal("real SSH integration Host has no open workspace for a disposable Session")
	}
	fixture = &remoteSSHIntegrationFixture{
		workspaceID:            workspaces.Items[0].WorkspaceID,
		topicTitle:             remoteSSHIntegrationTopicPrefix + " " + remoteSSHIntegrationRandomHex(t),
		topicCreateRequestID:   remoteSSHIntegrationRequestID(t, "topic-create"),
		sessionCreateRequestID: remoteSSHIntegrationRequestID(t, "session-create"),
		shellRequestID:         remoteSSHIntegrationRequestID(t, "shell"),
		cancelRequestID:        remoteSSHIntegrationRequestID(t, "operation-cancel"),
		closeRequestID:         remoteSSHIntegrationRequestID(t, "close"),
		trashRequestID:         remoteSSHIntegrationRequestID(t, "trash"),
		purgeRequestID:         remoteSSHIntegrationRequestID(t, "purge"),
		deleteRequestID:        remoteSSHIntegrationRequestID(t, "topic-delete"),
	}
	fixture.topicCreateSent = true
	topic, err := remoteSSHIntegrationRequest[protocol.TopicCreateResult](
		testCtx,
		client,
		protocol.MethodTopicCreate,
		protocol.TopicCreateParams{
			HostMutation: protocol.HostMutation{
				RequestID:         fixture.topicCreateRequestID,
				ExpectedHostEpoch: first.Initialize.HostEpoch,
			},
			WorkspaceID: fixture.workspaceID,
			Title:       fixture.topicTitle,
		},
	)
	if err != nil {
		dropFirstTransport()
		remoteSSHIntegrationFail(t, "create disposable Remote Topic", err)
	}
	if topic.Title != fixture.topicTitle || topic.TopicID == "" || topic.SessionCount != 0 {
		dropFirstTransport()
		t.Fatal("created Remote Topic did not match the requested empty catalog record")
	}
	fixture.topicID = topic.TopicID

	fixture.sessionCreateSent = true
	created, err := remoteSSHIntegrationRequest[protocol.SessionCreateResult](
		testCtx,
		client,
		protocol.MethodSessionCreate,
		protocol.SessionCreateParams{
			HostMutation: protocol.HostMutation{
				RequestID:         fixture.sessionCreateRequestID,
				ExpectedHostEpoch: first.Initialize.HostEpoch,
			},
			WorkspaceID:             fixture.workspaceID,
			AdditionalDirectoryRefs: []protocol.DirectoryRef{},
			Topic: protocol.TopicSelection{
				Kind:    protocol.TopicExisting,
				TopicID: fixture.topicID,
			},
			Profile: protocol.ProfileSelection{},
		},
	)
	if err != nil {
		dropFirstTransport()
		remoteSSHIntegrationFail(t, "create disposable Remote Session", err)
	}
	if created.Target.WorkspaceID != fixture.workspaceID || created.TopicID != fixture.topicID ||
		created.TopicTitle != fixture.topicTitle || created.RuntimeEpoch == "" {
		dropFirstTransport()
		t.Fatal("created Remote Session did not match its Topic and workspace")
	}
	fixture.target = created.Target
	fixture.runtimeEpoch = created.RuntimeEpoch
	remoteSSHIntegrationAssertCatalogFixture(t, testCtx, client, first.Initialize.HostEpoch, fixture)

	firstSubscription, err := client.Subscribe(testCtx, protocol.SessionSubscribeParams{
		ExpectedHostEpoch: first.Initialize.HostEpoch,
		Target:            fixture.target,
		PageTurns:         20,
	})
	if err != nil {
		dropFirstTransport()
		remoteSSHIntegrationFail(t, "subscribe disposable Remote Session", err)
	}
	fixture.subscriptionID = firstSubscription.ID
	if firstSubscription.Snapshot.Target != fixture.target ||
		firstSubscription.Snapshot.RuntimeEpoch != fixture.runtimeEpoch ||
		firstSubscription.Snapshot.Meta.TopicID != fixture.topicID {
		dropFirstTransport()
		t.Fatal("initial Session snapshot did not match the created Topic and runtime")
	}

	// Retain every mutation identity before sending it. Cleanup can replay the
	// exact frozen request after response loss to recover Host-generated IDs;
	// a never-accepted replay may create work, but the same cleanup immediately
	// cancels and removes that disposable fixture.
	fixture.shellSent = true
	started, err := remoteSSHIntegrationRequest[protocol.OperationStartedResult](
		testCtx,
		client,
		protocol.MethodShellRun,
		protocol.ShellRunParams{
			SessionMutation: protocol.SessionMutation{
				RequestID:            fixture.shellRequestID,
				ExpectedHostEpoch:    first.Initialize.HostEpoch,
				Target:               fixture.target,
				ExpectedRuntimeEpoch: fixture.runtimeEpoch,
			},
			Command: remoteSSHIntegrationShellCommand,
		},
	)
	if err != nil {
		dropFirstTransport()
		remoteSSHIntegrationFail(t, "start deterministic Host Shell transcript", err)
	}
	if started.OperationID == "" || started.Disposition != "started" {
		dropFirstTransport()
		t.Fatal("Host Shell operation omitted its required start identity")
	}
	fixture.operationID = started.OperationID
	beforeEvent := remoteSSHIntegrationWaitForOperationMarker(
		t,
		testCtx,
		firstSubscription.Updates,
		firstSubscription.Snapshot.BoundarySeq,
		started.OperationID,
		remoteSSHIntegrationBeforeMarker,
	)

	// Cancel the context that owns the first production ssh.exe process. The
	// accepted Host Shell and its protocol transcript projection belong to the
	// daemon, so stream loss must retain both the lease and subscription cursor.
	fixture.subscriptionID = ""
	dropFirstTransport()
	remoteSSHIntegrationWaitForState(t, testCtx, client, remoteclient.StateDisconnected)
	recovery := client.RecoveryState()
	if recovery.ResumeLeaseID != first.Initialize.Lease.LeaseID || recovery.HostEpoch != first.Initialize.HostEpoch {
		t.Fatal("transport loss did not retain the initialized recovery identity")
	}
	if len(recovery.Subscriptions) != 1 || recovery.Subscriptions[0].Target != fixture.target ||
		recovery.Subscriptions[0].PreviousRuntimeEpoch != fixture.runtimeEpoch ||
		recovery.Subscriptions[0].LastSeq < beforeEvent.Seq {
		t.Fatal("transport loss did not retain the Session transcript recovery cursor")
	}
	recoveryCursor := recovery.Subscriptions[0].LastSeq
	// Stay locally disconnected until the Host-side command emits the gap
	// marker. The client cannot advance LastSeq while no subscription transport
	// exists, so the resumed atomic snapshot must cross this saved cursor.
	remoteSSHIntegrationWaitWhileDisconnected(t, testCtx, client, remoteSSHIntegrationGapWait)

	second, err := client.Connect(testCtx)
	if err != nil {
		remoteSSHIntegrationFail(t, "resume over a fresh production SSH attach", err)
	}
	if second.Generation <= first.Generation ||
		second.Initialize.Lease.LeaseID != first.Initialize.Lease.LeaseID ||
		second.Initialize.HostEpoch != first.Initialize.HostEpoch {
		t.Fatal("reconnect did not resume the same lease and Host epoch")
	}
	secondPing, err := client.Ping(testCtx)
	if err != nil {
		remoteSSHIntegrationFail(t, "ping resumed Host", err)
	}
	if secondPing.HostEpoch != first.Initialize.HostEpoch || secondPing.LeaseTTL != protocol.LeaseTTLMillis {
		t.Fatal("resumed ping changed the Host epoch or frozen lease TTL")
	}
	remoteSSHIntegrationAssertCatalogFixture(t, testCtx, client, second.Initialize.HostEpoch, fixture)

	secondSubscription, err := client.Subscribe(testCtx, protocol.SessionSubscribeParams{
		ExpectedHostEpoch: second.Initialize.HostEpoch,
		Target:            fixture.target,
		PageTurns:         20,
	})
	if err != nil {
		remoteSSHIntegrationFail(t, "resubscribe Session after production SSH loss", err)
	}
	fixture.subscriptionID = secondSubscription.ID
	resumedSnapshot := secondSubscription.Snapshot
	if resumedSnapshot.Target != fixture.target || resumedSnapshot.RuntimeEpoch != fixture.runtimeEpoch ||
		resumedSnapshot.Meta.TopicID != fixture.topicID || resumedSnapshot.BoundarySeq <= recoveryCursor {
		t.Fatal("resumed Session snapshot changed identity or did not cross the disconnected event gap")
	}
	if !resumedSnapshot.Runtime.Running || resumedSnapshot.Runtime.CurrentOperation == nil ||
		resumedSnapshot.Runtime.CurrentOperation.OperationID != started.OperationID ||
		resumedSnapshot.Runtime.CurrentOperation.Kind != protocol.OperationShell {
		t.Fatal("resumed snapshot could not exercise active-operation recovery")
	}
	transcript, transcriptErr := remoteSSHIntegrationSeedTranscript(resumedSnapshot.Runtime.LiveEvents)
	if transcriptErr != nil {
		remoteSSHIntegrationFail(t, "seed resumed protocol transcript from atomic snapshot", transcriptErr)
	}
	if !transcript.sawBefore || !transcript.sawGap {
		t.Fatal("resumed Session snapshot did not retain both sides of the disconnected event gap")
	}
	remoteSSHIntegrationWaitForOperationCompletion(
		t,
		testCtx,
		secondSubscription.Updates,
		resumedSnapshot.BoundarySeq,
		started.OperationID,
		transcript,
	)
	if _, err := client.Unsubscribe(testCtx, secondSubscription.ID); err != nil {
		remoteSSHIntegrationFail(t, "unsubscribe resumed Session transcript", err)
	}
	fixture.subscriptionID = ""
	if failures := remoteSSHIntegrationCleanupFixture(testCtx, client, fixture); len(failures) != 0 {
		for _, failure := range failures {
			remoteSSHIntegrationReport(t, failure.stage, failure.err)
		}
		t.FailNow()
	}
	fixture = nil

	result, err := client.Detach(testCtx)
	if err != nil {
		remoteSSHIntegrationFail(t, "detach resumed Host lease", err)
	}
	if !result.Detached ||
		client.Status().State != remoteclient.StateDisconnected ||
		client.RecoveryState().ResumeLeaseID != "" {
		t.Fatal("successful detach retained a connected state or Remote recovery identity")
	}
	detached = true
}

func TestRemoteSSHIntegrationPrivateTempDirACL(t *testing.T) {
	dir, err := remoteSSHIntegrationPrivateTempDir(t)
	if err != nil {
		if validationErr, ok := err.(remoteSSHIntegrationACLValidationError); ok {
			t.Fatalf("protected SSH integration directory failed ACL invariant %d", validationErr)
		}
		remoteSSHIntegrationFail(t, "create protected SSH integration test directory", err)
	}
	if err := remoteSSHIntegrationWritePrivateFile(filepath.Join(dir, "acl_probe"), []byte("protected")); err != nil {
		remoteSSHIntegrationFail(t, "write harmless protected-directory probe", err)
	}
}

func remoteSSHIntegrationRequest[T any](
	ctx context.Context,
	client *remoteclient.Client,
	method protocol.Method,
	params any,
) (T, error) {
	var zero T
	value, err := client.Request(ctx, method, params)
	if err != nil {
		return zero, err
	}
	result, ok := value.(T)
	if !ok {
		return zero, errors.New("Remote result type differs from the frozen registry")
	}
	return result, nil
}

func remoteSSHIntegrationRequestID(t *testing.T, purpose string) protocol.RequestID {
	t.Helper()
	return protocol.RequestID("ssh-integration-" + purpose + "-" + remoteSSHIntegrationRandomHex(t))
}

func remoteSSHIntegrationRandomHex(t *testing.T) string {
	t.Helper()
	var suffix [12]byte
	if _, err := rand.Read(suffix[:]); err != nil {
		remoteSSHIntegrationFail(t, "generate disposable Remote request identity", err)
	}
	return hex.EncodeToString(suffix[:])
}

func remoteSSHIntegrationAssertCatalogFixture(
	t *testing.T,
	ctx context.Context,
	client *remoteclient.Client,
	hostEpoch protocol.HostEpoch,
	fixture *remoteSSHIntegrationFixture,
) {
	t.Helper()
	topics, err := remoteSSHIntegrationRequest[protocol.TopicListResult](ctx, client, protocol.MethodTopicList, protocol.TopicListParams{
		ExpectedHostEpoch: hostEpoch,
		WorkspaceID:       fixture.workspaceID,
	})
	if err != nil {
		remoteSSHIntegrationFail(t, "list disposable Remote Topic", err)
	}
	topicFound := false
	for _, item := range topics.Items {
		if item.TopicID == fixture.topicID {
			topicFound = item.Title == fixture.topicTitle && item.SessionCount == 1
			break
		}
	}
	if !topicFound {
		t.Fatal("Remote Topic catalog did not retain the disposable Session")
	}

	sessions, err := remoteSSHIntegrationRequest[protocol.SessionListResult](ctx, client, protocol.MethodSessionList, protocol.SessionListParams{
		ExpectedHostEpoch: hostEpoch,
		WorkspaceID:       fixture.workspaceID,
	})
	if err != nil {
		remoteSSHIntegrationFail(t, "list disposable Remote Session", err)
	}
	sessionFound := false
	for _, item := range sessions.Items {
		if item.Target == fixture.target {
			sessionFound = item.TopicID == fixture.topicID && item.Runtime != nil &&
				item.Runtime.RuntimeEpoch == fixture.runtimeEpoch
			break
		}
	}
	if !sessionFound {
		t.Fatal("Remote Session catalog did not retain the created runtime")
	}
}

func remoteSSHIntegrationWaitForOperationMarker(
	t *testing.T,
	ctx context.Context,
	updates <-chan remoteclient.SubscriptionUpdate,
	boundary uint64,
	operationID protocol.OperationID,
	marker string,
) protocol.SessionEvent {
	t.Helper()
	lastSeq := boundary
	var progress strings.Builder
	for {
		event := remoteSSHIntegrationNextEvent(t, ctx, updates, &lastSeq)
		if event.OperationID != operationID {
			continue
		}
		if event.TurnID != "" {
			t.Fatal("Host Shell event also carried a Turn identity")
		}
		if event.Event.Kind != "tool_progress" || event.Event.Tool == nil {
			continue
		}
		progress.WriteString(event.Event.Tool.Output)
		if strings.Contains(progress.String(), marker) {
			return event
		}
	}
}

func remoteSSHIntegrationNextEvent(
	t *testing.T,
	ctx context.Context,
	updates <-chan remoteclient.SubscriptionUpdate,
	lastSeq *uint64,
) protocol.SessionEvent {
	t.Helper()
	select {
	case <-ctx.Done():
		t.Fatal("timed out waiting for the deterministic Host transcript event")
	case update, ok := <-updates:
		if !ok {
			t.Fatal("Session subscription closed before the deterministic Host transcript completed")
		}
		if update.Err != nil {
			remoteSSHIntegrationFail(t, "receive deterministic Host transcript event", update.Err)
		}
		if update.Resync != nil || update.SnapshotRequired || update.Event == nil {
			t.Fatal("Session transcript required an unexpected snapshot refresh")
		}
		if update.Event.Seq != *lastSeq+1 {
			t.Fatal("Session transcript event sequence was not contiguous")
		}
		*lastSeq = update.Event.Seq
		return *update.Event
	}
	return protocol.SessionEvent{}
}

func remoteSSHIntegrationWaitWhileDisconnected(
	t *testing.T,
	ctx context.Context,
	client *remoteclient.Client,
	duration time.Duration,
) {
	t.Helper()
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		t.Fatal("test context ended before the Host-side disconnected event gap")
	case <-timer.C:
	}
	if client.Status().State != remoteclient.StateDisconnected {
		t.Fatal("Remote client did not remain disconnected through the Host event gap")
	}
}

type remoteSSHIntegrationTranscriptState struct {
	toolID    string
	progress  string
	sawBefore bool
	sawGap    bool
	sawAfter  bool
	sawResult bool
	sawDone   bool
}

func remoteSSHIntegrationSeedTranscript(events []eventwire.Event) (remoteSSHIntegrationTranscriptState, error) {
	var state remoteSSHIntegrationTranscriptState
	for _, item := range events {
		if err := state.apply(item); err != nil {
			return remoteSSHIntegrationTranscriptState{}, err
		}
	}
	if state.sawDone {
		return remoteSSHIntegrationTranscriptState{}, errors.New("atomic snapshot already contained a completed operation")
	}
	return state, nil
}

func (s *remoteSSHIntegrationTranscriptState) apply(item eventwire.Event) error {
	switch item.Kind {
	case "tool_dispatch":
		if item.Tool != nil && item.Tool.Name == "bash" {
			if s.toolID != "" && s.toolID != item.Tool.ID {
				return errors.New("protocol transcript replaced the active Shell tool")
			}
			s.toolID = item.Tool.ID
		}
	case "tool_progress":
		if item.Tool == nil || s.toolID == "" || item.Tool.ID != s.toolID {
			return nil
		}
		if s.sawResult || s.sawDone {
			return errors.New("Shell progress arrived after its terminal boundary")
		}
		s.progress += item.Tool.Output
		before, gap, after := remoteSSHIntegrationMarkerPositions(s.progress)
		if gap >= 0 && (before < 0 || gap < before) {
			return errors.New("disconnected gap marker preceded the before marker")
		}
		if after >= 0 && (gap < 0 || after < gap) {
			return errors.New("after marker preceded the disconnected gap marker")
		}
		s.sawBefore = before >= 0
		s.sawGap = gap >= 0
		s.sawAfter = after >= 0
	case "tool_result":
		if item.Tool == nil || s.toolID == "" || item.Tool.ID != s.toolID {
			return nil
		}
		if s.sawResult || s.sawDone || !s.sawAfter {
			return errors.New("Shell result did not follow the after marker exactly once")
		}
		before, gap, after := remoteSSHIntegrationMarkerPositions(item.Tool.Output)
		if before < 0 || gap < before || after < gap {
			return errors.New("Shell result lost the ordered before-gap-after transcript")
		}
		s.sawResult = true
	case "operation_done":
		if s.sawDone || !s.sawResult || item.Err != "" {
			return errors.New("Operation completion did not follow a successful Shell result exactly once")
		}
		s.sawDone = true
	}
	return nil
}

func remoteSSHIntegrationMarkerPositions(value string) (before int, gap int, after int) {
	return strings.Index(value, remoteSSHIntegrationBeforeMarker),
		strings.Index(value, remoteSSHIntegrationGapMarker),
		strings.Index(value, remoteSSHIntegrationAfterMarker)
}

func remoteSSHIntegrationWaitForOperationCompletion(
	t *testing.T,
	ctx context.Context,
	updates <-chan remoteclient.SubscriptionUpdate,
	boundary uint64,
	operationID protocol.OperationID,
	transcript remoteSSHIntegrationTranscriptState,
) {
	t.Helper()
	lastSeq := boundary
	for {
		event := remoteSSHIntegrationNextEvent(t, ctx, updates, &lastSeq)
		if event.OperationID != operationID {
			continue
		}
		if event.TurnID != "" {
			t.Fatal("resumed Host Shell event also carried a Turn identity")
		}
		if err := transcript.apply(event.Event); err != nil {
			remoteSSHIntegrationFail(t, "validate resumed Host Shell N+1 transcript", err)
		}
		if transcript.sawDone {
			return
		}
	}
}

func remoteSSHIntegrationCleanupFixture(
	ctx context.Context,
	client *remoteclient.Client,
	fixture *remoteSSHIntegrationFixture,
) []remoteSSHIntegrationFailure {
	if fixture == nil {
		return nil
	}
	var failures []remoteSSHIntegrationFailure
	record := func(stage string, err error) {
		if err != nil {
			failures = append(failures, remoteSSHIntegrationFailure{stage: stage, err: err})
		}
	}
	hostEpoch := client.Status().HostEpoch
	if hostEpoch == "" {
		record("resolve Host identity for disposable fixture cleanup", errors.New("connected Remote client omitted Host identity"))
		return failures
	}

	// These exact replays are cleanup discovery, not retries with new semantic
	// intent. If an RPC response was lost, the Host returns its cached opaque
	// identity. If the request never arrived, the disposable record may be
	// created here and is immediately removed by the same cleanup sequence.
	if fixture.topicCreateSent && fixture.topicID == "" {
		topic, err := remoteSSHIntegrationRequest[protocol.TopicCreateResult](ctx, client, protocol.MethodTopicCreate, protocol.TopicCreateParams{
			HostMutation: protocol.HostMutation{
				RequestID:         fixture.topicCreateRequestID,
				ExpectedHostEpoch: hostEpoch,
			},
			WorkspaceID: fixture.workspaceID,
			Title:       fixture.topicTitle,
		})
		if err != nil {
			record("replay disposable Remote Topic creation during cleanup", err)
		} else if topic.TopicID == "" || topic.Title != fixture.topicTitle {
			record("replay disposable Remote Topic creation during cleanup", errors.New("Topic replay changed its frozen result"))
		} else {
			fixture.topicID = topic.TopicID
		}
	}
	if fixture.sessionCreateSent && fixture.target.WorkspaceID == "" && fixture.topicID != "" {
		created, err := remoteSSHIntegrationRequest[protocol.SessionCreateResult](ctx, client, protocol.MethodSessionCreate, protocol.SessionCreateParams{
			HostMutation: protocol.HostMutation{
				RequestID:         fixture.sessionCreateRequestID,
				ExpectedHostEpoch: hostEpoch,
			},
			WorkspaceID:             fixture.workspaceID,
			AdditionalDirectoryRefs: []protocol.DirectoryRef{},
			Topic: protocol.TopicSelection{
				Kind:    protocol.TopicExisting,
				TopicID: fixture.topicID,
			},
			Profile: protocol.ProfileSelection{},
		})
		if err != nil {
			record("replay disposable Remote Session creation during cleanup", err)
		} else if created.Target.WorkspaceID != fixture.workspaceID || created.TopicID != fixture.topicID || created.RuntimeEpoch == "" {
			record("replay disposable Remote Session creation during cleanup", errors.New("Session replay changed its frozen result"))
		} else {
			fixture.target = created.Target
			fixture.runtimeEpoch = created.RuntimeEpoch
		}
	}

	if fixture.target.WorkspaceID == "" && fixture.topicID != "" {
		limit := 1000
		listed, err := remoteSSHIntegrationRequest[protocol.SessionListResult](ctx, client, protocol.MethodSessionList, protocol.SessionListParams{
			ExpectedHostEpoch: hostEpoch,
			WorkspaceID:       fixture.workspaceID,
			Limit:             &limit,
		})
		if err != nil {
			record("discover partially-created Remote Session for cleanup", err)
		} else {
			for _, item := range listed.Items {
				if item.TopicID != fixture.topicID {
					continue
				}
				fixture.target = item.Target
				if item.Runtime != nil {
					fixture.runtimeEpoch = item.Runtime.RuntimeEpoch
				}
				break
			}
		}
	}

	if fixture.shellSent && fixture.operationID == "" && fixture.target.WorkspaceID != "" && fixture.runtimeEpoch != "" {
		started, err := remoteSSHIntegrationRequest[protocol.OperationStartedResult](ctx, client, protocol.MethodShellRun, protocol.ShellRunParams{
			SessionMutation: protocol.SessionMutation{
				RequestID:            fixture.shellRequestID,
				ExpectedHostEpoch:    hostEpoch,
				Target:               fixture.target,
				ExpectedRuntimeEpoch: fixture.runtimeEpoch,
			},
			Command: remoteSSHIntegrationShellCommand,
		})
		if err != nil {
			record("replay disposable Host Shell start during cleanup", err)
		} else if started.OperationID == "" || started.Disposition != "started" {
			record("replay disposable Host Shell start during cleanup", errors.New("Shell replay changed its frozen result"))
		} else {
			fixture.operationID = started.OperationID
		}
	}
	if fixture.operationID != "" && fixture.runtimeEpoch != "" {
		_, err := remoteSSHIntegrationRequest[protocol.OperationCancelResult](ctx, client, protocol.MethodOperationCancel, protocol.OperationCancelParams{
			SessionMutation: protocol.SessionMutation{
				RequestID:            fixture.cancelRequestID,
				ExpectedHostEpoch:    hostEpoch,
				Target:               fixture.target,
				ExpectedRuntimeEpoch: fixture.runtimeEpoch,
			},
			ExpectedOperationID: fixture.operationID,
		})
		if err != nil && !remoteSSHIntegrationHasRemoteCode(err, protocol.ErrOperationNotActive) {
			record("cancel disposable Host Shell during cleanup", err)
		}
	}
	if fixture.subscriptionID != "" {
		_, err := client.Unsubscribe(ctx, fixture.subscriptionID)
		if err != nil && !remoteSSHIntegrationHasRemoteCode(err, protocol.ErrSubscriptionNotFound) {
			record("unsubscribe disposable Remote Session during cleanup", err)
		} else {
			fixture.subscriptionID = ""
		}
	}

	if fixture.target.WorkspaceID != "" {
		if err := remoteSSHIntegrationWaitForFixtureIdle(ctx, client, hostEpoch, fixture.target); err != nil {
			record("wait for disposable Remote Session to become idle", err)
		}
		if fixture.runtimeEpoch != "" {
			closed, err := remoteSSHIntegrationRequest[protocol.SessionCloseResult](ctx, client, protocol.MethodSessionClose, protocol.SessionCloseParams{
				SessionMutation: protocol.SessionMutation{
					RequestID:            fixture.closeRequestID,
					ExpectedHostEpoch:    hostEpoch,
					Target:               fixture.target,
					ExpectedRuntimeEpoch: fixture.runtimeEpoch,
				},
			})
			if err != nil {
				record("close disposable Remote Session", err)
			} else if closed.Disposition == protocol.SessionRetainedActive {
				record("close disposable Remote Session", errors.New("Remote Session remained active after transcript completion"))
			}
		}
		if _, err := remoteSSHIntegrationRequest[protocol.SessionTrashResult](ctx, client, protocol.MethodSessionTrash, protocol.SessionTrashParams{
			SessionRecordMutation: protocol.SessionRecordMutation{
				RequestID:         fixture.trashRequestID,
				ExpectedHostEpoch: hostEpoch,
				Target:            fixture.target,
			},
			Guard: protocol.TrashNormal,
		}); err != nil {
			record("trash disposable Remote Session", err)
		}
		if _, err := remoteSSHIntegrationRequest[protocol.SessionPurgeResult](ctx, client, protocol.MethodSessionPurge, protocol.SessionPurgeParams{
			SessionRecordMutation: protocol.SessionRecordMutation{
				RequestID:         fixture.purgeRequestID,
				ExpectedHostEpoch: hostEpoch,
				Target:            fixture.target,
			},
			Guard: protocol.TrashNormal,
		}); err != nil {
			record("purge disposable Remote Session", err)
		}
	}
	if fixture.topicID != "" {
		if _, err := remoteSSHIntegrationRequest[protocol.TopicDeleteResult](ctx, client, protocol.MethodTopicDelete, protocol.TopicDeleteParams{
			HostMutation: protocol.HostMutation{
				RequestID:         fixture.deleteRequestID,
				ExpectedHostEpoch: hostEpoch,
			},
			WorkspaceID: fixture.workspaceID,
			TopicID:     fixture.topicID,
		}); err != nil {
			record("delete disposable Remote Topic", err)
		}
	}
	return failures
}

func remoteSSHIntegrationWaitForFixtureIdle(
	ctx context.Context,
	client *remoteclient.Client,
	hostEpoch protocol.HostEpoch,
	target protocol.RuntimeTarget,
) error {
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	for {
		listed, err := remoteSSHIntegrationRequest[protocol.SessionListResult](ctx, client, protocol.MethodSessionList, protocol.SessionListParams{
			ExpectedHostEpoch: hostEpoch,
			WorkspaceID:       target.WorkspaceID,
		})
		if err != nil {
			return err
		}
		found := false
		for _, item := range listed.Items {
			if item.Target != target {
				continue
			}
			found = true
			if item.Runtime == nil || (!item.Runtime.Running && !item.Runtime.PendingPrompt && item.Runtime.ActiveJobs == 0) {
				return nil
			}
			break
		}
		if !found {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func remoteSSHIntegrationHasRemoteCode(err error, code protocol.ReasonixErrorCode) bool {
	var remoteErr *protocol.RemoteError
	return errors.As(err, &remoteErr) && remoteErr != nil && remoteErr.Code == code
}

func remoteSSHIntegrationAssertBuildMismatch(
	t *testing.T,
	factory RemoteSSHHostTransportFactory,
	entry RemoteHostEntry,
	buildID protocol.BuildID,
) {
	t.Helper()
	mismatched := buildID
	mismatched.SourceRevision = strings.Repeat("0", 40)
	if mismatched.SourceRevision == buildID.SourceRevision {
		mismatched.SourceRevision = strings.Repeat("1", 40)
	}
	if err := mismatched.Validate(); err != nil {
		remoteSSHIntegrationFail(t, "construct mismatched Build ID", err)
	}
	client, err := remoteclient.New(remoteclient.Options{
		Factory:          factory,
		BuildID:          mismatched,
		ClientInstanceID: protocol.ClientInstanceID(entry.ClientInstanceID),
	})
	if err != nil {
		remoteSSHIntegrationFail(t, "create mismatched Remote client", err)
	}
	defer func() {
		remoteSSHIntegrationReport(t, "close mismatched Remote client", client.Close())
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, connectErr := client.Connect(ctx)
	if connectErr == nil {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
		_, detachErr := client.Detach(cleanupCtx)
		cleanupCancel()
		remoteSSHIntegrationReport(t, "detach mismatched Remote lease", detachErr)
		t.Fatal("Remote attach accepted a mismatched source revision")
	}
	var remoteErr *protocol.RemoteError
	if !errors.As(connectErr, &remoteErr) || remoteErr == nil || remoteErr.Code != protocol.ErrVersionMismatch {
		t.Fatal("Remote attach did not strictly reject Build ID mismatch with VERSION_MISMATCH")
	}
	if client.Status().State != remoteclient.StateDisconnected || client.RecoveryState().ResumeLeaseID != "" {
		t.Fatal("Build ID mismatch unexpectedly fell back or retained recovery state")
	}
}

func remoteSSHIntegrationAbsolutePath(t *testing.T, envName string) string {
	t.Helper()
	value := os.Getenv(envName)
	if value == "" || !filepath.IsAbs(value) || filepath.Clean(value) != value {
		t.Fatalf("%s must be an explicit absolute clean path", envName)
	}
	return value
}

func remoteSSHIntegrationIsolatedSSHConfig(
	t *testing.T,
	alias string,
	configPath string,
	knownHostsPath string,
) (string, error) {
	t.Helper()
	includePath, err := remoteSSHIntegrationQuotedConfigPath(configPath)
	if err != nil {
		return "", err
	}
	knownHosts, err := os.ReadFile(knownHostsPath)
	if err != nil {
		return "", err
	}
	if len(knownHosts) == 0 {
		return "", errors.New("known_hosts source is empty")
	}

	dir, err := remoteSSHIntegrationPrivateTempDir(t)
	if err != nil {
		return "", err
	}
	isolatedKnownHosts := filepath.Join(dir, "known_hosts")
	if err := remoteSSHIntegrationWritePrivateFile(isolatedKnownHosts, knownHosts); err != nil {
		return "", err
	}
	quotedKnownHosts, err := remoteSSHIntegrationQuotedConfigPath(isolatedKnownHosts)
	if err != nil {
		return "", err
	}
	wrapperPath := filepath.Join(dir, "ssh_config")
	// The production transport also supplies StrictHostKeyChecking=ask on the
	// command line, so the wrapper matches that effective policy. Authentication
	// stays non-interactive and public-key-only even if the included config has
	// broader fallback methods.
	wrapper := strings.Join([]string{
		"Host " + alias,
		"    StrictHostKeyChecking ask",
		"    BatchMode yes",
		"    PasswordAuthentication no",
		"    KbdInteractiveAuthentication no",
		"    ChallengeResponseAuthentication no",
		"    PreferredAuthentications publickey",
		"    UpdateHostKeys no",
		"    UserKnownHostsFile " + quotedKnownHosts,
		"    GlobalKnownHostsFile " + quotedKnownHosts,
		"    Include " + includePath,
		"",
	}, "\n")
	if err := remoteSSHIntegrationWritePrivateFile(wrapperPath, []byte(wrapper)); err != nil {
		return "", err
	}
	return wrapperPath, nil
}

func remoteSSHIntegrationPrivateTempDir(t *testing.T) (string, error) {
	t.Helper()
	dir := t.TempDir()
	path, err := windows.UTF16PtrFromString(dir)
	if err != nil {
		return "", err
	}
	// Hold the directory without FILE_SHARE_DELETE until all later-registered
	// client/SSH cleanup has finished. The path therefore cannot be renamed or
	// replaced after its handle-scoped DACL is protected and verified.
	handle, err := windows.CreateFile(
		path,
		windows.READ_CONTROL|windows.WRITE_DAC|windows.FILE_READ_ATTRIBUTES,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE,
		nil,
		windows.OPEN_EXISTING,
		windows.FILE_FLAG_BACKUP_SEMANTICS|windows.FILE_FLAG_OPEN_REPARSE_POINT,
		0,
	)
	if err != nil {
		return "", err
	}
	t.Cleanup(func() {
		remoteSSHIntegrationReport(t, "close protected SSH integration directory handle", windows.CloseHandle(handle))
	})
	var info windows.ByHandleFileInformation
	if err := windows.GetFileInformationByHandle(handle, &info); err != nil {
		return "", err
	}
	if info.FileAttributes&windows.FILE_ATTRIBUTE_DIRECTORY == 0 {
		return "", remoteSSHIntegrationACLHandleNotDirectory
	}
	if info.FileAttributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
		return "", remoteSSHIntegrationACLHandleIsReparsePoint
	}

	trustees, err := remoteSSHIntegrationPrivateACLTrustees()
	if err != nil {
		return "", err
	}
	var sddl strings.Builder
	sddl.WriteString("D:P")
	for _, trustee := range trustees {
		value := trustee.sid.String()
		if value == "" {
			return "", errors.New("private ACL trustee could not be encoded")
		}
		sddl.WriteString("(A;OICI;FA;;;")
		sddl.WriteString(value)
		sddl.WriteString(")")
	}
	sourceDescriptor, err := windows.SecurityDescriptorFromString(sddl.String())
	if err != nil {
		return "", err
	}
	acl, _, err := sourceDescriptor.DACL()
	if err != nil {
		return "", err
	}
	if err := windows.SetSecurityInfo(
		handle,
		windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION,
		nil,
		nil,
		acl,
		nil,
	); err != nil {
		return "", err
	}
	descriptor, err := windows.GetSecurityInfo(
		handle,
		windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION,
	)
	if err != nil {
		return "", err
	}
	if err := remoteSSHIntegrationValidatePrivateACL(descriptor, trustees); err != nil {
		return "", err
	}
	return dir, nil
}

func remoteSSHIntegrationPrivateACLTrustees() ([]remoteSSHIntegrationACLTrustee, error) {
	current, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return nil, err
	}
	system, err := windows.CreateWellKnownSid(windows.WinLocalSystemSid)
	if err != nil {
		return nil, err
	}
	administrators, err := windows.CreateWellKnownSid(windows.WinBuiltinAdministratorsSid)
	if err != nil {
		return nil, err
	}
	candidates := []remoteSSHIntegrationACLTrustee{
		{sid: current.User.Sid},
		{sid: system},
		{sid: administrators},
	}
	trustees := make([]remoteSSHIntegrationACLTrustee, 0, len(candidates))
	for _, candidate := range candidates {
		duplicate := false
		for _, existing := range trustees {
			if existing.sid.Equals(candidate.sid) {
				duplicate = true
				break
			}
		}
		if !duplicate {
			trustees = append(trustees, candidate)
		}
	}
	return trustees, nil
}

func remoteSSHIntegrationValidatePrivateACL(
	descriptor *windows.SECURITY_DESCRIPTOR,
	trustees []remoteSSHIntegrationACLTrustee,
) error {
	if descriptor == nil {
		return remoteSSHIntegrationACLDescriptorMissing
	}
	control, _, err := descriptor.Control()
	if err != nil {
		return err
	}
	if control&windows.SE_DACL_PROTECTED == 0 {
		return remoteSSHIntegrationACLNotProtected
	}
	dacl, defaulted, err := descriptor.DACL()
	if err != nil {
		return err
	}
	if dacl == nil {
		return remoteSSHIntegrationACLDACLMissing
	}
	if defaulted {
		return remoteSSHIntegrationACLDACLDefaulted
	}
	if int(dacl.AceCount) != len(trustees) {
		return remoteSSHIntegrationACLTrusteeCountMismatch
	}
	seen := make([]bool, len(trustees))
	for index := uint16(0); index < dacl.AceCount; index++ {
		var ace *windows.ACCESS_ALLOWED_ACE
		if err := windows.GetAce(dacl, uint32(index), &ace); err != nil {
			return err
		}
		if ace == nil || ace.Header.AceType != windows.ACCESS_ALLOWED_ACE_TYPE {
			return remoteSSHIntegrationACLEntryTypeInvalid
		}
		const fileAllAccess = windows.STANDARD_RIGHTS_REQUIRED | windows.SYNCHRONIZE | 0x1ff
		if ace.Mask != windows.ACCESS_MASK(windows.GENERIC_ALL) && ace.Mask != windows.ACCESS_MASK(fileAllAccess) {
			return remoteSSHIntegrationACLPermissionInvalid
		}
		flags := ace.Header.AceFlags
		inherit := uint8(windows.OBJECT_INHERIT_ACE | windows.CONTAINER_INHERIT_ACE)
		if flags != inherit {
			return remoteSSHIntegrationACLFlagsInvalid
		}
		aceSID := (*windows.SID)(unsafe.Pointer(&ace.SidStart))
		matched := -1
		for trusteeIndex, trustee := range trustees {
			if trustee.sid.Equals(aceSID) {
				matched = trusteeIndex
				break
			}
		}
		if matched < 0 || seen[matched] {
			return remoteSSHIntegrationACLTrusteeUnexpected
		}
		seen[matched] = true
	}
	for _, matched := range seen {
		if !matched {
			return remoteSSHIntegrationACLTrusteeMissing
		}
	}
	return nil
}

func remoteSSHIntegrationWritePrivateFile(path string, content []byte) error {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0)
	if err != nil {
		return err
	}
	written, writeErr := file.Write(content)
	closeErr := file.Close()
	if writeErr != nil {
		return writeErr
	}
	if written != len(content) {
		return errors.New("protected file write was incomplete")
	}
	return closeErr
}

func remoteSSHIntegrationQuotedConfigPath(value string) (string, error) {
	value = filepath.ToSlash(value)
	if strings.ContainsAny(value, "\"\r\n") {
		return "", errors.New("SSH config path contains an unsupported character")
	}
	return "\"" + value + "\"", nil
}

func remoteSSHIntegrationWaitForState(
	t *testing.T,
	ctx context.Context,
	client *remoteclient.Client,
	want remoteclient.ConnectionState,
) {
	t.Helper()
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	for {
		if client.Status().State == want {
			return
		}
		select {
		case <-ctx.Done():
			t.Fatal("Remote client did not observe the expected transport state")
		case <-ticker.C:
		}
	}
}

// Keep operational failures useful without ever formatting raw SSH stderr or
// opaque client/lease identities into test output.
func remoteSSHIntegrationFail(t *testing.T, stage string, err error) {
	t.Helper()
	if err == nil {
		t.Fatalf("%s failed", stage)
	}
	var remoteErr *protocol.RemoteError
	if errors.As(err, &remoteErr) && remoteErr != nil {
		t.Fatalf("%s failed with Remote code %s", stage, remoteErr.Code)
	}
	t.Fatalf("%s failed (%T)", stage, err)
}

func remoteSSHIntegrationReport(t *testing.T, stage string, err error) {
	t.Helper()
	if err == nil {
		return
	}
	var remoteErr *protocol.RemoteError
	if errors.As(err, &remoteErr) && remoteErr != nil {
		t.Errorf("%s failed with Remote code %s", stage, remoteErr.Code)
		return
	}
	t.Errorf("%s failed (%T)", stage, err)
}
