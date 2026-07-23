//go:build !windows && live && reasonix_remote_integration

package main

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"reasonix/internal/config"
	"reasonix/internal/provider"
	remotebroker "reasonix/internal/remote/broker"
	"reasonix/internal/remote/protocol"
	"reasonix/internal/remote/workbench/attach"
	workbenchclient "reasonix/internal/remote/workbench/client"
	"reasonix/internal/remote/workbench/transport"
)

const (
	remoteWorkbenchLiveGateEnv        = "REASONIX_REMOTE_WORKBENCH_LIVE"
	remoteWorkbenchLiveProviderRefEnv = "REASONIX_REMOTE_WORKBENCH_LIVE_PROVIDER_REF"
	remoteWorkbenchLiveMarker         = "REMOTE_BROKER_LIVE_OK"
)

// TestRemoteWorkbenchLiveDesktopBroker is an opt-in release acceptance test.
// It loads the Desktop Provider before isolating the Host home and environment,
// then drives a real model turn through Desktop Broker -> Host runtime. The
// physical Windows-to-Linux acceptance test separately covers the SSH boundary.
func TestRemoteWorkbenchLiveDesktopBroker(t *testing.T) {
	if os.Getenv(remoteWorkbenchLiveGateEnv) != "1" {
		t.Skip("set REASONIX_REMOTE_WORKBENCH_LIVE=1 to run the live Desktop Broker acceptance test")
	}

	// Load the Desktop-owned Provider and credential before replacing every
	// Host-visible config location below. Never log cfg: it contains secrets.
	desktopConfig, err := config.Load()
	if err != nil {
		t.Fatal("load Desktop Provider configuration")
	}
	providerRef, effort, ok := liveRemoteWorkbenchProvider(desktopConfig, os.Getenv(remoteWorkbenchLiveProviderRefEnv))
	if !ok {
		t.Skip("no configured DeepSeek chat model is available on Desktop")
	}
	providerEntry, ok := desktopConfig.ResolveModel(providerRef)
	if !ok {
		t.Fatal("selected live Provider model no longer resolves")
	}

	// Keep the Unix socket path below the macOS sockaddr_un length limit.
	hostHome, err := os.MkdirTemp("/tmp", "rx-live-host-")
	if err != nil {
		t.Fatal(err)
	}
	workspace, err := os.MkdirTemp("/tmp", "rx-live-work-")
	if err != nil {
		_ = os.RemoveAll(hostHome)
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = os.RemoveAll(workspace)
		_ = os.RemoveAll(hostHome)
	})
	if err := os.WriteFile(filepath.Join(workspace, "live-broker-proof.txt"), []byte(remoteWorkbenchLiveMarker+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", hostHome)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(hostHome, ".config"))
	t.Setenv("REASONIX_HOME", filepath.Join(hostHome, ".reasonix"))
	t.Setenv("REASONIX_STATE_HOME", filepath.Join(hostHome, ".state"))
	t.Setenv("DEEPSEEK_API_KEY", "")
	t.Setenv("OPENAI_API_KEY", "")
	if keyEnv := strings.TrimSpace(providerEntry.APIKeyEnv); keyEnv != "" {
		t.Setenv(keyEnv, "")
	}
	t.Setenv("REASONIX_SAFE_MODE", "1")
	assertRemoteWorkbenchLiveHostCredentialFree(t, hostHome, providerEntry.APIKeyEnv)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	attachErr := make(chan error, 1)
	factory := transport.FactoryFunc(func(openCtx context.Context) (transport.Stream, error) {
		desktopSide, hostSide := net.Pipe()
		go func() {
			attachErr <- attach.Run(openCtx, hostSide, hostSide, attach.Options{
				Workspace: workspace,
				Home:      hostHome,
				Version:   version,
				InProcess: true,
			})
		}()
		return desktopSide, nil
	})

	allowed := map[string]struct{}{providerRef: {}}
	brokerOpts := remotebroker.Options{
		Catalog: func(_ context.Context, filter map[string]struct{}) ([]protocol.BrokerProviderDescriptor, error) {
			return catalogDescriptors(desktopConfig, allowed, filter)
		},
		Open: func(streamCtx context.Context, ref, requestedEffort string, request provider.Request) (<-chan provider.Chunk, error) {
			if ref != providerRef {
				return nil, fmt.Errorf("unexpected live Provider ref")
			}
			return openLocalProviderStream(streamCtx, desktopConfig, ref, requestedEffort, request)
		},
	}
	buildID := protocol.CurrentBuildID(version)
	events := make(chan protocol.SessionEvent, 128)
	client, err := workbenchclient.Connect(ctx, factory, 1, brokerOpts, map[string]any{
		"productVersion": buildID.ProductVersion, "sourceRevision": buildID.SourceRevision,
		"protocolVersion": buildID.ProtocolVersion, "schemaHash": buildID.SchemaHash,
	}, workspace, workbenchclient.Callbacks{OnSessionEvent: func(event protocol.SessionEvent) { events <- event }})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	if _, err := client.CreateSession(ctx, providerRef, effort); err != nil {
		t.Fatal(err)
	}
	if _, err := client.Subscribe(ctx, 20); err != nil {
		t.Fatal(err)
	}
	if _, err := client.Submit(ctx, "Use the read_file tool to read live-broker-proof.txt, then reply with only the token stored in that file. You must use the tool; do not guess."); err != nil {
		t.Fatal(err)
	}

	var response strings.Builder
	var sawToolDispatch, sawToolResult bool
	for {
		select {
		case event := <-events:
			if event.Event.Kind == "text" {
				response.WriteString(event.Event.Text)
			}
			if event.Event.Kind == "tool_dispatch" && event.Event.Tool.Name == "read_file" {
				sawToolDispatch = true
			}
			if event.Event.Kind == "tool_result" && event.Event.Tool.Name == "read_file" && strings.Contains(event.Event.Tool.Output, remoteWorkbenchLiveMarker) {
				sawToolResult = true
			}
			if event.Event.Kind != "turn_done" {
				continue
			}
			if !sawToolDispatch || !sawToolResult || !strings.Contains(response.String(), remoteWorkbenchLiveMarker) {
				t.Fatalf("live Remote Broker tool loop incomplete: dispatch=%v result=%v responseMarker=%v", sawToolDispatch, sawToolResult, strings.Contains(response.String(), remoteWorkbenchLiveMarker))
			}
			assertRemoteWorkbenchLiveHostCredentialFree(t, hostHome, providerEntry.APIKeyEnv)
			t.Log("live DeepSeek tool-call loop completed through Desktop Broker with an isolated credential-free Host")
			return
		case err := <-attachErr:
			if err != nil && ctx.Err() == nil {
				t.Fatalf("Host attach exited before the live turn completed: %v", err)
			}
		case <-ctx.Done():
			t.Fatal("live Remote Broker turn timed out")
		}
	}
}

func assertRemoteWorkbenchLiveHostCredentialFree(t *testing.T, hostHome, providerKeyEnv string) {
	t.Helper()
	if _, err := os.Stat(filepath.Join(hostHome, ".reasonix", ".env")); !os.IsNotExist(err) {
		t.Fatal("isolated Host unexpectedly has a Reasonix credential file")
	}
	if keyEnv := strings.TrimSpace(providerKeyEnv); keyEnv != "" && os.Getenv(keyEnv) != "" {
		t.Fatal("isolated Host unexpectedly has a Provider credential environment value")
	}
}

func liveRemoteWorkbenchProvider(cfg *config.Config, requested string) (ref, effort string, ok bool) {
	requested = strings.TrimSpace(requested)
	for _, candidate := range localProviderRefs(cfg) {
		if requested != "" && candidate != requested {
			continue
		}
		if requested == "" && !strings.Contains(strings.ToLower(candidate), "deepseek") {
			continue
		}
		entry, found := cfg.ResolveModel(candidate)
		if !found || !entry.Configured() {
			continue
		}
		return candidate, config.EffectiveEffort(entry), true
	}
	return "", "", false
}
