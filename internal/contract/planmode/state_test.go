package planmode

import "testing"

// Every accepted move raises the epoch. Deciding case by case which transition
// "really" changes authority is the optimization that grows a stale-work hole:
// entering and submitting change which work is admissible just as approving
// does, and a uint64 is not the thing to economize on.
func TestEveryTransitionRaisesTheEpoch(t *testing.T) {
	r := NewRuntime()
	if got := r.State(); got.Phase != Inactive || got.Epoch != 0 {
		t.Fatalf("fresh runtime = %+v, want inactive at epoch 0", got)
	}
	for i, step := range []struct {
		action Action
		want   Phase
	}{
		{Enter, Planning},
		{Submit, AwaitingApproval},
		{Revise, Planning},
		{Submit, AwaitingApproval},
		{Start, Executing},
		{Exit, Inactive},
		{Enter, Planning},
	} {
		before := r.State()
		got, ok := r.Apply(step.action)
		if !ok {
			t.Fatalf("step %d: %v refused from %v", i, step.action, before.Phase)
		}
		if got.Phase != step.want {
			t.Errorf("step %d: %v led to %v, want %v", i, step.action, got.Phase, step.want)
		}
		if got.Epoch != before.Epoch+1 {
			t.Errorf("step %d: %v moved the epoch %d→%d, want +1", i, step.action, before.Epoch, got.Epoch)
		}
	}
}

// A phase only accepts the moves that lead out of it. Without this, a caller
// could reach Executing without ever passing an approval.
func TestIllegalTransitionsAreRefused(t *testing.T) {
	for _, tc := range []struct {
		from   Phase
		action Action
	}{
		{Inactive, Submit},
		{Inactive, Start},
		{Inactive, Revise},
		{Inactive, Exit},
		{Planning, Start},
		{Planning, Revise},
		{Planning, Enter},
		{AwaitingApproval, Enter},
		{AwaitingApproval, Submit},
		{Executing, Start},
		{Executing, Submit},
		{Executing, Revise},
	} {
		r := &Runtime{state: State{Phase: tc.from, Epoch: 7}}
		if got, ok := r.Apply(tc.action); ok {
			t.Errorf("%v accepted %v, reaching %v", tc.from, tc.action, got.Phase)
		}
		if got := r.State(); got.Epoch != 7 {
			t.Errorf("%v/%v moved the epoch to %d on a refusal", tc.from, tc.action, got.Epoch)
		}
	}
}

// A control-plane action carries the state it was offered on. Answering it
// after the workflow moved must do nothing at all — the click is stale, not
// merely late, because the state it was deciding about no longer exists.
func TestLateActionsFindTheirExpectedStateGone(t *testing.T) {
	t.Run("approval after revise", func(t *testing.T) {
		r := NewRuntime()
		r.Apply(Enter)
		offered, _ := r.Apply(Submit)
		if _, ok := r.Apply(Revise); !ok {
			t.Fatal("revise refused")
		}
		if got, ok := r.Transition(offered, Start); ok {
			t.Fatalf("a stale approval started execution: %+v", got)
		}
		if got := r.State(); got.Phase != Planning {
			t.Fatalf("state after the stale approval = %+v, want planning", got)
		}
	})

	t.Run("approval after exit", func(t *testing.T) {
		r := NewRuntime()
		r.Apply(Enter)
		offered, _ := r.Apply(Submit)
		r.Apply(Exit)
		if _, ok := r.Transition(offered, Start); ok {
			t.Fatal("a stale approval resurrected the workflow")
		}
		if got := r.State(); got.Active() {
			t.Fatalf("state after the stale approval = %+v, want inactive", got)
		}
	})

	t.Run("same phase, older epoch", func(t *testing.T) {
		r := NewRuntime()
		r.Apply(Enter)
		offered, _ := r.Apply(Submit)
		r.Apply(Revise)
		r.Apply(Submit)
		// Back in the phase the action expected, under a later authority.
		if got := r.State(); got.Phase != offered.Phase || got.Epoch == offered.Epoch {
			t.Fatalf("setup = %+v, want the same phase at a later epoch than %+v", got, offered)
		}
		if _, ok := r.Transition(offered, Start); ok {
			t.Fatal("phase matched but the epoch did not; the action must still be stale")
		}
	})
}

// Re-entering is a new authority, not a return to the old one: work produced in
// the workflow before it closed stays stale after it reopens.
func TestReEnteringDoesNotReuseAnEpoch(t *testing.T) {
	r := NewRuntime()
	first, _ := r.Apply(Enter)
	r.Apply(Exit)
	second, _ := r.Apply(Enter)
	if second.Phase != first.Phase {
		t.Fatalf("re-entry phase = %v, want %v", second.Phase, first.Phase)
	}
	if second.Epoch == first.Epoch {
		t.Fatalf("re-entry reused epoch %d", second.Epoch)
	}
}

func TestActiveIsDerivedFromPhase(t *testing.T) {
	for phase, want := range map[Phase]bool{
		Inactive:         false,
		Planning:         true,
		AwaitingApproval: true,
		Executing:        true,
	} {
		if got := (State{Phase: phase}).Active(); got != want {
			t.Errorf("%v active = %v, want %v", phase, got, want)
		}
	}
}

func TestNilRuntimeIsInactiveAndUnmovable(t *testing.T) {
	var r *Runtime
	if got := r.State(); got.Active() {
		t.Errorf("nil runtime state = %+v, want inactive", got)
	}
	if _, ok := r.Apply(Enter); ok {
		t.Error("nil runtime accepted a transition")
	}
	if _, ok := r.Transition(State{}, Enter); ok {
		t.Error("nil runtime accepted a compare-and-move")
	}
}
