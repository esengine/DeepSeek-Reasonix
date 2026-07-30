//go:build windows && reasonix_remote_integration

package main

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"reasonix/internal/config"
	"reasonix/internal/provider"
	remotebroker "reasonix/internal/remote/broker"
	"reasonix/internal/remote/protocol"
	workbenchclient "reasonix/internal/remote/workbench/client"
)

const (
	remoteWorkbenchIntegrationGateEnv        = "REASONIX_REMOTE_WORKBENCH_INTEGRATION"
	remoteWorkbenchIntegrationHostEnv        = "REASONIX_REMOTE_WORKBENCH_HOST"
	remoteWorkbenchIntegrationPortEnv        = "REASONIX_REMOTE_WORKBENCH_PORT"
	remoteWorkbenchIntegrationUserEnv        = "REASONIX_REMOTE_WORKBENCH_USER"
	remoteWorkbenchIntegrationIdentityEnv    = "REASONIX_REMOTE_WORKBENCH_IDENTITY_FILE"
	remoteWorkbenchIntegrationWorkspaceEnv   = "REASONIX_REMOTE_WORKBENCH_WORKSPACE"
	remoteWorkbenchIntegrationVersionEnv     = "REASONIX_REMOTE_WORKBENCH_EXPECTED_VERSION"
	remoteWorkbenchIntegrationExpectedSHAEnv = "REASONIX_REMOTE_WORKBENCH_EXPECTED_SHA"
	remoteWorkbenchIntegrationFingerprintEnv = "REASONIX_REMOTE_WORKBENCH_FINGERPRINT"
	remoteWorkbenchIntegrationMarker         = "reasonix-remote-workbench-physical-tool-result"
)

type remoteWorkbenchAcceptanceProvider struct {
	requests chan provider.Request
	mu       sync.Mutex
	calls    int
}

func (p *remoteWorkbenchAcceptanceProvider) Name() string { return "physical-desktop-stub" }

