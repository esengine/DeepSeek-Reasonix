package provider

import (
	"context"
	"errors"
	"reflect"
	"sync/atomic"
	"testing"
	"time"
)

type admissionFunc func(context.Context) (func(), error)

func (f admissionFunc) AcquireRequest(ctx context.Context) (func(), error) { return f(ctx) }

type admittedProvider struct {
	stream func(context.Context, Request) (<-chan Chunk, error)
}

func (p admittedProvider) Name() string { return "admission-fixture" }
func (p admittedProvider) Stream(ctx context.Context, req Request) (<-chan Chunk, error) {
	return p.stream(ctx, req)
}

func TestAdmissionRetainsCapacityUntilCancelledProviderCloses(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	input := make(chan Chunk)
	released := make(chan struct{})
	ctx = WithRequestAdmission(ctx, admissionFunc(func(context.Context) (func(), error) {
		return func() { close(released) }, nil
	}))
	output, err := Stream(ctx, admittedProvider{stream: func(context.Context, Request) (<-chan Chunk, error) {
		return input, nil
	}}, Request{})
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	select {
	case _, ok := <-output:
		if ok {
			t.Fatal("cancelled consumer received a chunk")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("cancelled consumer remained blocked")
	}
	select {
	case <-released:
		t.Fatal("logical cancellation released physical capacity")
	default:
	}
	input <- Chunk{Type: ChunkUsage, Usage: &Usage{PromptTokens: 10}}
	close(input)
	select {
	case <-released:
	case <-time.After(5 * time.Second):
		t.Fatal("completed source retained capacity")
	}
}

func TestAdmissionPreservesRequestAndChunksWithoutNestedReacquisition(t *testing.T) {
	var acquired, released atomic.Int32
	done := make(chan struct{})
	ctx := WithRequestAdmission(t.Context(), admissionFunc(func(context.Context) (func(), error) {
		acquired.Add(1)
		return func() { released.Add(1); close(done) }, nil
	}))
	req := Request{Messages: []Message{{Role: RoleSystem, Content: "stable prefix"}}, MaxTokens: 100}
	chunks := []Chunk{{Type: ChunkText, Text: "answer"}, {Type: ChunkDone}}
	inner := admittedProvider{stream: func(_ context.Context, got Request) (<-chan Chunk, error) {
		if !reflect.DeepEqual(got, req) {
			t.Errorf("request changed: %+v", got)
		}
		ch := make(chan Chunk, len(chunks))
		for _, chunk := range chunks {
			ch <- chunk
		}
		close(ch)
		return ch, nil
	}}
	outer := admittedProvider{stream: func(ctx context.Context, req Request) (<-chan Chunk, error) {
		return Stream(ctx, inner, req)
	}}
	stream, err := Stream(ctx, outer, req)
	if err != nil {
		t.Fatal(err)
	}
	var got []Chunk
	for chunk := range stream {
		got = append(got, chunk)
	}
	<-done
	if acquired.Load() != 1 || released.Load() != 1 || !reflect.DeepEqual(got, chunks) {
		t.Fatalf("acquired=%d released=%d chunks=%+v", acquired.Load(), released.Load(), got)
	}
}

func TestAdmissionFailedStartsReleaseExactlyOnce(t *testing.T) {
	for _, mode := range []string{"error", "nil_stream", "panic", "cancel_after_admission"} {
		t.Run(mode, func(t *testing.T) {
			var released, called atomic.Int32
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			ctx = WithRequestAdmission(ctx, admissionFunc(func(context.Context) (func(), error) {
				if mode == "cancel_after_admission" {
					cancel()
				}
				return func() { released.Add(1) }, nil
			}))
			p := admittedProvider{stream: func(context.Context, Request) (<-chan Chunk, error) {
				called.Add(1)
				switch mode {
				case "error":
					return nil, errors.New("request rejected")
				case "panic":
					panic("provider panic")
				default:
					return nil, nil
				}
			}}
			func() {
				defer func() {
					if caught := recover(); (caught != nil) != (mode == "panic") {
						t.Fatalf("unexpected panic result: %v", caught)
					}
				}()
				if _, err := Stream(ctx, p, Request{}); err == nil {
					t.Fatal("failed request succeeded")
				}
			}()
			if released.Load() != 1 || (mode == "cancel_after_admission" && called.Load() != 0) {
				t.Fatalf("released=%d called=%d", released.Load(), called.Load())
			}
		})
	}
}

func TestAdmissionRefusalNeverCallsProvider(t *testing.T) {
	want := errors.New("no capacity")
	ctx := WithRequestAdmission(t.Context(), admissionFunc(func(context.Context) (func(), error) {
		return nil, want
	}))
	p := admittedProvider{stream: func(context.Context, Request) (<-chan Chunk, error) {
		t.Fatal("provider called after admission refusal")
		return nil, nil
	}}
	if _, err := Stream(ctx, p, Request{}); !errors.Is(err, want) {
		t.Fatalf("got %v", err)
	}
}
