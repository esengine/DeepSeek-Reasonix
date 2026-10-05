package boot

import (
	"context"
	"testing"

	"reasonix/internal/contract/event"
)

func TestPhaseObserverReceivesCompletedStretch(t *testing.T) {
	timer := newPhaseTimer()
	var phases []Phase
	timer.observe = func(p Phase) { phases = append(phases, p) }
	timer.mark("sessions")
	if len(phases) != 1 || phases[0].Name != "sessions" || phases[0].D < 0 {
		t.Fatalf("observed phases = %#v", phases)
	}
	if phases[0] != timer.phases[0] {
		t.Fatal("observer did not receive the recorded phase")
	}
	if got := timer.done("assemble"); len(got) != 2 || len(phases) != 2 || phases[1] != got[1] {
		t.Fatalf("final phases = %#v, observed = %#v", got, phases)
	}
}

func TestBuildReportsPhasesToHostBeforeReturning(t *testing.T) {
	isolateConfigHome(t)
	dir := robustTempDir(t)
	t.Chdir(dir)
	writeFile(t, dir, "reasonix.toml", "[environment]\nenabled = false\n")
	var phases []Phase
	ctrl, err := Build(context.Background(), Options{
		Sink:    event.Discard,
		OnPhase: func(p Phase) { phases = append(phases, p) },
	})
	if err != nil {
		t.Fatal(err)
	}
	defer ctrl.Close()
	want := []string{"extensions", "config", "migrations", "sessions", "provider", "memory", "skills", "mcp", "assemble"}
	if len(phases) != len(want) {
		t.Fatalf("phases = %#v", phases)
	}
	for i, name := range want {
		if phases[i].Name != name || phases[i].D < 0 {
			t.Fatalf("phase %d = %#v, want %s", i, phases[i], name)
		}
	}
}