func (p *remoteWorkbenchAcceptanceProvider) Stream(ctx context.Context, request provider.Request) (<-chan provider.Chunk, error) {
	select {
	case p.requests <- request:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	p.mu.Lock()
	call := p.calls
	p.calls++
	p.mu.Unlock()

	chunks := make(chan provider.Chunk, 3)
	switch call {
	case 0:
		chunks <- provider.Chunk{Type: provider.ChunkReasoning, Text: "inspect the Linux workspace before answering"}
		chunks <- provider.Chunk{Type: provider.ChunkToolCall, ToolCall: &provider.ToolCall{
			ID: "physical-read-1", Name: "read_file", Arguments: `{"path":"broker-proof.txt"}`,
		}}
		chunks <- provider.Chunk{Type: provider.ChunkDone}
	case 1:
		chunks <- provider.Chunk{Type: provider.ChunkText, Text: "physical Windows to Linux tool loop complete"}
		chunks <- provider.Chunk{Type: provider.ChunkDone}
	default:
		chunks <- provider.Chunk{Type: provider.ChunkError, Err: fmt.Errorf("unexpected physical Provider call %d", call+1)}
	}
	close(chunks)
	return chunks, nil
}

func (*remoteWorkbenchAcceptanceProvider) RequiresToolCallReasoning() bool      { return true }
func (*remoteWorkbenchAcceptanceProvider) WarnOnMissingToolCallReasoning() bool { return false }

func TestRemoteWorkbenchWindowsToLinuxPhysicalAcceptance(t *testing.T) {
	if os.Getenv(remoteWorkbenchIntegrationGateEnv) != "1" {
		t.Skip("set REASONIX_REMOTE_WORKBENCH_INTEGRATION=1 to run the physical Windows to Linux acceptance test")
	}

	host := requiredRemoteWorkbenchIntegrationEnv(t, remoteWorkbenchIntegrationHostEnv)
	user := requiredRemoteWorkbenchIntegrationEnv(t, remoteWorkbenchIntegrationUserEnv)
	identityFile := requiredRemoteWorkbenchIntegrationEnv(t, remoteWorkbenchIntegrationIdentityEnv)
	workspace := requiredRemoteWorkbenchIntegrationEnv(t, remoteWorkbenchIntegrationWorkspaceEnv)
	expectedVersion := requiredRemoteWorkbenchIntegrationEnv(t, remoteWorkbenchIntegrationVersionEnv)
	expectedSHA := requiredRemoteWorkbenchIntegrationEnv(t, remoteWorkbenchIntegrationExpectedSHAEnv)
	expectedFingerprint := requiredRemoteWorkbenchIntegrationEnv(t, remoteWorkbenchIntegrationFingerprintEnv)
	port, err := strconv.Atoi(requiredRemoteWorkbenchIntegrationEnv(t, remoteWorkbenchIntegrationPortEnv))
	if err != nil || port < 1 || port > 65535 {
		t.Fatalf("%s must be a valid TCP port", remoteWorkbenchIntegrationPortEnv)
	}

	buildID := protocol.CurrentBuildID(version)
	if buildID.ProductVersion != expectedVersion {
		t.Fatalf("acceptance binary product version = %q, want %q", buildID.ProductVersion, expectedVersion)
	}
	if buildID.SourceRevision != expectedSHA {
		t.Fatalf("acceptance binary source revision = %q, want exact clean SHA %q", buildID.SourceRevision, expectedSHA)
	}

	entry := config.RemoteHostEntry{
		Name: "physical-acceptance", Host: host, Port: port, User: user,
		IdentityFile: identityFile, Workspace: workspace,
	}
	providerStub := &remoteWorkbenchAcceptanceProvider{requests: make(chan provider.Request, 2)}
	events := make(chan protocol.SessionEvent, 128)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	newClient := func(generation uint64) (*workbenchclient.Client, *windowsWorkbenchSSHFactory) {
		t.Helper()
		rawFactory, factoryErr := newWindowsWorkbenchSSHFactory(entry, func(context.Context, RemoteAskPassPrompt) (RemoteAskPassAnswer, error) {
			return RemoteAskPassAnswer{}, fmt.Errorf("unexpected SSH prompt; acceptance fixture must provide a trusted host key and non-interactive identity")
		})
		if factoryErr != nil {
			t.Fatal(factoryErr)
		}
		factory := rawFactory.(*windowsWorkbenchSSHFactory)
		brokerOpts := remotebroker.Options{
			Catalog: func(context.Context, map[string]struct{}) ([]protocol.BrokerProviderDescriptor, error) {
				return []protocol.BrokerProviderDescriptor{
					remotebroker.DescriptorFromProvider("physical/stub", "Physical Desktop stub", "stub", providerStub, []string{"high"}, "high", false, 128_000, nil),
				}, nil
			},
			Open: func(streamCtx context.Context, ref, _ string, request provider.Request) (<-chan provider.Chunk, error) {
				if ref != "physical/stub" {
					return nil, fmt.Errorf("unexpected Provider ref %q", ref)
				}
				return providerStub.Stream(streamCtx, request)
			},
			Authorize: func() error {
				peer, ok := factory.PeerIdentity()
				if !ok || peer.Fingerprint != expectedFingerprint {
					return fmt.Errorf("authenticated SSH peer fingerprint did not match the acceptance fixture")
				}
				return nil
			},
		}
		client, connectErr := workbenchclient.Connect(ctx, factory, generation, brokerOpts, map[string]any{
			"productVersion": buildID.ProductVersion, "sourceRevision": buildID.SourceRevision,
			"protocolVersion": buildID.ProtocolVersion, "schemaHash": buildID.SchemaHash,
		}, workspace, workbenchclient.Callbacks{OnSessionEvent: func(event protocol.SessionEvent) { events <- event }})
		if connectErr != nil {
			t.Fatal(connectErr)
		}
		return client, factory
	}

	client, firstFactory := newClient(1)
	created, err := client.CreateSession(ctx, "physical/stub", "high")
	if err != nil {
		t.Fatal(err)
	}
	subscribed, err := client.Subscribe(ctx, 20)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Submit(ctx, "read the physical acceptance marker"); err != nil {
		t.Fatal(err)
	}

	firstRequest := receiveRemoteWorkbenchAcceptanceRequest(t, ctx, providerStub.requests)
	if len(firstRequest.Messages) == 0 || firstRequest.Messages[len(firstRequest.Messages)-1].Content != "read the physical acceptance marker" {
		t.Fatalf("initial Broker request lost the user prompt: %+v", firstRequest.Messages)
	}
	secondRequest := receiveRemoteWorkbenchAcceptanceRequest(t, ctx, providerStub.requests)
	var sawReasoningToolCall, sawLinuxToolResult bool
	for _, message := range secondRequest.Messages {
		if message.Role == provider.RoleAssistant && strings.Contains(message.ReasoningContent, "inspect the Linux workspace") &&
			len(message.ToolCalls) == 1 && message.ToolCalls[0].ID == "physical-read-1" && message.ToolCalls[0].Name == "read_file" {
			sawReasoningToolCall = true
		}
		if message.Role == provider.RoleTool && message.ToolCallID == "physical-read-1" &&
			message.Name == "read_file" && strings.Contains(message.Content, remoteWorkbenchIntegrationMarker) {
			sawLinuxToolResult = true
		}
	}
	if !sawReasoningToolCall || !sawLinuxToolResult {
		t.Fatalf("second Broker request lost reasoning/tool replay or the Linux tool result: %+v", secondRequest.Messages)
	}

	seenDispatch, seenResult, seenText, seenDone := false, false, false, false
	for !seenDone {
		select {
		case event := <-events:
			if event.SubscriptionID != subscribed.SubscriptionID {
				t.Fatalf("session event subscription = %q, want %q", event.SubscriptionID, subscribed.SubscriptionID)
			}
			seenDispatch = seenDispatch || (event.Event.Kind == "tool_dispatch" && event.Event.Tool.Name == "read_file")
			seenResult = seenResult || (event.Event.Kind == "tool_result" && event.Event.Tool.Name == "read_file" && strings.Contains(event.Event.Tool.Output, remoteWorkbenchIntegrationMarker))
			seenText = seenText || (event.Event.Kind == "text" && strings.Contains(event.Event.Text, "physical Windows to Linux tool loop complete"))
			seenDone = event.Event.Kind == "turn_done"
		case <-ctx.Done():
			t.Fatal("timed out waiting for the physical Remote turn")
		}
	}
	if !seenDispatch || !seenResult || !seenText {
		t.Fatalf("physical projected events incomplete: dispatch=%v result=%v text=%v", seenDispatch, seenResult, seenText)
	}

	assertRemoteWorkbenchAcceptanceQueries(t, ctx, client)
	history, err := client.History(ctx, 20)
	if err != nil {
		t.Fatal(err)
	}
	assertRemoteWorkbenchAcceptanceHistory(t, history)

	client.Close()
	waitForRemoteWorkbenchSSHExit(t, firstFactory)

	reconnected, secondFactory := newClient(2)
	if err := reconnected.SelectSession(created.Target, created.RuntimeEpoch); err != nil {
		t.Fatal(err)
	}
	resumed, err := reconnected.Subscribe(ctx, 20)
	if err != nil {
		t.Fatal(err)
	}
	if resumed.Snapshot.Target != created.Target || resumed.Snapshot.RuntimeEpoch != created.RuntimeEpoch {
		t.Fatalf("reconnected snapshot authority = %+v/%q, want %+v/%q", resumed.Snapshot.Target, resumed.Snapshot.RuntimeEpoch, created.Target, created.RuntimeEpoch)
	}
	assertRemoteWorkbenchAcceptanceHistory(t, resumed.Snapshot.History)
	reconnected.Close()
	waitForRemoteWorkbenchSSHExit(t, secondFactory)

	t.Logf("physical Remote Workbench acceptance passed for clean head %s", expectedSHA)
}

func requiredRemoteWorkbenchIntegrationEnv(t *testing.T, name string) string {
	t.Helper()
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		t.Fatalf("%s is required when physical integration is enabled", name)
	}
	return value
}

