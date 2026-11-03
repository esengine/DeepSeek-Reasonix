package agent

import (
	"testing"
)

func TestAManualFoldAboveTheTriggerIsAcceptedWhenItFreesAnything(t *testing.T) {
	a := &Agent{agentConfig: agentConfig{contextWindow: 200_000, compactRatio: 0.8}}
	w := a.window()
	trigger := w.compactTrigger()
	source, candidate := trigger+20_000, trigger+5_000
	if err := w.acceptCheckpointCandidate(CompactionTriggerManual, compactionScope{}, source, candidate, 1_000); err != nil {
		t.Fatalf("a manual fold that frees %d tokens above the trigger was refused: %v", source-candidate, err)
	}
	if err := w.acceptCheckpointCandidate(CompactionTriggerManual, compactionScope{}, source, source, 1_000); err == nil {
		t.Fatal("a manual fold that frees nothing must still be refused")
	}
}

func TestAnAutomaticFoldAboveTheTriggerStillHasToLandBelowIt(t *testing.T) {
	a := &Agent{agentConfig: agentConfig{contextWindow: 200_000, compactRatio: 0.8}}
	w := a.window()
	trigger := w.compactTrigger()
	if err := w.acceptCheckpointCandidate(CompactionTriggerPressure, compactionScope{}, trigger+20_000, trigger+5_000, 1_000); err == nil {
		t.Fatal("an automatic fold that stays above the trigger buys the next fold and must be refused")
	}
}
