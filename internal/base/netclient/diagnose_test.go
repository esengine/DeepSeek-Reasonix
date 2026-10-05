package netclient

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"
)

// The steps exist to name the layer that broke, so the walk stops at the first
// failure: every later step fails for the same reason, and three red rows hide
// which one is the cause. A closed port on loopback is the one unreachable
// target that does not depend on what the machine's DNS does — a resolver with
// fake-ip answers even for .invalid.
func TestDiagnoseStopsAtTheFirstFailure(t *testing.T) {
	srv := httptest.NewServer(nil)
	closed := srv.URL
	srv.Close()

	probes := Diagnose(context.Background(), ProxySpec{Mode: ModeOff}, closed)
	if len(probes) == 0 {
		t.Fatal("no probes returned")
	}
	last := probes[len(probes)-1]
	if last.Step != "connect" || last.OK {
		t.Fatalf("expected to stop on a failed connect step, got %+v", probes)
	}
	if last.Advice == "" {
		t.Error("a failed connect step gave the user nothing to do next")
	}
	for _, p := range probes[:len(probes)-1] {
		if !p.OK {
			t.Errorf("step %s failed before the one being diagnosed: %s", p.Step, p.Detail)
		}
	}
}

// A reachable endpoint has to walk all the way through, or the diagnosis would
// report a problem where there is none.
func TestDiagnoseWalksAReachableEndpoint(t *testing.T) {
	srv := httptest.NewServer(nil)
	defer srv.Close()

	probes := Diagnose(context.Background(), ProxySpec{Mode: ModeOff}, srv.URL)
	steps := make([]string, 0, len(probes))
	for _, p := range probes {
		if !p.OK {
			t.Fatalf("step %s failed against a live server: %s", p.Step, p.Detail)
		}
		steps = append(steps, p.Step)
	}
	// Plain http has nothing to say about TLS, and inventing a step would be a
	// green row that tested nothing.
	if got := strings.Join(steps, ","); got != "proxy,dns,connect" {
		t.Errorf("steps = %s, want proxy,dns,connect", got)
	}
}

// A proxy line ends up in screenshots and bug reports far more often than it
// gets typed.
func TestRedactProxyURLKeepsTheHostReadable(t *testing.T) {
	got := RedactProxyURL("http://alice:hunter2@proxy.corp:8080")
	if strings.Contains(got, "hunter2") {
		t.Fatalf("password survived redaction: %s", got)
	}
	for _, want := range []string{"alice", "proxy.corp:8080"} {
		if !strings.Contains(got, want) {
			t.Errorf("redaction dropped %q, leaving %s unrecognisable", want, got)
		}
	}
	if plain := "socks5://127.0.0.1:7890"; RedactProxyURL(plain) != plain {
		t.Errorf("a proxy with no credentials was rewritten: %s", RedactProxyURL(plain))
	}
}
