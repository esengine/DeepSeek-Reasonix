package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"os"
	"os/user"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestCredentialProxyRealHostSmoke is a manual E2E against a real SSH host:
// set CRED_PROXY_SMOKE_HOST=<hostID> (whose config entry has
// credential_mode = "local-proxy") to run. It drives the full desktop path —
// connect, reverse tunnel, credential proxy, serve launch with the virtual
// token — then submits one real model turn and asserts the reply arrived
// through desktop-held credentials. Costs one tiny provider call.
func TestCredentialProxyRealHostSmoke(t *testing.T) {
	hostID := os.Getenv("CRED_PROXY_SMOKE_HOST")
	if hostID == "" {
		t.Skip("set CRED_PROXY_SMOKE_HOST=<hostID with credential_mode=local-proxy> to run the real-host smoke")
	}
	// The desktop test binary's TestMain redirects HOME to a scratch dir; the
	// smoke needs the real user config (hosts, provider, .env key). Point the
	// config root back at the real ~/.reasonix for this test only.
	if real, err := user.Current(); err == nil && real.HomeDir != "" {
		t.Setenv("HOME", real.HomeDir)
		t.Setenv("REASONIX_HOME", filepath.Join(real.HomeDir, ".reasonix"))
	} else {
		t.Fatal("cannot resolve the real home directory")
	}
	workspace := os.Getenv("CRED_PROXY_SMOKE_WORKSPACE")
	if workspace == "" {
		workspace = "/root/smoke-a"
	}

	a := &App{ctx: context.Background()}
	mgr := newDesktopRemoteManager(a)
	a.remoteRuntime = mgr
	t.Cleanup(func() { _ = mgr.Close(); a.closeCredentialProxy() })

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	if err := mgr.Connect(hostID); err != nil {
		t.Fatalf("connect: %v", err)
	}
	if err := waitForRemoteHostSmoke(mgr, hostID, 60*time.Second); err != nil {
		t.Fatalf("host never connected: %v", err)
	}

	view, token, err := mgr.EnsureServer(ctx, hostID, workspace)
	if err != nil {
		t.Fatalf("EnsureServer: %v", err)
	}
	if view.State != "ready" || view.LocalURL == "" {
		t.Fatalf("serve view = %+v", view)
	}
	base := strings.TrimRight(view.LocalURL, "/")
	t.Logf("serve ready at %s (workspace %s)", base, workspace)

	// Verify the reverse tunnel is registered and the remote provider entry
	// points at it.
	foundForward := false
	for _, f := range mgr.client(hostID).Forwards().List() {
		if f.Spec.Name == "cred-proxy:"+hostID && f.Up {
			foundForward = true
		}
	}
	if !foundForward {
		t.Fatal("credential proxy reverse forward is not up")
	}

	// One real model turn through the desktop-held key.
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar, Timeout: 120 * time.Second}
	if err := serveHandshake(ctx, client, base, token); err != nil {
		t.Fatalf("handshake: %v", err)
	}
	post := func(path string, body any) (int, []byte) {
		data, _ := json.Marshal(body)
		req, _ := http.NewRequest(http.MethodPost, base+path, strings.NewReader(string(data)))
		req.Header.Set("Content-Type", "application/json")
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		defer resp.Body.Close()
		out, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, out
	}
	if code, body := post("/new", map[string]string{}); code != 204 {
		t.Fatalf("/new: %d %s", code, body)
	}
	if code, body := post("/submit", map[string]string{"input": "Reply with exactly the word PROXY-OK and nothing else."}); code != 202 {
		t.Fatalf("/submit: %d %s", code, body)
	}

	// Poll history for the assistant reply (the turn rides the tunnel).
	deadline := time.Now().Add(90 * time.Second)
	for {
		resp, err := client.Get(base + "/history")
		if err == nil {
			out, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			if strings.Contains(string(out), "PROXY-OK") {
				t.Logf("model replied through the desktop credential proxy")
				return
			}
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("/history: %d %s", resp.StatusCode, out)
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("no PROXY-OK reply within 90s (last history fetch err=%v)", err)
		}
		time.Sleep(2 * time.Second)
	}
}

func waitForRemoteHostSmoke(rt remoteKernel, hostID string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		for _, status := range rt.Statuses() {
			if status.HostID != hostID {
				continue
			}
			switch status.State {
			case "connected", "degraded":
				return nil
			case "stopped":
				if status.Error != "" {
					return fmt.Errorf("remote host %q: %s", hostID, status.Error)
				}
				return fmt.Errorf("remote host %q stopped", hostID)
			}
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("remote host %q: connection timed out", hostID)
		}
		time.Sleep(250 * time.Millisecond)
	}
}
