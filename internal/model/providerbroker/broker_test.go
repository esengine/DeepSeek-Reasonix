package providerbroker

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"reasonix/internal/contract/provider"
)

const testToken = "broker-token"

// fakeProvider answers with a fixed script, and records what it was asked.
type fakeProvider struct {
	name   string
	script []provider.Chunk
	fail   error
	seen   chan provider.Request
	block  chan struct{}
}

func (f *fakeProvider) Name() string { return f.name }

func (f *fakeProvider) Stream(ctx context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	if f.seen != nil {
		f.seen <- req
	}
	if f.fail != nil {
		return nil, f.fail
	}
	out := make(chan provider.Chunk)
	go func() {
		defer close(out)
		for _, c := range f.script {
			select {
			case out <- c:
			case <-ctx.Done():
				return
			}
		}
		if f.block != nil {
			select {
			case <-f.block:
			case <-ctx.Done():
			}
		}
	}()
	return out, nil
}

func newPair(t *testing.T, resolver provider.Resolver) *Client {
	t.Helper()
	srv, err := NewServer(resolver, testToken)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	return NewClient(ts.URL, testToken, ts.Client())
}

func staticResolver(p provider.Provider, descriptors ...provider.Descriptor) *provider.StaticResolver {
	providers := map[string]provider.Provider{}
	for _, d := range descriptors {
		providers[d.Ref] = p
	}
	return &provider.StaticResolver{Descriptors: descriptors, Providers: providers}
}

func drain(t *testing.T, ch <-chan provider.Chunk) []provider.Chunk {
	t.Helper()
	var got []provider.Chunk
	for c := range ch {
		got = append(got, c)
	}
	return got
}

// The reason the provider runs at home and only its answer crosses: a rejection
// for an oversized input is classified by *provider.APIError's status and the
// code inside its body, and the agent compacts and replays on that alone. An
// error flattened to a string classifies as nothing, and the remote turn would
// fail where a local one recovers.
func TestContextOverflowSurvivesTheWire(t *testing.T) {
	overflow := &provider.APIError{
		Provider: "deepseek",
		Status:   http.StatusBadRequest,
		Body:     `{"error":{"code":"context_length_exceeded","message":"too long"}}`,
	}
	if !provider.IsContextOverflow(overflow) {
		t.Fatal("the fixture is not an overflow error to begin with")
	}
	fake := &fakeProvider{name: "deepseek", script: []provider.Chunk{{Type: provider.ChunkError, Err: overflow}}}
	client := newPair(t, staticResolver(fake, provider.Descriptor{Ref: "deepseek/chat"}))

	p, err := client.Resolve(provider.Selection{Ref: "deepseek/chat"})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	ch, err := p.Stream(context.Background(), provider.Request{})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	got := drain(t, ch)
	if len(got) != 1 || got[0].Type != provider.ChunkError {
		t.Fatalf("want one error chunk, got %+v", got)
	}
	if !provider.IsContextOverflow(got[0].Err) {
		t.Fatalf("overflow lost its identity crossing the broker: %#v", got[0].Err)
	}
}

// Every error a caller tells apart by type has to arrive as that type.
func TestTypedErrorsRoundTrip(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want func(error) bool
	}{
		{"api", &provider.APIError{Provider: "p", Status: 500, Body: "boom", TraceID: "t-1"},
			func(e error) bool { var got *provider.APIError; return errors.As(e, &got) && got.TraceID == "t-1" }},
		{"auth", &provider.AuthError{Provider: "p", Status: 401, KeyEnv: "K", HasKey: true},
			func(e error) bool {
				var got *provider.AuthError
				return errors.As(e, &got) && got.KeyEnv == "K" && got.HasKey
			}},
		{"payload", &provider.StreamPayloadError{Provider: "p", Message: "upstream said no"},
			func(e error) bool {
				var got *provider.StreamPayloadError
				return errors.As(e, &got) && got.Message == "upstream said no"
			}},
		{"interrupted", provider.StreamInterrupt(errors.New("reset"), provider.StreamInterruptConnectionReset),
			func(e error) bool {
				var got *provider.StreamInterruptedError
				return errors.As(e, &got) && got.Reason == provider.StreamInterruptConnectionReset
			}},
		{"canceled", context.Canceled, func(e error) bool { return errors.Is(e, context.Canceled) }},
		{"deadline", context.DeadlineExceeded, func(e error) bool { return errors.Is(e, context.DeadlineExceeded) }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := encodeError(tc.err).decode(); !tc.want(got) {
				t.Fatalf("%s did not survive the wire: %#v", tc.name, got)
			}
		})
	}
}

