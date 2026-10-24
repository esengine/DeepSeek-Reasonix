//go:build darwin || linux

package agent

import (
	"fmt"
	"os"
	"syscall"
	"testing"

	"reasonix/internal/safety/evidence"
)

func init() {
	if os.Getenv("REASONIX_TEST_BASELINE_SIGNAL_HELPER") != "1" {
		return
	}
	fmt.Println(`{"Action":"pass","Test":"TestEvict"}`)
	_ = syscall.Kill(os.Getpid(), syscall.SIGKILL)
	os.Exit(2)
}

func TestSignaledBaselineCannotSettleEarlierPartialPass(t *testing.T) {
	installBaselineTestCommand(t)
	t.Setenv("REASONIX_TEST_BASELINE_SIGNAL_HELPER", "1")
	a := baselineWorkspace(t, "package spike\nfunc Evict(n int) int { return n-1 }\n", assertsNothing)
	a.evaluateBaselineCriteriaOnce(t.Context())
	facts := a.baselineFacts()
	if len(facts) != 1 || facts[0].Evaluation == nil || facts[0].Evaluation.Result != evidence.CriterionUnavailable {
		t.Fatalf("signal death settled captured criterion: %+v", facts)
	}
	if len(evidence.BaselineTestObligations(facts, a.mutationEpoch())) != 1 {
		t.Fatal("signal death cleared obligation")
	}
}
