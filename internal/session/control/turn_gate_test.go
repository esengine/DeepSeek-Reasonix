package control

import (
	"context"
	"testing"
)

func TestTurnGateCancelCause(t *testing.T) {
	begin := func() *turnGate {
		g := &turnGate{}
		g.begin(func() {})
		return g
	}
	t.Run("no active turn", func(t *testing.T) {
		g := &turnGate{}
		if g.requestCancel(causeAskSkipped) != nil || g.canceling {
			t.Fatal("a cancel with no turn in flight must return nil and mark nothing")
		}
	})
	t.Run("skip alone", func(t *testing.T) {
		g := begin()
		g.requestCancel(causeAskSkipped)
		if !g.skippedAsk() {
			t.Fatal("a lone skip must read as a skip")
		}
	})
	t.Run("skip then stop is a stop", func(t *testing.T) {
		g := begin()
		g.requestCancel(causeAskSkipped)
		g.requestCancel(causeStop)
		if g.skippedAsk() || !g.canceling {
			t.Fatal("a stop after a skip must win")
		}
	})
	t.Run("stop then skip is a stop", func(t *testing.T) {
		g := begin()
		g.requestCancel(causeStop)
		g.requestCancel(causeAskSkipped)
		if g.skippedAsk() || !g.canceling {
			t.Fatal("a skip after a stop must not downgrade it")
		}
	})
	t.Run("end and begin reset the cause", func(t *testing.T) {
		g := begin()
		g.requestCancel(causeAskSkipped)
		g.end()
		if g.skippedAsk() || g.canceling {
			t.Fatal("end must clear the cancel and its cause")
		}
		g.begin(context.CancelFunc(func() {}))
		if g.skippedAsk() || g.canceling {
			t.Fatal("begin must start without a cause")
		}
	})
}
