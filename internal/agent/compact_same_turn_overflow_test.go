package agent

import (
	"context"
	"errors"
	"strings"
	"testing"

	"reasonix/internal/provider"
)

func TestHardCeilingAllowsRepeatedSameTurnProgress(t *testing.T) {
	sess := foldableSessionOverForce(6)
	a := agentOverForce(t, &fakeProvider{reply: "digest"}, sess)
	a.activeTurnCreatedAt.Store(42)

	if err := prepareContext(context.Background(), a, CompactionTriggerPressure); err != nil {
		t.Fatalf("first pressure fold: %v", err)
	}
	version := a.currentProjectionVersion()
	if version == 0 || a.sess.compaction.lastTurn.Load() != 42 {
		t.Fatalf("first fold version=%d lastTurn=%d", version, a.sess.compaction.lastTurn.Load())
	}

	big := strings.Repeat("word ", 400)
	for i := 0; i < 100 && a.estimatedVisibleRequestTokens(a.modelVisibleMessages()) < a.hardInputCeiling(); i++ {
		sess.Add(provider.Message{Role: provider.RoleAssistant, Content: big})
		sess.Add(provider.Message{Role: provider.RoleUser, Content: "continue"})
	}
	if est, hard := a.estimatedVisibleRequestTokens(a.modelVisibleMessages()), a.hardInputCeiling(); est < hard {
		t.Fatalf("fixture did not reach hard ceiling: %d < %d", est, hard)
	}

	if err := prepareContext(context.Background(), a, CompactionTriggerOverflow); err != nil {
		t.Fatalf("same-turn overflow recovery: %v", err)
	}
	if got := a.currentProjectionVersion(); got == version {
		t.Fatalf("same-turn overflow kept projection version %d; recovery did not run", got)
	}
	recoveryVersion := a.currentProjectionVersion()

	for i := 0; i < 100 && a.estimatedVisibleRequestTokens(a.modelVisibleMessages()) < a.hardInputCeiling(); i++ {
		sess.Add(provider.Message{Role: provider.RoleAssistant, Content: big})
		sess.Add(provider.Message{Role: provider.RoleUser, Content: "continue"})
	}
	if err := prepareContext(context.Background(), a, CompactionTriggerOverflow); err != nil {
		t.Fatalf("second same-turn overflow recovery: %v", err)
	}
	secondRecoveryVersion := a.currentProjectionVersion()
	if secondRecoveryVersion == recoveryVersion {
		t.Fatalf("second same-turn recovery kept projection version %d", recoveryVersion)
	}

	// The same view cannot pay for another summary. New provider-visible input,
	// not merely another overflow signal, is what permits same-turn recovery.
	err := prepareContext(context.Background(), a, CompactionTriggerOverflow)
	if !errors.Is(err, ErrCompactionRequired) {
		t.Fatalf("unchanged-view recovery error = %v, want ErrCompactionRequired", err)
	}
	if got := a.currentProjectionVersion(); got != secondRecoveryVersion {
		t.Fatalf("unchanged-view recovery advanced projection version to %d, want %d", got, secondRecoveryVersion)
	}
}
