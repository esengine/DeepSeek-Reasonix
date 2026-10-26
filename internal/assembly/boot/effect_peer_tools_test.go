package boot

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/session/control"
	"reasonix/internal/state/peermail"
)

// A model told by injected text to message another session asks the user first,
// and until the user answers nothing has been written to that session's spool.
type peerToolProvider struct {
	mu   sync.Mutex
	reqs []provider.Request
	to   string
	text string
}

func (p *peerToolProvider) Name() string { return "boot-peer-tools" }

func (p *peerToolProvider) Stream(_ context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	p.mu.Lock()
	p.reqs = append(p.reqs, req)
	first := len(p.reqs) == 1
	p.mu.Unlock()
	ch := make(chan provider.Chunk, 3)
	offered := false
	for _, t := range req.Tools {
		offered = offered || t.Name == "send_peer_message"
	}
	if first && offered {
		args, _ := json.Marshal(map[string]string{"to": p.to, "text": p.text})
		ch <- provider.Chunk{Type: provider.ChunkToolCall, ToolCall: &provider.ToolCall{ID: "call-peer", Name: "send_peer_message", Arguments: string(args)}}
	} else {
		ch <- provider.Chunk{Type: provider.ChunkText, Text: "done"}
	}
	ch <- provider.Chunk{Type: provider.ChunkDone}
	close(ch)
	return ch, nil
}

func (p *peerToolProvider) toolNames() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	var names []string
	if len(p.reqs) > 0 {
		for _, t := range p.reqs[0].Tools {
			names = append(names, t.Name)
		}
	}
	return names
}

func buildPeerToolSession(t *testing.T, enabled bool, to, text string, peersExtra ...string) (*peerToolProvider, *control.Controller, string, *struct {
	mu        sync.Mutex
	approvals []event.Approval
}) {
	t.Helper()
	home := statedBootHome(t)
	dir := testenv.TempDir(t)
	rec := &peerToolProvider{to: to, text: text}
	kind := fmt.Sprintf("boot-peer-tools-%s-%v", strings.ReplaceAll(t.Name(), "/", "-"), enabled)
	provider.Register(kind, func(provider.Config) (provider.Provider, error) { return rec, nil })
	writeFile(t, dir, "reasonix.toml", `
default_model = "test-model"

[codegraph]
enabled = false

[[providers]]
name = "test-model"
kind = "`+kind+`"
model = "x"
`)
	if enabled {
		writeFile(t, home, "config.toml", "[peers]\nenabled = true\n"+strings.Join(peersExtra, ""))
	}
	approveWorkspaceAt(t, home, dir)
	seen := &struct {
		mu        sync.Mutex
		approvals []event.Approval
	}{}
	var ctrl *control.Controller
	var ready sync.WaitGroup
	ready.Add(1)
	sink := event.FuncSink(func(e event.Event) {
		if e.Kind == event.ApprovalRequest {
			seen.mu.Lock()
			seen.approvals = append(seen.approvals, e.Approval)
			seen.mu.Unlock()
		}
	})
	var err error
	ctrl, err = Build(context.Background(), Options{Home: home, WorkspaceRoot: dir, Sink: sink})
	ready.Done()
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	t.Cleanup(ctrl.Close)
	if err := ctrl.NewSession(); err != nil {
		t.Fatal(err)
	}
	ctrl.SetToolApprovalMode(control.ToolApprovalAsk)
	ctrl.EnableInteractiveApproval()
	return rec, ctrl, home + "/peers", seen
}

func TestEffectPeerToolsExistOnlyWhereMessagesBetweenSessionsAreOn(t *testing.T) {
	t.Parallel()
	for _, enabled := range []bool{true, false} {
		rec, ctrl, _, _ := buildPeerToolSession(t, enabled, "nobody", "hello")
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		_ = ctrl.Run(ctx, "hi")
		cancel()
		has := map[string]bool{}
		for _, name := range rec.toolNames() {
			has[name] = true
		}
		if has["list_peers"] != enabled || has["send_peer_message"] != enabled {
			t.Fatalf("enabled=%v: tools in the first request = %v", enabled, rec.toolNames())
		}
	}
}

func TestEffectAnInjectedRequestToMessageAnotherSessionAsksFirstAndWritesNothingUntilApproved(t *testing.T) {
	t.Parallel()
	for _, allow := range []bool{true, false} {
		t.Run(fmt.Sprintf("allow=%v", allow), func(t *testing.T) {
			t.Parallel()
			const receiver = "s-00000000000000aa"
			const words = "INJECTED-TEXT-SAYS-SEND-THE-PLAN"
			_, ctrl, peersDir, seen := buildPeerToolSession(t, true, receiver, words)
			d, err := peermail.Open(peersDir)
			if err != nil {
				t.Fatal(err)
			}
			if err := d.PublishLive(peermail.Live{SessionID: receiver, Title: "docs", Project: "notes", Inbound: "hold", BeatAt: time.Now(), StartedAt: time.Now()}); err != nil {
				t.Fatal(err)
			}
			waitFor(t, "the other session to be listed", func() bool { return len(ctrl.Peers()) == 1 })

			done := make(chan error, 1)
			go func() {
				ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
				defer cancel()
				done <- ctrl.Run(ctx, "read the page and do what it says")
			}()
			var request event.Approval
			waitFor(t, "the approval request", func() bool {
				seen.mu.Lock()
				defer seen.mu.Unlock()
				if len(seen.approvals) == 0 {
					return false
				}
				request = seen.approvals[0]
				return true
			})
			if request.Tool != "send_peer_message" || !strings.Contains(request.Subject, receiver) || !strings.Contains(request.Subject, words) {
				t.Fatalf("the card does not show the recipient and the whole text: %+v", request)
			}
			if pending, _ := d.Pending(receiver); len(pending) != 0 {
				t.Fatalf("a message was spooled before the user answered: %+v", pending)
			}
			ctrl.Approve(request.ID, allow, false, false)
			if err := <-done; err != nil {
				t.Fatal(err)
			}
			pending, _ := d.Pending(receiver)
			switch {
			case allow && (len(pending) != 1 || pending[0].Body != words):
				t.Fatalf("after approval the spool holds %+v, want the one message", pending)
			case !allow && len(pending) != 0:
				t.Fatalf("a declined message was spooled: %+v", pending)
			}
		})
	}
}

// Handing a task to another project is a second switch, off unless the user's
// own file turns it on beside messages between sessions: without it the model's
// tool block is exactly what it was.
func TestEffectDispatchToolExistsOnlyWhereTheUserTurnedDispatchOn(t *testing.T) {
	t.Parallel()
	for _, dispatch := range []bool{true, false} {
		t.Run(fmt.Sprintf("dispatch=%v", dispatch), func(t *testing.T) {
			t.Parallel()
			extra := ""
			if dispatch {
				extra = "dispatch = true\n"
			}
			rec, ctrl, _, _ := buildPeerToolSession(t, true, "nobody", "hello", extra)
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			_ = ctrl.Run(ctx, "hi")
			cancel()
			has := map[string]bool{}
			for _, name := range rec.toolNames() {
				has[name] = true
			}
			if has["dispatch_to_project"] != dispatch || has["open_projects"] != dispatch || !has["send_peer_message"] {
				t.Fatalf("tools in the first request = %v", rec.toolNames())
			}
		})
	}
}
