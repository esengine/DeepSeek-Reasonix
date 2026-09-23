package cli

import (
	"context"
	"flag"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"

	"reasonix/internal/provider"
	"reasonix/internal/providerbroker"
)

func brokerFlagsFor(t *testing.T, args ...string) brokerFlags {
	t.Helper()
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	b := registerBrokerFlags(fs)
	if err := fs.Parse(args); err != nil {
		t.Fatalf("parse %v: %v", args, err)
	}
	return b
}

func writeTokenFile(t *testing.T, token string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "broker.token")
	if err := os.WriteFile(path, []byte(token+"\n"), 0o600); err != nil {
		t.Fatalf("write token: %v", err)
	}
	return path
}

// The address arrives from the forward that published it. A non-loopback one
// means something other than the tunnel is carrying the conversation, and the
// whole conversation — the workspace in it — is what these requests send.
func TestBrokerRefusesANonLoopbackAddress(t *testing.T) {
	refused := []string{
		"http://10.0.0.4:8080",
		"http://broker.example.com:8080",
		"https://127.0.0.1:8080",
		"http://0.0.0.0:8080",
	}
	tokenFile := writeTokenFile(t, "t")
	for _, addr := range refused {
		t.Run(addr, func(t *testing.T) {
			b := brokerFlagsFor(t, "--provider-broker", addr, "--provider-broker-token-file", tokenFile)
			if _, err := b.resolver(); err == nil {
				t.Fatalf("%s was accepted as a broker address", addr)
			}
		})
	}
}

func TestBrokerAcceptsLoopback(t *testing.T) {
	tokenFile := writeTokenFile(t, "secret")
	for _, addr := range []string{"http://127.0.0.1:41235", "127.0.0.1:41235", "http://localhost:41235", "http://[::1]:41235"} {
		t.Run(addr, func(t *testing.T) {
			b := brokerFlagsFor(t, "--provider-broker", addr, "--provider-broker-token-file", tokenFile)
			resolver, err := b.resolver()
			if err != nil {
				t.Fatalf("%s was refused: %v", addr, err)
			}
			if resolver == nil {
				t.Fatal("a configured broker produced no resolver")
			}
		})
	}
}

// No broker flag is the ordinary local serve, which must keep reading its own
// config rather than being handed a resolver that resolves nothing.
func TestNoBrokerLeavesTheLocalPath(t *testing.T) {
	b := brokerFlagsFor(t)
	resolver, err := b.resolver()
	if err != nil {
		t.Fatalf("resolver: %v", err)
	}
	if resolver != nil {
		t.Fatalf("an unflagged serve got a broker resolver: %#v", resolver)
	}
}

// An address with no token would be a broker nobody authenticates to.
func TestBrokerRequiresAToken(t *testing.T) {
	b := brokerFlagsFor(t, "--provider-broker", "http://127.0.0.1:41235")
	if _, err := b.resolver(); err == nil {
		t.Fatal("a broker without a token file was accepted")
	}
}

// The token file is read with serve's own rules: a world-readable one is every
// account on the machine holding a key to the user's model spend.
func TestBrokerTokenFileMustBePrivate(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permission bits do not decide access here")
	}
	path := writeTokenFile(t, "secret")
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	b := brokerFlagsFor(t, "--provider-broker", "http://127.0.0.1:41235", "--provider-broker-token-file", path)
	_, err := b.resolver()
	if err == nil {
		t.Fatal("a world-readable token file was accepted")
	}
	if !strings.Contains(err.Error(), "chmod 600") {
		t.Fatalf("the refusal did not name the fix: %v", err)
	}
}

type namedProvider struct{ name string }

func (p namedProvider) Name() string { return p.name }
func (p namedProvider) Stream(context.Context, provider.Request) (<-chan provider.Chunk, error) {
	out := make(chan provider.Chunk, 2)
	out <- provider.Chunk{Type: provider.ChunkText, Text: p.name}
	out <- provider.Chunk{Type: provider.ChunkDone}
	close(out)
	return out, nil
}

func brokerAt(t *testing.T, name, token string) string {
	t.Helper()
	desc := provider.Descriptor{Ref: "p/m"}
	srv, err := providerbroker.NewServer(&provider.StaticResolver{
		Descriptors: []provider.Descriptor{desc},
		Providers:   map[string]provider.Provider{desc.Ref: namedProvider{name}},
	}, token)
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	return ts.URL
}

func writePrivate(t *testing.T, path, line string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(line+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
}

// Another window connecting publishes the broker on a new port and token. The
// serve reads the one file per request, so the conversation already running
// reaches the new broker instead of being restarted to find it.
func TestBrokerFileFollowsARepublishedBroker(t *testing.T) {
	file := filepath.Join(t.TempDir(), "broker.endpoint")
	first, second := brokerAt(t, "first", "one"), brokerAt(t, "second", "two")
	writePrivate(t, file, first+"\none")

	resolver, err := brokerFlagsFor(t, "--provider-broker-file", file).resolver()
	if err != nil {
		t.Fatal(err)
	}
	p, err := resolver.Resolve(provider.Selection{Ref: "p/m"})
	if err != nil {
		t.Fatal(err)
	}
	say := func() (string, error) {
		ch, err := p.Stream(context.Background(), provider.Request{})
		if err != nil {
			return "", err
		}
		return (<-ch).Text, nil
	}
	if got, err := say(); err != nil || got != "first" {
		t.Fatalf("before = %q, %v", got, err)
	}
	writePrivate(t, file, second+"\ntwo")
	if got, err := say(); err != nil || got != "second" {
		t.Fatalf("after the rewrite = %q, %v; want the republished broker", got, err)
	}

	writePrivate(t, file, "http://10.0.0.4:8080\ntwo")
	if _, err := say(); err == nil {
		t.Fatal("a rewrite to a non-loopback address was followed")
	}
	writePrivate(t, file, second)
	if _, err := say(); err == nil {
		t.Fatal("a file with no token line was accepted")
	}
}

func TestBrokerNamedTwiceIsRefused(t *testing.T) {
	dir := t.TempDir()
	file, tokenFile := filepath.Join(dir, "broker.endpoint"), writeTokenFile(t, "t")
	writePrivate(t, file, "http://127.0.0.1:1\nt")
	b := brokerFlagsFor(t, "--provider-broker", "http://127.0.0.1:1", "--provider-broker-file", file, "--provider-broker-token-file", tokenFile)
	if _, err := b.resolver(); err == nil {
		t.Fatal("two broker addresses were accepted")
	}
}

// A broker answering with a redirect would send the token and the whole
// conversation to wherever it points; the serve does not follow it.
func TestBrokerDoesNotFollowARedirect(t *testing.T) {
	var reached atomic.Int32
	elsewhere := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { reached.Add(1) }))
	defer elsewhere.Close()
	redirecting := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, elsewhere.URL+r.URL.Path, http.StatusTemporaryRedirect)
	}))
	defer redirecting.Close()
	file := filepath.Join(t.TempDir(), "broker.endpoint")
	writePrivate(t, file, redirecting.URL+"\nt")

	resolver, err := brokerFlagsFor(t, "--provider-broker-file", file).resolver()
	if err != nil {
		t.Fatal(err)
	}
	p, err := resolver.Resolve(provider.Selection{Ref: "p/m"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.Stream(context.Background(), provider.Request{}); err == nil {
		t.Fatal("a redirected stream was accepted")
	}
	if n := reached.Load(); n != 0 {
		t.Fatalf("the redirect target was reached %d times", n)
	}
}
