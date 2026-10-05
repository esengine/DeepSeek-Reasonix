package agent

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"

	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/contract/tool"
	"reasonix/internal/safety/evidence"
	"reasonix/internal/state/sessionstore"
)

// Restart equivalence: resuming at a turn boundary must leave the host deciding
// what it would have decided uninterrupted. The restarted arm keeps only what a
// resume keeps: the transcript and the goal sidecar's DeliveryCheckpoint.

// restartStep is one user turn and the model's replies to it, in order. A step
// that runs out of replies answers "done", so a divergence shows up as a
// verdict rather than as a hang.
type restartStep struct {
	input   string
	replies [][]provider.Chunk
}

type restartScenario struct {
	scope    string
	task     string
	options  Options
	registry func() *tool.Registry
	steps    []restartStep
}

// stepVerdict is what the host decided when a step ended.
type stepVerdict struct {
	Ready   bool
	Missing []string
}

func (v stepVerdict) String() string {
	if v.Ready {
		return "ready"
	}
	return "owes " + strings.Join(v.Missing, ",")
}

// stepProvider replays the current step's replies and then says "done".
type stepProvider struct {
	replies [][]provider.Chunk
}

func (p *stepProvider) Name() string { return "restart-equivalence" }

func (p *stepProvider) Stream(context.Context, provider.Request) (<-chan provider.Chunk, error) {
	reply := []provider.Chunk{{Type: provider.ChunkText, Text: "done"}, {Type: provider.ChunkDone}}
	if len(p.replies) > 0 {
		reply, p.replies = p.replies[0], p.replies[1:]
	}
	ch := make(chan provider.Chunk, len(reply))
	for _, c := range reply {
		ch <- c
	}
	close(ch)
	return ch, nil
}

// run plays the scenario, restarting after step restartAfter (1-based); zero
// never restarts.
func (s restartScenario) run(t *testing.T, restartAfter int) []stepVerdict {
	t.Helper()
	prov := &stepProvider{}
	boot := func(session *sessionstore.Session) *Agent {
		return New(prov, s.registry(), session, s.options, event.Discard)
	}
	a := boot(sessionstore.NewSession(""))
	ctx := deliveryGoalContext(s.scope, s.task)
	verdicts := make([]stepVerdict, 0, len(s.steps))
	for i, step := range s.steps {
		if i > 0 && i == restartAfter {
			a = restart(t, a, boot)
		}
		prov.replies = step.replies
		verdicts = append(verdicts, verdictOf(t, a.Run(ctx, step.input)))
	}
	return verdicts
}

// restart is what survives a process boundary: the transcript and the
// delivery checkpoint, serialized as the sidecar serializes it. Nothing else
// the running agent held is carried.
func restart(t *testing.T, running *Agent, boot func(*sessionstore.Session) *Agent) *Agent {
	t.Helper()
	raw, err := json.Marshal(running.DeliveryCheckpoint())
	if err != nil {
		t.Fatal(err)
	}
	var persisted evidence.DeliveryCheckpoint
	if err := json.Unmarshal(raw, &persisted); err != nil {
		t.Fatal(err)
	}
	session := sessionstore.NewSession("")
	session.Messages = slices.Clone(running.Session().Messages)
	resumed := boot(session)
	resumed.RestoreDeliveryCheckpoint(persisted)
	return resumed
}

func verdictOf(t *testing.T, err error) stepVerdict {
	t.Helper()
	if err == nil {
		return stepVerdict{Ready: true}
	}
	var unready *FinalReadinessError
	if !errors.As(err, &unready) {
		t.Fatalf("step failed outside readiness: %v", err)
	}
	missing := slices.Clone(unready.Missing)
	slices.Sort(missing)
	return stepVerdict{Missing: missing}
}

func deliveryRegistry() *tool.Registry {
	reg := evidenceRegistry()
	reg.Add(fakeTool{name: "read_file", readOnly: true})
	reg.Add(fakeTool{name: "review", readOnly: true})
	return reg
}

