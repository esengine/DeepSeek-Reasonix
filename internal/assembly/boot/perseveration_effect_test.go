package boot

// Boot-level effect tests for the perseveration guard: they assert what
// actually reaches the provider request boundary and the frontend sink through
// the real Build stack, not what a component returns in isolation.

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"reasonix/internal/contract/config"
	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/runtime/agent"
	"reasonix/internal/session/control"
)

// perseverationKindSeq keeps each build's registered provider kind unique: two
// runs in one test function would otherwise register the same kind twice.
var perseverationKindSeq atomic.Uint64

// perseverationLoopProvider streams a byte-identical block of prose every agent
// round, then ends the round. Requests without a tool surface (titles, triage)
// get a plain answer so they never loop.
type perseverationLoopProvider struct {
	mu     sync.Mutex
	reqs   []provider.Request
	rounds int
}

func (p *perseverationLoopProvider) Name() string { return "boot-perseveration" }

// perseverationBlock is the recorded "Let me write / Hmm / OK" loop. 400 copies
// clear the guard's >=8 repeats and >=1 KiB floor.
const perseverationBlock = "Let me write.\n\nHmm.\n\nOK.\n\n"

func (p *perseverationLoopProvider) Stream(_ context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	p.mu.Lock()
	p.reqs = append(p.reqs, req)
	p.rounds++
	p.mu.Unlock()
	ch := make(chan provider.Chunk, 2)
	if len(req.Tools) == 0 {
		ch <- provider.Chunk{Type: provider.ChunkText, Text: "ok"}
	} else {
		ch <- provider.Chunk{Type: provider.ChunkText, Text: strings.Repeat(perseverationBlock, 400)}
	}
	ch <- provider.Chunk{Type: provider.ChunkDone}
	close(ch)
	return ch, nil
}

func (p *perseverationLoopProvider) requests() []provider.Request {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]provider.Request(nil), p.reqs...)
}

type perseverationRun struct {
	err    error
	reqs   []provider.Request
	events []event.Event
}

// runPerseveration builds the real stack around the looping provider with
// projectExtra in reasonix.toml and userConfig in the user file, runs once, and
// returns the executor's provider requests and everything the sink saw.
func runPerseveration(t *testing.T, projectExtra, userConfig string) perseverationRun {
	t.Helper()
	return runPerseverationProvider(t, projectExtra, userConfig, "")
}

// runPerseverationProvider is runPerseveration with extra keys in the
// [[providers]] block, for the per-provider override.
func runPerseverationProvider(t *testing.T, projectExtra, userConfig, providerExtra string) perseverationRun {
	t.Helper()
	isolateConfigHome(t)
	dir := robustTempDir(t)
	t.Chdir(dir)
	if userConfig != "" {
		path := config.UserConfigPath()
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(userConfig), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	kind := "boot-perseveration-" + strings.NewReplacer("/", "-", " ", "-").Replace(strings.ToLower(t.Name())) +
		fmt.Sprintf("-%d", perseverationKindSeq.Add(1))
	rec := &perseverationLoopProvider{}
	provider.Register(kind, func(provider.Config) (provider.Provider, error) { return rec, nil })
	writeFile(t, dir, "reasonix.toml", `
default_model = "test-model"
`+projectExtra+`
[codegraph]
enabled = false

[[providers]]
name = "test-model"
kind = "`+kind+`"
model = "x"
`+providerExtra+`
`)
	approveWorkspace(t, dir)
	sink := &watchSink{}
	ctrl, err := Build(context.Background(), Options{Sink: sink})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	t.Cleanup(func() { ctrl.Close() })
	ctrl.SetToolApprovalMode(control.ToolApprovalYolo)
	runErr := ctrl.Run(context.Background(), "write the file")
	return perseverationRun{err: runErr, reqs: agentRequests(rec.requests()), events: sinkEvents(sink)}
}

func sinkEvents(s *watchSink) []event.Event {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]event.Event(nil), s.events...)
}

func perseverationReports(events []event.Event) []event.ProgressWatch {
	var out []event.ProgressWatch
	for _, e := range events {
		if e.Kind == event.ProgressWatchEvent && e.ProgressWatch != nil && e.ProgressWatch.Cause == event.ProgressWatchCausePerseveration {
			out = append(out, *e.ProgressWatch)
		}
	}
	return out
}

// The default: the guard detects but never cuts, so a byte-identical
// perseveration is reported through the progress-watch channel. The stream
// reaches its own terminal, the model is never nudged, and no cut notice reaches
// the sink.
func TestEffectPerseverationDefaultReportsWithoutCutting(t *testing.T) {
	run := runPerseveration(t, "", "")
	if run.err != nil {
		t.Fatalf("default run error = %v, want a clean terminal", run.err)
	}
	if len(run.reqs) != 1 {
		t.Fatalf("agent requests = %d, want 1: the default must not cut or retry", len(run.reqs))
	}
	reports := perseverationReports(run.events)
	if len(reports) == 0 {
		t.Fatal("default run did not report the loop through the progress-watch channel")
	}
	if !reports[len(reports)-1].Stalled {
		t.Fatalf("default report = %+v, want a stall", reports[len(reports)-1])
	}
	for _, e := range run.events {
		switch {
		case e.Kind == event.Steer:
			t.Fatalf("default run nudged the model: %q", e.Text)
		case e.Kind == event.Notice && (e.Code == event.NoticeCodePerseverationLoop || e.Code == event.NoticeCodePerseverationRetry):
			t.Fatalf("default run emitted a %s notice", e.Code)
		}
	}
}

