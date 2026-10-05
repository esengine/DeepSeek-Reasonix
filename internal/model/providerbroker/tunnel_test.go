package providerbroker

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"

	"reasonix/internal/contract/provider"
	"reasonix/internal/platform/remote/forward"
	"reasonix/internal/platform/remote/sshtest"
)

// tunnelTo publishes local on the test server's loopback the way an attach
// does — one -R forward — and returns the address a kernel over there calls.
func tunnelTo(t *testing.T, addr string) string {
	t.Helper()
	srv := sshtest.Start(t, sshtest.Options{})
	cl, err := ssh.Dial("tcp", srv.Addr, &ssh.ClientConfig{
		User:            "test",
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         5 * time.Second,
	})
	if err != nil {
		t.Fatalf("ssh dial: %v", err)
	}
	t.Cleanup(func() { cl.Close() })

	set := forward.NewSet(nil)
	t.Cleanup(set.Close)
	if err := set.Attach(cl); err != nil {
		t.Fatalf("attach forwards: %v", err)
	}
	bound, err := set.Add(forward.Spec{
		Name:       "provider-broker",
		Direction:  forward.Remote,
		BindAddr:   "127.0.0.1:0",
		TargetAddr: addr,
	})
	if err != nil {
		t.Fatalf("publish the broker: %v", err)
	}
	return "http://" + bound
}

// The design end to end: a kernel on another machine, holding no key and
// reaching no model endpoint, completes a turn over a reverse forward whose
// far end is this machine's providers.
func TestATurnCompletesOverTheReverseForward(t *testing.T) {
	seen := make(chan provider.Request, 1)
	fake := &fakeProvider{
		name: "deepseek",
		seen: seen,
		script: []provider.Chunk{
			{Type: provider.ChunkReasoning, Text: "thinking"},
			{Type: provider.ChunkText, Text: "hello from home"},
			{Type: provider.ChunkUsage, Usage: &provider.Usage{PromptTokens: 11, CacheHitTokens: 4}},
			{Type: provider.ChunkDone},
		},
	}
	local, err := Listen(staticResolver(fake, provider.Descriptor{
		Ref: "deepseek/chat", DisplayName: "deepseek", ContextWindow: 128000,
	}))
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	t.Cleanup(func() { local.Close() })

	// Everything below is what the far side has: an address on its own
	// loopback, and a token. No endpoint, no key, no proxy settings.
	client := NewClient(tunnelTo(t, local.Addr), local.Token, nil)

	catalog := client.Catalog()
	if len(catalog) != 1 || catalog[0].Ref != "deepseek/chat" {
		t.Fatalf("the catalog did not cross the tunnel: %+v", catalog)
	}
	p, err := client.Resolve(provider.Selection{Ref: "deepseek/chat"})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	ch, err := p.Stream(context.Background(), provider.Request{
		Messages: []provider.Message{{Role: provider.RoleUser, Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	got := drain(t, ch)
	if len(got) != 4 {
		t.Fatalf("want 4 chunks over the tunnel, got %d: %+v", len(got), got)
	}
	if got[1].Text != "hello from home" {
		t.Fatalf("the answer did not survive the tunnel: %+v", got[1])
	}
	if got[2].Usage == nil || got[2].Usage.CacheHitTokens != 4 {
		t.Fatalf("usage did not survive the tunnel: %+v", got[2])
	}
	if sent := <-seen; len(sent.Messages) != 1 || sent.Messages[0].Content != "hi" {
		t.Fatalf("the request did not reach the local provider intact: %+v", sent)
	}
}

// The classification the whole design exists to preserve, measured where it
// actually has to hold: through the tunnel, not just through the codec.
func TestContextOverflowSurvivesTheReverseForward(t *testing.T) {
	overflow := &provider.APIError{
		Provider: "deepseek",
		Status:   http.StatusBadRequest,
		Body:     `{"error":{"code":"context_window_exceeded"}}`,
	}
	fake := &fakeProvider{name: "deepseek", script: []provider.Chunk{{Type: provider.ChunkError, Err: overflow}}}
	local, err := Listen(staticResolver(fake, provider.Descriptor{Ref: "deepseek/chat"}))
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	t.Cleanup(func() { local.Close() })

	client := NewClient(tunnelTo(t, local.Addr), local.Token, nil)
	p, err := client.Resolve(provider.Selection{Ref: "deepseek/chat"})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	ch, err := p.Stream(context.Background(), provider.Request{})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	got := drain(t, ch)
	if len(got) != 1 || !provider.IsContextOverflow(got[0].Err) {
		t.Fatalf("overflow lost its identity over the tunnel: %+v", got)
	}
}

// A tunnel that drops mid-answer must read as an interruption, which the agent
// replays — not as a finished turn, which it would commit truncated.
func TestATunnelDroppedMidAnswerReportsAnInterruption(t *testing.T) {
	released := make(chan struct{})
	fake := &fakeProvider{
		name:   "deepseek",
		script: []provider.Chunk{{Type: provider.ChunkText, Text: "half an ans"}},
		block:  released,
	}
	local, err := Listen(staticResolver(fake, provider.Descriptor{Ref: "deepseek/chat"}))
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	t.Cleanup(func() { local.Close() })
	t.Cleanup(func() { close(released) })

	client := NewClient(tunnelTo(t, local.Addr), local.Token, nil)
	p, _ := client.Resolve(provider.Selection{Ref: "deepseek/chat"})
	ch, err := p.Stream(context.Background(), provider.Request{})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	if first := <-ch; first.Text != "half an ans" {
		t.Fatalf("want the partial answer, got %+v", first)
	}
	// The broker goes away under the still-open stream, which is what a lost
	// link looks like from the far side.
	_ = local.Close()

	var last provider.Chunk
	for c := range ch {
		last = c
	}
	var interrupted *provider.StreamInterruptedError
	if !errors.As(last.Err, &interrupted) {
		t.Fatalf("a dropped tunnel reported %#v, not an interruption", last.Err)
	}
}
