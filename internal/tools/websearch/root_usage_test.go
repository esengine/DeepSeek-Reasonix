package websearch

import (
	"context"
	"errors"
	"testing"

	"reasonix/internal/contract/provider"
)

type observedSearchProvider struct {
	chunks []provider.Chunk
	after  func()
	err    error
}

func (*observedSearchProvider) Name() string { return "observed-search" }
func (p *observedSearchProvider) Stream(ctx context.Context, _ provider.Request) (<-chan provider.Chunk, error) {
	if p.err != nil {
		return nil, p.err
	}
	ch := make(chan provider.Chunk)
	go func() {
		defer close(ch)
		for _, chunk := range p.chunks {
			select {
			case ch <- chunk:
			case <-ctx.Done():
				return
			}
		}
		if p.after != nil {
			p.after()
		}
	}()
	return ch, nil
}

func TestRootWebSearchUsageTerminal(t *testing.T) {
	billedFailure := errors.New("billed native search failed")
	for _, tc := range []struct {
		name       string
		missing    bool
		noReport   bool
		noObserver bool
		invalid    bool
		noFactory  bool
		factoryErr bool
		cancel     bool
		preCancel  bool
		streamErr  bool
		fail       bool
		want       int
	}{
		{name: "reported", want: 1},
		{name: "no_reporter", noReport: true, want: 1},
		{name: "missing", missing: true},
		{name: "no_observer", noObserver: true},
		{name: "input_refused", invalid: true},
		{name: "no_factory", noFactory: true},
		{name: "factory_error", factoryErr: true},
		{name: "billed_failure", fail: true, want: 1},
		{name: "cancel_after_usage", cancel: true, want: 1},
		{name: "cancel_before_start", preCancel: true},
		{name: "stream_error", streamErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			usage := &provider.Usage{PromptTokens: 1000, CompletionTokens: 100, TotalTokens: 1100, RequestCount: 3}
			chunks := []provider.Chunk{}
			if !tc.missing {
				chunks = append(chunks, provider.Chunk{Type: provider.ChunkUsage, Usage: usage})
			}
			chunks = append(chunks, provider.Chunk{Type: provider.ChunkProviderTool, ToolCall: &provider.ToolCall{Name: "web_search"}, Text: "https://example.com/fixture"})
			if tc.fail {
				chunks = append(chunks, provider.Chunk{Type: provider.ChunkError, Err: billedFailure})
			} else if !tc.cancel {
				chunks = append(chunks, provider.Chunk{Type: provider.ChunkText, Text: "fixture answer"}, provider.Chunk{Type: provider.ChunkDone})
			}
			p := &observedSearchProvider{chunks: chunks}
			if tc.streamErr {
				p.err = billedFailure
			}
			if tc.cancel {
				p.after = cancel
			}
			calls, reports, factory := 0, 0, 0
			if !tc.noObserver {
				ctx = provider.WithAuxiliaryUsageObserver(ctx, func(u *provider.Usage) {
					calls++
					if u.PromptTokens != 1000 || u.RequestCount != 3 {
						t.Errorf("observer usage=%+v", u)
					}
					u.PromptTokens = -1
				})
			}
			search := &Tool{Factory: func() (provider.Provider, error) {
				factory++
				if tc.factoryErr {
					return nil, ErrUnavailable
				}
				return p, nil
			}, ReportUsage: func(u *provider.Usage) {
				reports++
				if u.PromptTokens != 1000 || u.RequestCount != 3 {
					t.Errorf("observer changed reported usage=%+v", u)
				}
			}}
			if tc.noFactory {
				search.Factory = nil
			}
			if tc.noReport {
				search.ReportUsage = nil
			}
			args := []byte(`{"query":"fixture"}`)
			if tc.invalid {
				args = []byte(`{"query":" "}`)
			}
			if tc.preCancel {
				cancel()
			}
			out, err := search.Execute(ctx, args)
			switch {
			case tc.fail || tc.streamErr:
				if !errors.Is(err, billedFailure) {
					t.Fatal(err)
				}
			case tc.cancel || tc.preCancel:
				if !errors.Is(err, context.Canceled) {
					t.Fatal(err)
				}
				if tc.preCancel && factory != 0 {
					t.Fatal("canceled request reached the factory")
				}
			case tc.invalid:
				if !errors.Is(err, ErrQuery) || factory != 0 {
					t.Fatalf("refusal=%v factory=%d", err, factory)
				}
			case tc.noFactory || tc.factoryErr:
				if !errors.Is(err, ErrUnavailable) {
					t.Fatal(err)
				}
			default:
				if err != nil || out == "" {
					t.Fatalf("result=%q err=%v", out, err)
				}
			}
			wantReport := 1
			if tc.missing || tc.noReport || tc.invalid || tc.noFactory || tc.factoryErr || tc.streamErr || tc.preCancel {
				wantReport = 0
			}
			if calls != tc.want || reports != wantReport || usage.PromptTokens != 1000 {
				t.Fatalf("observe/report/original=%d/%d/%d want%d/%d/1000", calls, reports, usage.PromptTokens, tc.want, wantReport)
			}
		})
	}
}