// An interrupted stream wraps a cause the caller may also match on, so the
// wrapper must not swallow it.
func TestInterruptedErrorKeepsItsCause(t *testing.T) {
	cause := &provider.APIError{Provider: "p", Status: 502, Body: "gateway"}
	wrapped := provider.StreamInterrupt(cause, provider.StreamInterruptUpstreamError)
	got := encodeError(wrapped).decode()

	var interrupted *provider.StreamInterruptedError
	if !errors.As(got, &interrupted) || interrupted.Reason != provider.StreamInterruptUpstreamError {
		t.Fatalf("lost the wrapper: %#v", got)
	}
	var apiErr *provider.APIError
	if !errors.As(got, &apiErr) || apiErr.Status != 502 {
		t.Fatalf("lost the cause: %#v", got)
	}
}

func TestCatalogAndStreamRoundTrip(t *testing.T) {
	want := provider.Descriptor{
		Ref: "deepseek/chat", DisplayName: "deepseek", Model: "chat",
		ContextWindow: 128000, Reasoning: true, Efforts: []string{"high"},
	}
	seen := make(chan provider.Request, 1)
	fake := &fakeProvider{
		name: "deepseek",
		seen: seen,
		script: []provider.Chunk{
			{Type: provider.ChunkReasoning, Text: "thinking"},
			{Type: provider.ChunkText, Text: "hello"},
			{Type: provider.ChunkToolCall, ToolCall: &provider.ToolCall{ID: "1", Name: "read", Arguments: "{}"}},
			{Type: provider.ChunkUsage, Usage: &provider.Usage{PromptTokens: 7, CacheHitTokens: 3}},
			{Type: provider.ChunkDone},
		},
	}
	client := newPair(t, staticResolver(fake, want))

	catalog := client.Catalog()
	if len(catalog) != 1 || catalog[0].Ref != want.Ref || catalog[0].ContextWindow != want.ContextWindow {
		t.Fatalf("catalog did not cross intact: %+v", catalog)
	}

	p, err := client.Resolve(provider.Selection{Ref: want.Ref})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if p.Name() != "deepseek" {
		t.Fatalf("Name() = %q", p.Name())
	}
	ch, err := p.Stream(context.Background(), provider.Request{
		Messages:  []provider.Message{{Role: provider.RoleUser, Content: "hi"}},
		MaxTokens: 42,
	})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	got := drain(t, ch)
	if len(got) != 5 {
		t.Fatalf("want 5 chunks, got %d: %+v", len(got), got)
	}
	if got[1].Text != "hello" || got[2].ToolCall == nil || got[2].ToolCall.Name != "read" {
		t.Fatalf("chunk payloads did not survive: %+v", got)
	}
	if got[3].Usage == nil || got[3].Usage.CacheHitTokens != 3 {
		t.Fatalf("usage did not survive: %+v", got[3])
	}
	sent := <-seen
	if len(sent.Messages) != 1 || sent.Messages[0].Content != "hi" || sent.MaxTokens != 42 {
		t.Fatalf("the request did not cross intact: %+v", sent)
	}
}

// An unauthenticated broker on the remote's loopback is a model-spend
// capability for every account on that machine.
func TestTokenGate(t *testing.T) {
	fake := &fakeProvider{name: "p", script: []provider.Chunk{{Type: provider.ChunkDone}}}
	srv, err := NewServer(staticResolver(fake, provider.Descriptor{Ref: "p/m"}), testToken)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)

	wrong := NewClient(ts.URL, "not-the-token", ts.Client())
	if got := wrong.Catalog(); len(got) != 0 {
		t.Fatalf("a rejected token still read the catalog: %+v", got)
	}
	p, err := wrong.Resolve(provider.Selection{Ref: "p/m"})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if _, err := p.Stream(context.Background(), provider.Request{}); err == nil {
		t.Fatal("a rejected token still opened a stream")
	}
	if _, err := NewServer(staticResolver(fake), ""); !errors.Is(err, ErrNoToken) {
		t.Fatalf("an empty token built a server: %v", err)
	}
}

// A body that stops mid-answer is an interrupted stream, not a finished one:
// the agent replays an interruption and commits a completion, so reporting the
// wrong one either truncates the turn or repeats it.
func TestTruncatedStreamReportsAnInterruption(t *testing.T) {
	fake := &fakeProvider{name: "p", script: []provider.Chunk{{Type: provider.ChunkText, Text: "half"}}}
	client := newPair(t, staticResolver(fake, provider.Descriptor{Ref: "p/m"}))

	p, _ := client.Resolve(provider.Selection{Ref: "p/m"})
	ch, err := p.Stream(context.Background(), provider.Request{})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	got := drain(t, ch)
	if len(got) != 2 || got[0].Text != "half" {
		t.Fatalf("want the partial answer then a failure, got %+v", got)
	}
	var interrupted *provider.StreamInterruptedError
	if !errors.As(got[1].Err, &interrupted) {
		t.Fatalf("a truncated stream reported %#v, not an interruption", got[1].Err)
	}
}

