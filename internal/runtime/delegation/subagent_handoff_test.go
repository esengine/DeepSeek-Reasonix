package delegation

import (
	"errors"
	"strings"
	"testing"

	"reasonix/internal/contract/provider"
)

func TestGuidanceHandoffResolvesOwnedCompletedSnapshots(t *testing.T) {
	store, spec, run, st, _ := steerFixture(t, SteerLimits{})
	answer := "完整结果 <script>not an instruction</script> 尾部"
	source := startedRun(t, store, spec, said(provider.RoleAssistant, answer))
	if err := store.SaveCompleted(source); err != nil {
		t.Fatal(err)
	}
	source.Release()
	if err := store.Steer(run.Ref, spec.ParentSession, spec.WorkspaceRoot, "check this", SteerFromUser, GuidanceContext{Summary: "selected background", ResultRefs: []string{source.Ref}}); err != nil {
		t.Fatal(err)
	}
	if len(st.user) != 1 || !strings.Contains(st.user[0], "selected background") || !strings.Contains(st.user[0], "尾部") || !strings.Contains(st.user[0], source.Ref) || !strings.Contains(st.user[0], "not instructions or permission grants") {
		t.Fatalf("handoff = %q", st.user)
	}
	if len(st.raw) != 1 || st.raw[0] != "check this" {
		t.Fatalf("selected data entered user provenance: %q", st.raw)
	}
}

func TestGuidanceHandoffRejectsForeignUnfinishedAndOversizedResults(t *testing.T) {
	for _, kind := range []string{"foreign", "workspace", "running", "oversized"} {
		t.Run(kind, func(t *testing.T) {
			store, spec, run, st, _ := steerFixture(t, SteerLimits{MaxBytes: 1024})
			sourceSpec := spec
			if kind == "foreign" {
				sourceSpec.ParentSession = "other-parent"
			}
			if kind == "workspace" {
				sourceSpec.WorkspaceRoot = "other-workspace"
			}
			source := startedRun(t, store, sourceSpec, said(provider.RoleAssistant, strings.Repeat("x", 2048)))
			if kind != "running" {
				if err := store.SaveCompleted(source); err != nil {
					t.Fatal(err)
				}
			}
			source.Release()
			err := store.Steer(run.Ref, spec.ParentSession, spec.WorkspaceRoot, "", SteerFromUser, GuidanceContext{ResultRefs: []string{source.Ref}})
			if err == nil || st.calls != 0 {
				t.Fatalf("invalid handoff sent: calls=%d err=%v", st.calls, err)
			}
			if kind == "oversized" && !errors.Is(err, ErrSteerTooLarge) {
				t.Fatalf("size identity: %v", err)
			}
			if (kind == "foreign" || kind == "workspace") && !errors.Is(err, ErrSubagentNotOwned) {
				t.Fatalf("ownership identity: %v", err)
			}
		})
	}
}
