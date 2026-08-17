package acp

import (
	"context"
	"testing"

	"reasonix/internal/control"
)

func TestACPBeginRefusesWhileStateChangeIsInFlight(t *testing.T) {
	sess := &acpSession{id: "sess-role-switch", ctrl: control.New(control.Options{})}
	sess.stateChangeMu.Lock()
	if _, _, ok := sess.begin(context.Background()); ok {
		sess.stateChangeMu.Unlock()
		t.Fatal("begin succeeded while an in-place role switch owned stateChangeMu")
	}
	sess.stateChangeMu.Unlock()

	_, cancel, ok := sess.begin(context.Background())
	if !ok {
		t.Fatal("begin failed after the role switch released stateChangeMu")
	}
	cancel()
	sess.finish()
}