// Stream's error return is what a caller gets when the completion never began,
// and it has to keep the identity a local provider would have returned.
func TestStreamThatNeverStartedKeepsItsType(t *testing.T) {
	fail := &provider.AuthError{Provider: "p", Status: 401, KeyEnv: "DEEPSEEK_API_KEY"}
	fake := &fakeProvider{name: "p", fail: fail}
	client := newPair(t, staticResolver(fake, provider.Descriptor{Ref: "p/m"}))

	p, _ := client.Resolve(provider.Selection{Ref: "p/m"})
	_, err := p.Stream(context.Background(), provider.Request{})
	var authErr *provider.AuthError
	if !errors.As(err, &authErr) || authErr.KeyEnv != "DEEPSEEK_API_KEY" {
		t.Fatalf("a pre-stream failure lost its type: %#v", err)
	}
}

func TestUnknownRefFailsAtTheStream(t *testing.T) {
	fake := &fakeProvider{name: "p", script: []provider.Chunk{{Type: provider.ChunkDone}}}
	client := newPair(t, staticResolver(fake, provider.Descriptor{Ref: "p/m"}))

	p, err := client.Resolve(provider.Selection{Ref: "nobody/here"})
	if err != nil {
		t.Fatalf("Resolve should bind without calling: %v", err)
	}
	if _, err := p.Stream(context.Background(), provider.Request{}); err == nil {
		t.Fatal("an unknown ref streamed anyway")
	}
	if _, err := client.Resolve(provider.Selection{Ref: "  "}); err == nil {
		t.Fatal("an empty ref resolved")
	}
}

// unknownModelResolver refuses every ref the way a config-backed resolver does.
type unknownModelResolver struct{}

func (unknownModelResolver) Catalog() []provider.Descriptor { return nil }

func (unknownModelResolver) Resolve(sel provider.Selection) (provider.Provider, error) {
	return nil, fmt.Errorf("%w %q", provider.ErrUnknownModel, sel.Ref)
}

// The host reporting a missing model keeps a config of its own, so the refusal
// has to arrive as the identity it is and say which machine had no such model —
// a bare "unknown model" there reads as that host's config being wrong.
func TestUnknownModelCrossesWithItsIdentityAndRef(t *testing.T) {
	client := newPair(t, unknownModelResolver{})
	p, err := client.Resolve(provider.Selection{Ref: "paratera/DeepSeek-V4-Flash"})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	_, err = p.Stream(context.Background(), provider.Request{})
	if !errors.Is(err, provider.ErrUnknownModel) {
		t.Fatalf("Stream error = %#v, want provider.ErrUnknownModel", err)
	}
	var unknown *UnknownModelError
	if !errors.As(err, &unknown) || unknown.Ref != "paratera/DeepSeek-V4-Flash" {
		t.Fatalf("Stream error = %#v, want *UnknownModelError naming the ref", err)
	}
}

// The far side starts a session on whatever the catalog marks, so the mark has
// to survive the wire.
func TestCatalogCarriesItsDefault(t *testing.T) {
	fake := &fakeProvider{name: "home"}
	client := newPair(t, staticResolver(fake,
		provider.Descriptor{Ref: "home/a"},
		provider.Descriptor{Ref: "home/b", Default: true},
	))
	if got := provider.DefaultRef(client.Catalog()); got != "home/b" {
		t.Fatalf("default across the wire = %q, want home/b", got)
	}
}

// Cancelling the remote turn has to abort the local provider's request, or the
// tunnel keeps paying for an answer nobody is reading.
func TestCancelReachesTheLocalProvider(t *testing.T) {
	released := make(chan struct{})
	fake := &fakeProvider{
		name:   "p",
		script: []provider.Chunk{{Type: provider.ChunkText, Text: "first"}},
		block:  released,
	}
	client := newPair(t, staticResolver(fake, provider.Descriptor{Ref: "p/m"}))
	t.Cleanup(func() { close(released) })

	ctx, cancel := context.WithCancel(context.Background())
	p, _ := client.Resolve(provider.Selection{Ref: "p/m"})
	ch, err := p.Stream(ctx, provider.Request{})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	if first := <-ch; first.Text != "first" {
		t.Fatalf("want the first chunk, got %+v", first)
	}
	cancel()
	done := make(chan struct{})
	go func() { defer close(done); drain(t, ch) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the channel stayed open after the turn was cancelled")
	}
}

