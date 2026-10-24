package agent

import (
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"testing"
	"time"

	"reasonix/internal/safety/evidence"
)

func init() {
	address := os.Getenv("REASONIX_TEST_BASELINE_CANCEL_HELPER")
	if address == "" {
		return
	}
	fmt.Println(`{"Action":"pass","Test":"TestEvict"}`)
	connection, err := net.Dial("tcp", address)
	if err != nil {
		os.Exit(2)
	}
	defer connection.Close()
	_, _ = io.Copy(io.Discard, connection)
	os.Exit(0)
}

func TestCanceledBaselineCannotSettleEarlierPartialPass(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	installBaselineTestCommand(t)
	t.Setenv("REASONIX_TEST_BASELINE_CANCEL_HELPER", listener.Addr().String())
	a := baselineWorkspace(t, "package spike\nfunc Evict(n int) int { return n - 1 }\n", assertsNothing)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	done := make(chan struct{})
	go func() { defer close(done); a.evaluateBaselineCriteriaOnce(ctx) }()
	_ = listener.(*net.TCPListener).SetDeadline(time.Now().Add(8 * time.Second))
	connection, err := listener.Accept()
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	cancel()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("canceled baseline did not terminate")
	}
	facts := a.baselineFacts()
	if len(facts) != 1 || facts[0].Evaluation == nil || facts[0].Evaluation.Result != evidence.CriterionUnavailable {
		t.Fatalf("partial pass accepted after cancellation: %+v", facts)
	}
	if len(evidence.BaselineTestObligations(facts, a.mutationEpoch())) != 1 {
		t.Fatal("cancellation cleared the captured criterion")
	}
}
