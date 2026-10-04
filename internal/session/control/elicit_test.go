package control

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/event"
	"reasonix/internal/contract/tool"
)

func waitAsk(t *testing.T, sink *askProbeSink) event.Ask {
	t.Helper()
	deadline := time.After(testenv.Budget(t))
	for {
		sink.mu.Lock()
		if len(sink.asks) > 0 {
			ask := sink.asks[len(sink.asks)-1]
			sink.mu.Unlock()
			return ask
		}
		sink.mu.Unlock()
		select {
		case <-deadline:
			t.Fatal("no question reached the frontend")
		case <-time.After(5 * time.Millisecond):
		}
	}
}

// A server's form reaches the person marked with who asks, and survives a
// reconnect with that mark. Refusing it answers the server and leaves the turn
// running, where an empty answer to the agent's own question ends the turn.
// The running turn is stood in by its cancel func: this proves the branch
// given a turn in flight, which is the only state a tool call runs in.
func TestAFormRefusedGoesToTheServerAndTheTurnGoesOn(t *testing.T) {
	for _, fromServer := range []bool{true, false} {
		sink := &askProbeSink{}
		c := New(Options{Sink: sink, SessionDir: testenv.TempDir(t)})
		var cancelled atomic.Bool
		c.mu.Lock()
		c.gate.begin(func() { cancelled.Store(true) })
		c.mu.Unlock()

		ctx, stop := context.WithCancel(context.Background())
		done := make(chan tool.ElicitReply, 1)
		go func() {
			if fromServer {
				reply, _ := c.Elicit(ctx, tool.ElicitRequest{Source: "deployer", Message: "Who?",
					Fields: []tool.ElicitField{{Name: "name", Title: "Name"}, {Name: "env", Choices: []string{"prod", "dev"}}}})
				done <- reply
				return
			}
			_, _ = c.Ask(ctx, askProbeQuestions())
			done <- tool.ElicitReply{}
		}()
		ask := waitAsk(t, sink)
		if fromServer {
			if o := ask.Origin; o == nil || o.Kind != event.AskOriginMCP || o.Source != "deployer" || o.Message != "Who?" {
				t.Fatalf("origin = %+v", ask.Origin)
			}
			if len(ask.Questions) != 2 || len(ask.Questions[0].Options) != 0 || len(ask.Questions[1].Options) != 2 {
				t.Fatalf("questions = %+v", ask.Questions)
			}
			if _, asks := c.approval.snapshotPrompts(); len(asks) != 1 || asks[0].Origin == nil {
				t.Fatalf("a reconnecting frontend is shown %+v", asks)
			}
		}
		c.AnswerQuestion(ask.ID, nil)
		if fromServer {
			select {
			case reply := <-done:
				if !reply.Declined {
					t.Fatalf("reply = %+v, want declined", reply)
				}
			case <-time.After(testenv.Budget(t)):
				stop()
				t.Fatal("refusing a server's form never answered the server")
			}
			if cancelled.Load() {
				t.Fatal("refusing a server's form ended the turn")
			}
		} else {
			if !cancelled.Load() {
				t.Fatal("an empty answer to the agent's own question no longer ends the turn")
			}
			stop()
			<-done
		}
		stop()
	}
}

// A receipt says a form was answered and by which fields, never with what: the
// values went to the server, and a receipt travels to every device and file.
func TestAFormsReceiptCarriesNoValues(t *testing.T) {
	origin := &event.AskOrigin{Kind: event.AskOriginMCP, Source: "deployer"}
	qs := []event.AskQuestion{{ID: "token", Prompt: "API token"}, {ID: "env", Prompt: "env"}}
	subject, outcome := formReceipt(origin, qs, []event.AskAnswer{{QuestionID: "token", Selected: []string{"sk-live-123"}}})
	if outcome != "accepted" || subject != "form from deployer: token, env" {
		t.Fatalf("receipt = %q, %q", subject, outcome)
	}
	if _, outcome := formReceipt(origin, qs, nil); outcome != "declined" {
		t.Fatalf("an empty answer recorded as %q", outcome)
	}
}
