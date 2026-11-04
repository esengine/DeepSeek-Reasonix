package remotehost

import (
	"context"
	"errors"
	"testing"
	"time"

	"reasonix/internal/frontend/serve"
	"reasonix/internal/platform/remote"
)

func TestChallengeUsesCanonicalAsk(t *testing.T) {
	asked := make(chan serve.Ask, 1)
	broker := serve.NewAskBroker(func(q serve.Ask) { asked <- q })
	p := prompts{asks: broker}.build()
	if p.Challenge == nil {
		t.Fatal("challenge port missing")
	}
	ctx, cancel := context.WithTimeout(serve.WithOperationID(context.Background(), "otp-connect"), time.Second)
	defer cancel()
	input := remote.Challenge{Host: "test", Name: "Exact server title", Instruction: "Exact\ninstruction", Question: "Unclassified:", Echo: true, AnswerRequired: true}
	result := make(chan string, 1)
	failure := make(chan error, 1)
	go func() { s, err := p.Challenge(ctx, input); result <- s; failure <- err }()
	q := <-asked
	if q.Kind != "keyboard-interactive" || q.Host != input.Host || q.Name != input.Name || q.Instruction != input.Instruction || q.Question != input.Question || !q.Echo || !q.AnswerRequired || q.OperationID != "otp-connect" {
		t.Fatalf("lost challenge: %+v", q)
	}
	if err := broker.Answer(q.Epoch, q.OperationID, q.ID, serve.AskAnswer{OK: true, Text: "fresh-code"}); err != nil {
		t.Fatal(err)
	}
	if got := <-result; got != "fresh-code" {
		t.Fatalf("answer=%q", got)
	}
	if err := <-failure; err != nil {
		t.Fatal(err)
	}
	if len(broker.Pending("")) != 0 {
		t.Fatal("answered challenge remains pending")
	}
}

func TestChallengeDeclineAndCancellation(t *testing.T) {
	for _, decline := range []bool{false, true} {
		t.Run(map[bool]string{true: "decline", false: "cancel"}[decline], func(t *testing.T) {
			asked := make(chan serve.Ask, 1)
			broker := serve.NewAskBroker(func(q serve.Ask) { asked <- q })
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			result := make(chan error, 1)
			go func() {
				_, err := (prompts{asks: broker}).challenge(ctx, remote.Challenge{Host: "host", AnswerRequired: true})
				result <- err
			}()
			q := <-asked
			if decline {
				if err := broker.Answer(q.Epoch, q.OperationID, q.ID, serve.AskAnswer{}); err != nil {
					t.Fatal(err)
				}
			} else {
				cancel()
			}
			if err := <-result; !errors.Is(err, context.Canceled) {
				t.Fatalf("error=%v", err)
			}
			if len(broker.Pending("")) != 0 {
				t.Fatal("cancelled challenge remains pending")
			}
		})
	}
	if (prompts{}).build().Challenge != nil {
		t.Fatal("headless challenge prompt installed")
	}
}