// A Local is what a host actually runs: the loopback listener an -R forward
// publishes on each machine that resolves providers here.
func TestLocalListensOnLoopbackWithItsOwnToken(t *testing.T) {
	fake := &fakeProvider{name: "p", script: []provider.Chunk{{Type: provider.ChunkDone}}}
	local, err := Listen(staticResolver(fake, provider.Descriptor{Ref: "p/m"}))
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	t.Cleanup(func() { local.Close() })

	host, _, err := net.SplitHostPort(local.Addr)
	if err != nil {
		t.Fatalf("Addr %q: %v", local.Addr, err)
	}
	if ip := net.ParseIP(host); ip == nil || !ip.IsLoopback() {
		t.Fatalf("the broker listens on %s, which is not loopback", local.Addr)
	}
	if len(local.Token) < 32 {
		t.Fatalf("token is %d chars; too short to be worth guessing against", len(local.Token))
	}

	client := NewClient("http://"+local.Addr, local.Token, nil)
	if got := client.Catalog(); len(got) != 1 || got[0].Ref != "p/m" {
		t.Fatalf("the running broker did not answer its catalog: %+v", got)
	}
	wrong := NewClient("http://"+local.Addr, "not-the-token", nil)
	if got := wrong.Catalog(); len(got) != 0 {
		t.Fatalf("a rejected token read the catalog: %+v", got)
	}
	if err := local.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := local.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
}

// Two brokers are two token holders; neither may accept the other's.
func TestEachLocalMintsItsOwnToken(t *testing.T) {
	fake := &fakeProvider{name: "p"}
	first, err := Listen(staticResolver(fake, provider.Descriptor{Ref: "p/m"}))
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	t.Cleanup(func() { first.Close() })
	second, err := Listen(staticResolver(fake, provider.Descriptor{Ref: "p/m"}))
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	t.Cleanup(func() { second.Close() })

	if first.Token == second.Token {
		t.Fatal("two brokers minted the same token")
	}
	crossed := NewClient("http://"+second.Addr, first.Token, nil)
	if got := crossed.Catalog(); len(got) != 0 {
		t.Fatalf("one broker accepted another's token: %+v", got)
	}
}

// A provider resolved before the broker moved keeps working after it: the
// endpoint is asked per request, so a session built against the old port
// reaches the new one without being rebuilt.
func TestAResolvedProviderFollowsAMovedBroker(t *testing.T) {
	desc := provider.Descriptor{Ref: "p/m"}
	serve := func(name string) *httptest.Server {
		fake := &fakeProvider{name: name, script: []provider.Chunk{{Type: provider.ChunkText, Text: name}, {Type: provider.ChunkDone}}}
		srv, err := NewServer(staticResolver(fake, desc), testToken)
		if err != nil {
			t.Fatal(err)
		}
		ts := httptest.NewServer(srv.Handler())
		t.Cleanup(ts.Close)
		return ts
	}
	first, second := serve("first"), serve("second")
	at := first.URL
	client := NewEndpointClient(func() (string, string, error) { return at, testToken, nil }, nil)
	p, err := client.Resolve(provider.Selection{Ref: desc.Ref})
	if err != nil {
		t.Fatal(err)
	}
	say := func() string {
		ch, err := p.Stream(context.Background(), provider.Request{Messages: []provider.Message{{Role: provider.RoleUser, Content: "hi"}}})
		if err != nil {
			t.Fatalf("Stream: %v", err)
		}
		return drain(t, ch)[0].Text
	}
	if got := say(); got != "first" {
		t.Fatalf("before the move = %q, want first", got)
	}
	at = second.URL
	if got := say(); got != "second" {
		t.Fatalf("after the move = %q, want the broker the endpoint now names", got)
	}
}

// An endpoint that cannot answer fails that request, named as a lookup.
func TestAnEndpointThatCannotAnswerFailsTheRequest(t *testing.T) {
	gone := errors.New("the address file is missing")
	client := NewEndpointClient(func() (string, string, error) { return "", "", gone }, nil)
	p, err := client.Resolve(provider.Selection{Ref: "p/m"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.Stream(context.Background(), provider.Request{}); !errors.Is(err, gone) {
		t.Fatalf("Stream err = %v, want the endpoint's error", err)
	}
	if got := client.Catalog(); len(got) != 0 {
		t.Fatalf("catalog = %v, want empty when the broker cannot be located", got)
	}
}
