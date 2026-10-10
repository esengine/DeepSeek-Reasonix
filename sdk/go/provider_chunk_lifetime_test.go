package extension

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"
)

type chunkLifetimeRun struct {
	ctx  context.Context
	done chan struct{}
}

type chunkLifetimeProvider struct {
	invalid StreamChunk
	stop    chan struct{}
	runs    chan chunkLifetimeRun
}

func (*chunkLifetimeProvider) Catalog(context.Context) ([]ProviderDescriptor, error) {
	return []ProviderDescriptor{}, nil
}

func (p *chunkLifetimeProvider) Stream(ctx context.Context, req StreamRequest) (<-chan StreamChunk, error) {
	chunks := make(chan StreamChunk)
	run := chunkLifetimeRun{ctx: ctx, done: make(chan struct{})}
	p.runs <- run
	go func() {
		defer close(run.done)
		defer close(chunks)
		sequence := []StreamChunk{TextChunk("prefix")}
		if req.StreamID == "reject" {
			sequence = append(sequence, p.invalid, TextChunk("trailer"))
		}
		for _, chunk := range sequence {
			select {
			case chunks <- chunk:
			case <-ctx.Done():
				return
			case <-p.stop:
				return
			}
		}
		if req.StreamID == "cancel" {
			select {
			case <-ctx.Done():
			case <-p.stop:
			}
		}
	}()
	return chunks, nil
}

func TestProviderInvalidChunkStopsUnbufferedProducer(t *testing.T) {
	for _, tc := range []struct {
		name  string
		chunk StreamChunk
	}{
		{name: "zero"},
		{name: "unknown", chunk: StreamChunk{Type: "unknown"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			provider := &chunkLifetimeProvider{invalid: tc.chunk, stop: make(chan struct{}), runs: make(chan chunkLifetimeRun, 3)}
			var runs []chunkLifetimeRun
			t.Cleanup(func() {
				close(provider.stop)
				for _, run := range runs {
					select {
					case <-run.done:
					case <-time.After(time.Second):
						t.Error("test producer did not stop during cleanup")
					}
				}
			})
			host, _ := startFakeHost(t, providerHandler(), Options{Provider: provider})
			host.handshake(t)
			waitEnd := func(id string) StreamEndParams {
				for deadline := time.Now().Add(time.Second); time.Now().Before(deadline); {
					_, ends := host.streamNotifications()
					for _, end := range ends {
						if end.StreamID == id {
							return end
						}
					}
					time.Sleep(5 * time.Millisecond)
				}
				t.Fatalf("stream %s did not end", id)
				return StreamEndParams{}
			}
			open := openStreamRequest("reject")
			open.SeqBase = 7
			resp := host.request(MethodExtensionProviderStreamOpen, open)
			if resp.Err != nil {
				t.Fatalf("open: %+v", resp.Err)
			}
			run := <-provider.runs
			runs = append(runs, run)
			end := waitEnd("reject")
			sent, ends := host.streamNotifications()
			if len(sent) != 1 || len(ends) != 1 || sent[0].StreamID != "reject" || sent[0].Seq != 7 || sent[0].Chunk.Text != "prefix" || end.StreamID != "reject" || end.LastSeq != 7 || end.Error == "" || end.Interrupted {
				t.Fatalf("rejected stream: sent=%+v ends=%+v", sent, ends)
			}
			select {
			case <-run.done:
				if !errors.Is(run.ctx.Err(), context.Canceled) {
					t.Fatalf("producer context = %v", run.ctx.Err())
				}
			case <-time.After(time.Second):
				t.Fatal("unbuffered producer remains blocked after chunk rejection")
			}
			resp = host.request(MethodExtensionProviderStreamOpen, openStreamRequest("reused"))
			if resp.Err != nil {
				t.Fatalf("subsequent stream: %+v", resp.Err)
			}
			runs = append(runs, <-provider.runs)
			end = waitEnd("reused")
			if end.StreamID != "reused" || end.LastSeq != 1 || end.Error != "" || end.Interrupted {
				t.Fatalf("subsequent stream end = %+v", end)
			}
			resp = host.request(MethodExtensionProviderStreamOpen, openStreamRequest("cancel"))
			if resp.Err != nil {
				t.Fatalf("cancellable stream: %+v", resp.Err)
			}
			run = <-provider.runs
			runs = append(runs, run)
			deadline := time.Now().Add(time.Second)
			for {
				sent, _ = host.streamNotifications()
				if len(sent) == 3 && sent[2].StreamID == "cancel" {
					break
				}
				if time.Now().After(deadline) {
					t.Fatal("cancellable stream did not deliver its prefix")
				}
				time.Sleep(5 * time.Millisecond)
			}
			resp = host.request(MethodExtensionProviderStreamCancel, StreamCancelParams{StreamID: "cancel"})
			var cancelled StreamCancelResult
			if resp.Err != nil || json.Unmarshal(resp.Result, &cancelled) != nil || !cancelled.Cancelled {
				t.Fatalf("cancel: %+v", resp)
			}
			end = waitEnd("cancel")
			if end.StreamID != "cancel" || end.LastSeq != 1 || !end.Interrupted || end.Error != "" {
				t.Fatalf("cancel end = %+v", end)
			}
			select {
			case <-run.done:
			case <-time.After(time.Second):
				t.Fatal("explicit cancellation did not release producer")
			}
			sent, ends = host.streamNotifications()
			if len(sent) != 3 || len(ends) != 3 {
				t.Fatalf("notifications: sent=%+v ends=%+v", sent, ends)
			}
		})
	}
}