// shipStep writes path, inspects and verifies it, and signs off.
func shipStep(path string) restartStep {
	return restartStep{input: "implement it", replies: [][]provider.Chunk{
		{toolCallChunk("criteria", "todo_write", `{"todos":[{"content":"Ship it","status":"in_progress"}]}`), {Type: provider.ChunkDone}},
		{toolCallChunk("write", "write_file", `{"path":"`+path+`"}`), {Type: provider.ChunkDone}},
		{toolCallChunk("inspect", "read_file", `{"path":"`+path+`"}`), {Type: provider.ChunkDone}},
		{toolCallChunk("verify", "bash", `{"command":"go test ./..."}`), {Type: provider.ChunkDone}},
		{toolCallChunk("signoff", "complete_step", `{"step":"Ship it","result":"implemented","evidence":[{"kind":"verification","summary":"tests pass","command":"go test ./..."}]}`), {Type: provider.ChunkDone}},
	}}
}

var finishStep = restartStep{input: "finish the goal"}

// reverifyStep redoes everything a change owes except a review.
func reverifyStep(path string) restartStep {
	return restartStep{input: "verify again and finish", replies: [][]provider.Chunk{
		{toolCallChunk("inspect-2", "read_file", `{"path":"`+path+`"}`), {Type: provider.ChunkDone}},
		{toolCallChunk("verify-2", "bash", `{"command":"go test ./..."}`), {Type: provider.ChunkDone}},
		{toolCallChunk("signoff-2", "complete_step", `{"step":"Ship it","result":"implemented","evidence":[{"kind":"verification","summary":"tests pass","command":"go test ./..."}]}`), {Type: provider.ChunkDone}},
	}}
}

func TestRestartEquivalence(t *testing.T) {
	cases := []struct {
		name         string
		scenario     restartScenario
		restartAfter int
		// settles is the step the uninterrupted arm must end ready on, so an
		// agreement cannot be two arms stuck the same way.
		settles int
		// knownStricter pins what a restart is known to re-demand at a step:
		// proof whose receipts did not cross the boundary. The day it stops,
		// or grows, this case fails.
		knownStricter map[int][]string
	}{
		{
			name: "signed-off ordinary change",
			scenario: restartScenario{scope: "goal-plain", task: "ship main", registry: deliveryRegistry,
				options: Options{DeliveryProfile: true},
				steps:   []restartStep{shipStep("main.go"), finishStep}},
			restartAfter: 1,
			settles:      2,
		},
		{
			// The review is owed through the checkpoint, and the proof already
			// given rides it too, so a restart asks for exactly the review.
			name: "high-risk change still owes review",
			scenario: restartScenario{scope: "goal-auth", task: "change auth", registry: deliveryRegistry,
				options: Options{DeliveryProfile: true, ProjectSensitivePaths: []string{"auth/**"}},
				steps:   []restartStep{shipStep("auth/login.go"), finishStep, reverifyStep("auth/login.go")}},
			restartAfter: 1,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			uninterrupted := tc.scenario.run(t, 0)
			restarted := tc.scenario.run(t, tc.restartAfter)
			t.Logf("uninterrupted %v; restarted after step %d %v", uninterrupted, tc.restartAfter, restarted)
			if tc.settles > 0 && !uninterrupted[tc.settles-1].Ready {
				t.Fatalf("the scenario never settles: step %d %s", tc.settles, uninterrupted[tc.settles-1])
			}
			for i := range uninterrupted {
				step := i + 1
				for _, owed := range uninterrupted[i].Missing {
					if !slices.Contains(restarted[i].Missing, owed) {
						t.Fatalf("step %d: the restart dropped %s, which the uninterrupted run still owes", step, owed)
					}
				}
				var extra []string
				for _, owed := range restarted[i].Missing {
					if !slices.Contains(uninterrupted[i].Missing, owed) {
						extra = append(extra, owed)
					}
				}
				if want := tc.knownStricter[step]; !slices.Equal(extra, want) {
					t.Fatalf("step %d: the restart re-demands %v, known %v", step, extra, want)
				}
			}
		})
	}
}