func receiveRemoteWorkbenchAcceptanceRequest(t *testing.T, ctx context.Context, requests <-chan provider.Request) provider.Request {
	t.Helper()
	select {
	case request := <-requests:
		return request
	case <-ctx.Done():
		t.Fatal("timed out waiting for a Desktop Broker request")
		return provider.Request{}
	}
}

func assertRemoteWorkbenchAcceptanceQueries(t *testing.T, ctx context.Context, client *workbenchclient.Client) {
	t.Helper()
	raw, err := client.Request(ctx, string(protocol.MethodFilePreview), protocol.FilePreviewParams{Path: "broker-proof.txt"})
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := protocol.DecodeResult(protocol.MethodFilePreview, raw)
	if err != nil {
		t.Fatal(err)
	}
	preview := decoded.(protocol.FilePreviewResult)
	if preview.Body == nil || !strings.Contains(*preview.Body, remoteWorkbenchIntegrationMarker) {
		t.Fatalf("Remote file preview did not come from the Linux workspace: %+v", preview)
	}

	raw, err = client.Request(ctx, string(protocol.MethodWorkspaceChanges), protocol.WorkspaceChangesParams{})
	if err != nil {
		t.Fatal(err)
	}
	decoded, err = protocol.DecodeResult(protocol.MethodWorkspaceChanges, raw)
	if err != nil {
		t.Fatal(err)
	}
	changes := decoded.(protocol.WorkspaceChangesResult)
	if !changes.GitAvailable {
		t.Fatal("Remote workspace Git status was unavailable")
	}

	raw, err = client.Request(ctx, string(protocol.MethodGitHistory), protocol.GitHistoryParams{})
	if err != nil {
		t.Fatal(err)
	}
	decoded, err = protocol.DecodeResult(protocol.MethodGitHistory, raw)
	if err != nil {
		t.Fatal(err)
	}
	gitHistory := decoded.(protocol.GitHistoryResult)
	if len(gitHistory.Commits) == 0 {
		t.Fatal("Remote Git history was empty")
	}
}

