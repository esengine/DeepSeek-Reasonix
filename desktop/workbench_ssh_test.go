package main

import (
	"context"
	"io"
	"os/exec"
	"reflect"
	"testing"
	"time"

	"reasonix/internal/config"
	"reasonix/internal/provider"
	"reasonix/internal/remote/workbench/transport"
)

func TestMapConfigHostKeepsEverySSHConfigAliasInConfigMode(t *testing.T) {
	for _, entry := range []config.RemoteHostEntry{
		{Name: "dotted", Host: "gpu.corp.example", UseSSHConfig: true},
		{Name: "explicit-user", Host: "gpu-alias", User: "builder", UseSSHConfig: true},
	} {
		got, err := mapConfigHostToWorkbenchEntry(entry)
		if err != nil {
			t.Fatal(err)
		}
		if got.Mode != RemoteHostConnectionConfig || got.Alias != entry.Host {
			t.Fatalf("entry %+v mapped to %+v", entry, got)
		}
	}
}

func TestWindowsWorkbenchDirectPreservesIdentityAndProxyJump(t *testing.T) {
	t.Setenv("GO_WANT_REMOTE_SSH_FAKE", "1")
	t.Setenv("REMOTE_SSH_FAKE_MODE", "protocol")
	entry := config.RemoteHostEntry{
		Name: "direct", Host: "[2001:db8::20]", Port: 2200, User: "developer",
		IdentityFile: `C:\keys\reasonix key`, ProxyJump: "bastion-a,bastion-b",
	}
	bound, err := mapConfigHostToWorkbenchEntry(entry)
	if err != nil {
		t.Fatal(err)
	}
	var gotArgs []string
	sshFactory := &RemoteSSHTransportFactory{commandContext: func(ctx context.Context, _ string, args ...string) *exec.Cmd {
		gotArgs = append([]string(nil), args...)
		return remoteSSHFakeCommand(ctx)
	}}
	stream, err := openWindowsWorkbenchSSH(context.Background(), sshFactory, entry, bound)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = stream.Close() })

	wantTail := []string{
		"-l", "developer", "-p", "2200", "-i", entry.IdentityFile,
		"-J", entry.ProxyJump, "--", "2001:db8::20",
		"reasonix", "remote", "attach-workspace", "--stdio",
	}
	if len(gotArgs) < len(wantTail) || !reflect.DeepEqual(gotArgs[len(gotArgs)-len(wantTail):], wantTail) {
		t.Fatalf("direct Workbench argv = %#v, want tail %#v", gotArgs, wantTail)
	}
}

func TestAskPassOwnedStreamClosesBrokerWithTransport(t *testing.T) {
	broker, err := StartRemoteAskPassBroker(context.Background(), time.Minute, func(context.Context, RemoteAskPassPrompt) (RemoteAskPassAnswer, error) {
		return RemoteAskPassAnswer{}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	stream := &closeTrackingStream{}
	owned := &askPassOwnedStream{Stream: stream, broker: broker}
	if err := owned.Close(); err != nil {
		t.Fatal(err)
	}
	if !stream.closed {
		t.Fatal("transport was not closed")
	}
	if _, err := broker.SSHEnvironment("/absolute/helper"); err == nil {
		t.Fatal("AskPass capability remained live after transport close")
	}
	if err := owned.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestAuthorizeWorkbenchPeerFailsClosed(t *testing.T) {
	factory := &peerIdentityFactory{}
	if err := authorizeWorkbenchPeer(factory, "SHA256:trusted"); err == nil {
		t.Fatal("missing live peer identity was authorized")
	}
	factory.peer = workbenchPeerIdentity{KeyType: "ssh-ed25519", Fingerprint: "SHA256:other"}
	if err := authorizeWorkbenchPeer(factory, "SHA256:trusted"); err == nil {
		t.Fatal("changed peer identity was authorized")
	}
	factory.peer.Fingerprint = "SHA256:trusted"
	if err := authorizeWorkbenchPeer(factory, "SHA256:trusted"); err != nil {
		t.Fatalf("trusted live peer was rejected: %v", err)
	}
	if err := authorizeWorkbenchPeer(transport.FactoryFunc(func(context.Context) (transport.Stream, error) {
		return nil, nil
	}), "SHA256:trusted"); err == nil {
		t.Fatal("transport without peer identity reporting was authorized")
	}
}

func TestWorkbenchProviderCatalogUsesReplacedAccessAndCurrentMetadata(t *testing.T) {
	access := newWorkbenchProviderAccess(map[string]struct{}{"first/model-a": {}})
	first := &config.Config{
		Desktop: config.DesktopConfig{ProviderAccess: []string{"first"}},
		Providers: []config.ProviderEntry{{
			Name: "first", Kind: "openai", BaseURL: "http://127.0.0.1:11434/v1", Models: []string{"model-a"},
			ContextWindow: 64_000, Price: &provider.Pricing{Input: 1, Output: 2, Currency: "$"},
		}},
	}
	catalog, err := catalogDescriptors(first, access.snapshot(), nil)
	if err != nil || len(catalog) != 1 || catalog[0].Ref != "first/model-a" || catalog[0].ContextWindow != 64_000 {
		t.Fatalf("first catalog = %+v err=%v", catalog, err)
	}

	access.replace(map[string]struct{}{"second/model-b": {}})
	second := &config.Config{
		Desktop: config.DesktopConfig{ProviderAccess: []string{"second"}},
		Providers: []config.ProviderEntry{{
			Name: "second", Kind: "openai", BaseURL: "http://127.0.0.1:11434/v1", Models: []string{"model-b"},
			ContextWindow: 1_000_000, Price: &provider.Pricing{CacheHit: 0.1, Input: 1.25, Output: 4.5, Currency: "USD"},
		}},
	}
	catalog, err = catalogDescriptors(second, access.snapshot(), nil)
	if err != nil || len(catalog) != 1 || catalog[0].Ref != "second/model-b" {
		t.Fatalf("replaced catalog = %+v err=%v", catalog, err)
	}
	got := catalog[0]
	if got.ContextWindow != 1_000_000 || got.PricingCurrency != "USD" || got.CacheHitPerMillion != 0.1 || got.InputPerMillion != 1.25 || got.OutputPerMillion != 4.5 {
		t.Fatalf("replaced catalog metadata = %+v", got)
	}

	access.replace(map[string]struct{}{})
	catalog, err = catalogDescriptors(second, access.snapshot(), nil)
	if err != nil || len(catalog) != 0 {
		t.Fatalf("revoked catalog = %+v err=%v, want no providers", catalog, err)
	}
}

type peerIdentityFactory struct {
	peer workbenchPeerIdentity
}

func (*peerIdentityFactory) Open(context.Context) (transport.Stream, error) { return nil, nil }

func (f *peerIdentityFactory) PeerIdentity() (workbenchPeerIdentity, bool) {
	return f.peer, f.peer.Fingerprint != ""
}

type closeTrackingStream struct{ closed bool }

func (*closeTrackingStream) Read([]byte) (int, error)    { return 0, io.EOF }
func (*closeTrackingStream) Write(p []byte) (int, error) { return len(p), nil }
func (s *closeTrackingStream) Close() error              { s.closed = true; return nil }

var _ transport.Stream = (*closeTrackingStream)(nil)
