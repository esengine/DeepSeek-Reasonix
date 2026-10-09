package boot

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"reasonix/internal/base/i18n"
	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/runtime/agent"
)

type pacedDigestProvider struct {
	interval time.Duration
	words    int
	stall    bool
}

func (p pacedDigestProvider) Name() string { return "boot-paced-digest" }

func (p pacedDigestProvider) Stream(ctx context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	ch := make(chan provider.Chunk)
	digest := strings.Contains(req.Messages[0].Content, "compacting the earlier part")
	go func() {
		defer close(ch)
		if !digest {
			ch <- provider.Chunk{Type: provider.ChunkText, Text: strings.Repeat("work output line with detail. ", 400)}
			ch <- provider.Chunk{Type: provider.ChunkDone}
			return
		}
		for i := range p.words {
			select {
			case <-ctx.Done():
				return
			case <-time.After(p.interval):
			}
			select {
			case ch <- provider.Chunk{Type: provider.ChunkText, Text: fmt.Sprintf("## part %d\n", i)}:
			case <-ctx.Done():
				return
			}
		}
		if p.stall {
			<-ctx.Done()
			return
		}
		ch <- provider.Chunk{Type: provider.ChunkDone}
	}()
	return ch, nil
}

func compactOnPacedProvider(t *testing.T, prov pacedDigestProvider) *noticeSink {
	t.Helper()
	isolateConfigHome(t)
	dir := robustTempDir(t)
	t.Chdir(dir)
	kind := "boot-paced-digest-" + t.Name()
	provider.Register(kind, func(provider.Config) (provider.Provider, error) { return prov, nil })
	writeFile(t, dir, "reasonix.toml", `
default_model = "test-model"

[agent]
system_prompt = "BASE"

[[providers]]
name = "test-model"
kind = "`+kind+`"
model = "x"
context_window = 200000
`)
	approveWorkspace(t, dir)
	sink := &noticeSink{}
	ctrl, err := Build(context.Background(), Options{Sink: sink})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	t.Cleanup(func() { ctrl.Close() })
	for range 6 {
		_ = ctrl.Run(context.Background(), "keep going")
	}
	ctrl.Submit("/compact")
	return sink
}

func waitNotice(t *testing.T, sink *noticeSink, codes ...string) event.Event {
	t.Helper()
	for range 200 {
		for _, c := range codes {
			if e, ok := sink.notice(c); ok {
				return e
			}
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("no %v notice reached the sink", codes)
	return event.Event{}
}

func TestEffectSteadySlowDigestCompactsThroughBuild(t *testing.T) {
	prev := agent.SummaryBounds
	agent.SummaryBounds = agent.SummaryLimits{Idle: 600 * time.Millisecond, Ceiling: 30 * time.Second}
	defer func() { agent.SummaryBounds = prev }()
	sink := compactOnPacedProvider(t, pacedDigestProvider{interval: 80 * time.Millisecond, words: 10})
	waitNotice(t, sink, event.NoticeCodeCompacted, event.NoticeCodeCompactFailed)
	if _, failed := sink.notice(event.NoticeCodeCompactFailed); failed {
		t.Fatal("a steadily streaming digest was cut off")
	}
	progress := 0
	sink.mu.Lock()
	for _, e := range sink.ev {
		if e.Kind == event.CompactionProgress {
			progress++
		}
	}
	sink.mu.Unlock()
	if progress < 2 {
		t.Fatalf("frontend saw %d digest deltas, want the stream forwarded", progress)
	}
}

func TestEffectStalledDigestNoticeNamesTheStall(t *testing.T) {
	prev := agent.SummaryBounds
	agent.SummaryBounds = agent.SummaryLimits{Idle: 150 * time.Millisecond, Ceiling: 10 * time.Second}
	defer func() { agent.SummaryBounds = prev }()
	sink := compactOnPacedProvider(t, pacedDigestProvider{interval: 20 * time.Millisecond, words: 2, stall: true})
	got := waitNotice(t, sink, event.NoticeCodeCompactFailed)
	if got.Detail != "summary_timeout" {
		t.Fatalf("detail = %q, want summary_timeout", got.Detail)
	}
}

func TestEffectCeilingNoticeIsItsOwnCode(t *testing.T) {
	prev := agent.SummaryBounds
	agent.SummaryBounds = agent.SummaryLimits{Idle: time.Second, Ceiling: 300 * time.Millisecond}
	defer func() { agent.SummaryBounds = prev }()
	sink := compactOnPacedProvider(t, pacedDigestProvider{interval: 30 * time.Millisecond, words: 1 << 20})
	got := waitNotice(t, sink, event.NoticeCodeCompactFailed)
	if got.Detail != "summary_ceiling" {
		t.Fatalf("detail = %q, want summary_ceiling", got.Detail)
	}
}

func TestBoundWordingStatesTheBoundsTheKernelEnforces(t *testing.T) {
	for _, m := range []i18n.Messages{i18n.English, i18n.Chinese, i18n.ChineseTraditional} {
		idle := fmt.Sprint(int(agent.SummaryIdleTimeout / time.Minute))
		ceiling := fmt.Sprint(int(agent.SummaryCeiling / time.Minute))
		if !strings.Contains(m.CompactionWhy["summary_timeout"], idle) {
			t.Errorf("summary_timeout wording %q omits the %s-minute idle bound", m.CompactionWhy["summary_timeout"], idle)
		}
		if !strings.Contains(m.CompactionWhy["summary_ceiling"], ceiling) {
			t.Errorf("summary_ceiling wording %q omits the %s-minute ceiling", m.CompactionWhy["summary_ceiling"], ceiling)
		}
	}
}
