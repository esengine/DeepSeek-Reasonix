//go:build !windows

package sandbox

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// authorityFixture is a stand-in host service plus an external listener. The
// socket lives under the bound write root because bubblewrap swaps /tmp for a
// fresh tmpfs: an unbound socket would be absent rather than masked, and every
// deny cell would pass without denying.
type authorityFixture struct {
	bound, sock, url string
}

func newAuthorityFixture(t *testing.T) authorityFixture {
	t.Helper()
	if !Available() {
		t.Skip("no usable OS sandbox backend")
	}
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 not installed")
	}
	// Not t.TempDir(): its name carries the test's, and a Unix socket path is
	// capped near 104 bytes, so the listen would fail with "invalid argument".
	bound, err := os.MkdirTemp("", "rxa")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(bound) })
	if real, err := filepath.EvalSymlinks(bound); err == nil {
		bound = real
	}
	sock := filepath.Join(bound, "a.sock")
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			c.Write([]byte("AGENT-OK"))
			c.Close()
		}
	}()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("NET-OK"))
	}))
	t.Cleanup(srv.Close)
	return authorityFixture{bound: bound, sock: sock, url: srv.URL}
}

// probe reports what the confined command could actually reach.
func (f authorityFixture) probe(t *testing.T, spec Spec) (agent, network bool) {
	t.Helper()
	script := fmt.Sprintf(`import socket,urllib.request
try:
    s=socket.socket(socket.AF_UNIX); s.connect(%q); print(s.recv(16).decode())
except Exception as e: print("agent unreachable:", e)
try: print(urllib.request.urlopen(%q, timeout=5).read().decode())
except Exception as e: print("network unreachable:", e)`, f.sock, f.url)

	// Raw argv, not a shell command: quoting the script through a shell once
	// truncated it at an inner quote, so only the branches that ran in the deny
	// cells failed and the matrix read as if a socket deny cut all egress.
	argv, wrapped := CommandArgs(spec, []string{"python3", "-c", script})
	if !wrapped {
		t.Fatal("spec did not wrap; the matrix would measure nothing")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	out, _ := exec.CommandContext(ctx, argv[0], argv[1:]...).CombinedOutput()
	got := string(out)
	return strings.Contains(got, "AGENT-OK"), strings.Contains(got, "NET-OK")
}

func (f authorityFixture) spec(network bool, granted ...HostAuthority) Spec {
	return Spec{Mode: "enforce", WriteRoots: []string{f.bound}, Network: network, HostAuthorities: granted}
}

// TestExternalNetworkAndHostAuthorityAreSeparate is the acceptance matrix for
// the split. The third row decides it: revoking external egress must not revoke
// local signing authority, which one flag covering both could never express.
func TestExternalNetworkAndHostAuthorityAreSeparate(t *testing.T) {
	f := newAuthorityFixture(t)
	t.Setenv("SSH_AUTH_SOCK", f.sock)

	for _, tc := range []struct {
		name                       string
		network                    bool
		grant                      []HostAuthority
		wantAgent, wantExternalNet bool
	}{
		{"network allow, agent allow", true, []HostAuthority{SSHAgent}, true, true},
		{"network allow, agent deny", true, nil, false, true},
		{"network deny, agent allow", false, []HostAuthority{SSHAgent}, true, false},
		{"network deny, agent deny", false, nil, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			agent, network := f.probe(t, f.spec(tc.network, tc.grant...))
			if agent != tc.wantAgent {
				t.Errorf("ssh-agent reachable = %v, want %v", agent, tc.wantAgent)
			}
			if network != tc.wantExternalNet {
				t.Errorf("external network reachable = %v, want %v", network, tc.wantExternalNet)
			}
		})
	}
}

// TestContainerDaemonDeniedWhileNetworkAllowed is the other direction: a
// container daemon socket is host-wide execution, so it stays denied under the
// egress every ordinary build needs.
func TestContainerDaemonDeniedWhileNetworkAllowed(t *testing.T) {
	f := newAuthorityFixture(t)
	t.Setenv("DOCKER_HOST", "unix://"+f.sock)

	agent, network := f.probe(t, f.spec(true, SSHAgent))
	if agent {
		t.Error("docker socket reachable while only ssh_agent was granted")
	}
	if !network {
		t.Error("denying the daemon socket also cut ordinary egress")
	}
}

// TestGrantedDaemonIsReachable keeps the deny above honest: the same endpoint
// must come back when it is granted, or the test would pass on any breakage.
func TestGrantedDaemonIsReachable(t *testing.T) {
	f := newAuthorityFixture(t)
	t.Setenv("DOCKER_HOST", "unix://"+f.sock)

	if agent, _ := f.probe(t, f.spec(true, Docker)); !agent {
		t.Error("granting docker did not restore the socket")
	}
}

func TestAuthorityEndpointsFollowTheClientsResolution(t *testing.T) {
	t.Setenv("DOCKER_HOST", "tcp://10.0.0.1:2375")
	for _, p := range authorityEndpoints(Docker) {
		if strings.Contains(p, "10.0.0.1") {
			t.Error("a tcp:// daemon was treated as a socket to mask; it belongs to the network axis")
		}
	}
	t.Setenv("SSH_AUTH_SOCK", filepath.Join(t.TempDir(), "missing.sock"))
	if got := existingSockets(authorityEndpoints(SSHAgent)); len(got) != 0 {
		t.Errorf("a missing endpoint was kept (%v); masking it would fail the sandbox closed", got)
	}
}

// TestAuthorityEndpointsCoverEveryGovernedClient pins the two answers the
// resolution has beyond the defaults: podman's per-user socket, which lives
// wherever XDG_RUNTIME_DIR points, and an authority this table does not govern,
// which resolves to nothing rather than to a guess.
func TestAuthorityEndpointsCoverEveryGovernedClient(t *testing.T) {
	t.Setenv("CONTAINER_HOST", "")
	t.Setenv("XDG_RUNTIME_DIR", "/run/user/4242")
	got := authorityEndpoints(Podman)
	want := filepath.Join("/run/user/4242", "podman", "podman.sock")
	if !slices.Contains(got, want) {
		t.Fatalf("podman endpoints = %v, want the per-user socket %s", got, want)
	}

	// An authority nothing governs has no endpoints. Returning a default here
	// would mask a path on the strength of a name this table never resolved.
	if got := authorityEndpoints(HostAuthority("nothing-governs-this")); got != nil {
		t.Fatalf("ungoverned authority resolved to %v, want nothing", got)
	}
}