func assertRemoteWorkbenchAcceptanceHistory(t *testing.T, history protocol.HistoryPage) {
	t.Helper()
	var sawReasoningToolCall, sawToolResult, sawFinalText bool
	for _, message := range history.Messages {
		sawReasoningToolCall = sawReasoningToolCall || (message.Role == "assistant" && message.Reasoning != nil &&
			strings.Contains(*message.Reasoning, "inspect the Linux workspace") && len(message.ToolCalls) == 1 && message.ToolCalls[0].Name == "read_file")
		sawToolResult = sawToolResult || (message.Role == "tool" && message.ToolName == "read_file")
		sawFinalText = sawFinalText || (message.Role == "assistant" && message.Content != nil &&
			strings.Contains(*message.Content, "physical Windows to Linux tool loop complete"))
	}
	if !sawReasoningToolCall || !sawToolResult || !sawFinalText {
		t.Fatalf("Remote history incomplete: reasoning/tool=%v result=%v final=%v", sawReasoningToolCall, sawToolResult, sawFinalText)
	}
}

func waitForRemoteWorkbenchSSHExit(t *testing.T, factory *windowsWorkbenchSSHFactory) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		factory.mu.Lock()
		transport := factory.transport
		factory.mu.Unlock()
		if transport != nil && transport.processExited.Load() {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatal("Windows OpenSSH process was not reaped after closing the Remote Workbench client")
}
