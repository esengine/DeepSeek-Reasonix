package planmode

import "testing"

// These tests pin the model-facing Marker prose to the actual gate: every tool
// the Marker advertises as available must pass Decide, and every tool it names
// as forbidden must be blocked. This is the anti-drift guard the centralized
// policy set out to provide — without it, the Marker text and the executable
// gate can silently diverge (the model is told it may call a tool that the gate
// then refuses, or vice versa). If you edit Marker, update these lists too.

func TestMarkerAdvertisedAllowedToolsPassDecide(t *testing.T) {
	p := Policy{}
	// Marker: "ask clarifying questions with ask, maintain planning state with
	// todo_write, and delegate isolated read-only research with read_only_task
	// or read_only_skill". The two delegations are non-built-ins that self-report
	// plan-safe, exactly as the agent supplies at the call site.
	allowed := []Call{
		{Name: "ask"},
		{Name: "todo_write"},
		{Name: "read_only_task", ReadOnly: true, Safety: PlanSafetySafe},
		{Name: "read_only_skill", ReadOnly: true, Safety: PlanSafetySafe},
	}
	for _, c := range allowed {
		if d := p.Decide(c); d.Blocked {
			t.Errorf("Marker advertises %q as available while planning, but Decide blocked it: %s", c.Name, d.Message)
		}
	}
}

func TestMarkerForbiddenToolsAreBlocked(t *testing.T) {
	p := Policy{}
	// Marker: "You must not write files, run unsafe shell commands, install
	// capabilities, mutate memory, delegate to writer-capable sub-agents or
	// skills, control long-lived processes, or mark execution steps complete."
	forbidden := []Call{
		{Name: "write_file"},     // write files
		{Name: "edit_file"},      // write files
		{Name: "multi_edit"},     // write files
		{Name: "install_source"}, // install capabilities
		{Name: "install_skill"},  // install capabilities
		{Name: "remember"},       // mutate memory
		{Name: "forget"},         // mutate memory
		{Name: "task"},           // delegate to writer-capable sub-agents
		{Name: "run_skill"},      // delegate to writer-capable skills
		{Name: "kill_shell"},     // control long-lived processes
		// "mark execution steps complete" — the key anti-drift case. complete_step
		// is pseudo-read-only (ReadOnly()==true) and self-reports unsafe; the gate
		// must still refuse it on either signal.
		{Name: "complete_step", ReadOnly: true, Safety: PlanSafetyUnsafe},
	}
	for _, c := range forbidden {
		if d := p.Decide(c); !d.Blocked {
			t.Errorf("Marker says %q is unavailable while planning, but Decide allowed it", c.Name)
		}
	}
}