// Opted in at zero: the loop is reported through the progress-watch channel,
// but the stream is not cut and the model is not nudged. This is the same as the
// unset default, kept explicit so the two paths stay pinned.
func TestEffectPerseverationReportsThroughProgressWatch(t *testing.T) {
	run := runPerseveration(t, "", "[progress_watch]\nperseveration_retries = 0\n")
	if run.err != nil {
		t.Fatalf("run error = %v, want a clean terminal", run.err)
	}
	if len(run.reqs) != 1 {
		t.Fatalf("agent requests = %d, want 1: zero budget must not cut or retry", len(run.reqs))
	}
	reports := perseverationReports(run.events)
	if len(reports) == 0 {
		t.Fatal("no perseveration report reached the frontend sink")
	}
	last := reports[len(reports)-1]
	if !last.Stalled || last.Pausing {
		t.Fatalf("perseveration report = %+v, want a non-pausing stall", last)
	}
}

// Opted in at one: the stream is cut, the retry carries the host-marked nudge
// and the looped assistant text, and the run ends with a perseveration pause so a
// parent cannot mistake the loop for a clean result.
func TestEffectPerseverationCutsAndNudges(t *testing.T) {
	run := runPerseveration(t, "", "[progress_watch]\nperseveration_retries = 1\n")
	info, ok := agent.InspectRunPause(run.err)
	if !ok || info.Kind != agent.PauseKindPerseveration {
		t.Fatalf("run error = %v (pause %+v, %v), want a perseveration pause", run.err, info, ok)
	}
	if len(run.reqs) != 2 {
		t.Fatalf("agent requests = %d, want 2 (one retry)", len(run.reqs))
	}
	// What reaches the provider: the retry carries the host nudge and the
	// trimmed assistant turn.
	second := run.reqs[1]
	nudged := false
	var assistant string
	for _, m := range second.Messages {
		if m.Role == provider.RoleUser && strings.Contains(m.Content, "retrying (1)") {
			nudged = true
		}
		if m.Role == provider.RoleAssistant {
			assistant = m.Content
		}
	}
	if !nudged {
		t.Fatal("the retry request did not carry the host nudge")
	}
	// The cut collapses the loop in the stored turn: the phrase and its last
	// repetition, not the whole loop.
	want := perseverationBlock + perseverationBlock
	if assistant != want {
		t.Fatalf("the retry carried %d bytes of the loop, want the phrase and its last repetition %q", len(assistant), want)
	}
	// What reaches the sink: the settled frame follows the trim, and the
	// interjection is shown between the trimmed turn and the retry's output.
	var settled string
	interjected := false
	for _, e := range run.events {
		switch {
		case e.Kind == event.Message:
			settled = e.Text
		case e.Kind == event.Steer && e.HostAuthored && strings.Contains(e.Text, "retrying (1)"):
			interjected = true
		case e.Kind == event.Notice && e.Code == event.NoticeCodePerseverationRetry && strings.Contains(e.Text, "retrying (1)"):
			interjected = true
		}
	}
	if settled != strings.TrimSpace(want) {
		t.Fatalf("the settled frame carried %d bytes, want the trimmed %q", len(settled), strings.TrimSpace(want))
	}
	if !interjected {
		t.Fatal("no interjection reached the sink for the retry nudge")
	}
}

// The user's pause switch ends a perseveration run resumably.
func TestEffectPerseverationPausesUnderUserSetting(t *testing.T) {
	run := runPerseveration(t,
		"",
		"[progress_watch]\npause = true\nrounds = 1000\ntoken_multiple = 1000\nperseveration_retries = 0\n")
	info, ok := agent.InspectRunPause(run.err)
	if !ok || info.Kind != agent.PauseKindPerseveration {
		t.Fatalf("run error = %v (pause %+v, %v), want a perseveration pause", run.err, info, ok)
	}
	reports := perseverationReports(run.events)
	if len(reports) == 0 || !reports[len(reports)-1].Pausing {
		t.Fatalf("perseveration reports = %+v, want a pausing one", reports)
	}
}

// A provider's own perseveration_retries is the setting for that provider: it
// enables the guard when the global default is unset, and a provider value of
// -1 disables it even when the global default would enable it.
func TestEffectPerseverationProviderOverridesGlobal(t *testing.T) {
	// Global unset, provider opts in: the guard runs.
	on := runPerseverationProvider(t, "", "", "perseveration_retries = 0\n")
	if on.err != nil {
		t.Fatalf("run error = %v, want a clean terminal", on.err)
	}
	if len(perseverationReports(on.events)) == 0 {
		t.Fatal("a provider-level perseveration_retries did not enable the guard")
	}

	// Global opts in, provider opts out: the guard stays off.
	off := runPerseverationProvider(t, "", "[progress_watch]\nperseveration_retries = 0\n", "perseveration_retries = -1\n")
	if off.err != nil {
		t.Fatalf("run error = %v, want a clean terminal", off.err)
	}
	if len(perseverationReports(off.events)) != 0 {
		t.Fatalf("a provider-level -1 did not disable the guard: %+v", perseverationReports(off.events))
	}
}
