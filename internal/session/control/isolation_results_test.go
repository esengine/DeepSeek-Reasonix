package control

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"reasonix/internal/contract/event"
	"reasonix/internal/contract/tool"
	"reasonix/internal/runtime/agent"
	"reasonix/internal/runtime/isolation"
	"reasonix/internal/state/mutation"
	"reasonix/internal/state/sessionstore"
)

type descendantRecoveryOwner struct {
	IsolatedResultsOwner
	target   *isolation.RecoveryTarget
	before   func()
	calls    int
	previews int
}

func (o *descendantRecoveryOwner) Catalog(context.Context) (isolation.Catalog, error) {
	return isolation.Catalog{Enabled: true, Results: []isolation.ResultSummary{{ID: o.target.ResultID, Recovery: o.target}}}, nil
}

func (o *descendantRecoveryOwner) PreviewResultRecovery(_ context.Context, id string) (*isolation.RecoveryTarget, error) {
	o.previews++
	if id != o.target.ResultID {
		return nil, errors.New("unregistered result")
	}
	return o.target, nil
}

func TestRecoveryBindingAdmissionDoesNotTraverseStoresUnderHostPublicationLock(t *testing.T) {
	owner := &descendantRecoveryOwner{target: &isolation.RecoveryTarget{ResultID: "iso_child", Workspace: "/candidate", Revision: "journal"}}
	c := recoveryController(t, owner, func(context.Context, []string) error { return nil })
	target := owner.target.Request()
	target.Runtime = c.isolatedResults.identity
	if err := c.PrepareIsolatedRecovery(t.Context(), target); err != nil {
		t.Fatal(err)
	}
	if owner.previews != 0 {
		t.Fatal("binding admission traversed filesystem stores while the host holds its publication lock")
	}
}

func (o *descendantRecoveryOwner) RecoverResultExpected(ctx context.Context, id, workspace, revision string, authorize isolation.SourceAuthorizer) error {
	o.calls++
	if id != o.target.ResultID || workspace != o.target.Workspace || revision != o.target.Revision {
		return errors.New("recovery binding lost")
	}
	if o.before != nil {
		o.before()
	}
	return authorize(ctx, []string{"mapped-root-path"})
}

func recoveryController(t *testing.T, owner *descendantRecoveryOwner, access func(context.Context, []string) error) *Controller {
	t.Helper()
	root := t.TempDir()
	coordinator, err := mutation.New(root, t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	exec := agent.New(nil, tool.NewRegistry(), sessionstore.NewSession("sys"), agent.Options{}, event.Discard)
	c := New(Options{Executor: exec, Runner: exec, WorkspaceRoot: root, SessionDir: filepath.Join(t.TempDir(), "sessions"),
		MutationCoordinator: coordinator, IsolatedResults: owner, IsolationRecoveryAccess: access})
	t.Cleanup(c.Close)
	return c
}

func TestDescendantRecoveryCatalogBindsRuntimeWithoutMutatingOwner(t *testing.T) {
	owner := &descendantRecoveryOwner{target: &isolation.RecoveryTarget{ResultID: "iso_child", Workspace: "/candidate", Revision: "journal"}}
	c := recoveryController(t, owner, func(context.Context, []string) error { return nil })
	catalog, err := c.IsolationResults(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if target := catalog.Results[0].Recovery; target == nil || target.Runtime != c.isolatedResults.identity {
		t.Fatalf("descendant recovery is not runtime bound: %+v", target)
	}
	if owner.target.Runtime != "" {
		t.Fatal("catalog mutated the owner's recovery snapshot")
	}
}

func TestDescendantRecoveryChecksCurrentAuthorityAfterRouting(t *testing.T) {
	for _, scenario := range []string{"allowed", "denied", "retired", "changed_preview", "foreign_workspace", "unregistered", "stale_runtime"} {
		t.Run(scenario, func(t *testing.T) {
			owner := &descendantRecoveryOwner{target: &isolation.RecoveryTarget{ResultID: "iso_child", Workspace: "/candidate", Revision: "journal"}}
			accessed := false
			c := recoveryController(t, owner, func(_ context.Context, paths []string) error {
				accessed = true
				if len(paths) != 1 || paths[0] != "mapped-root-path" {
					t.Fatalf("access checked the candidate's paths before owner projection: %v", paths)
				}
				if scenario == "denied" {
					return errors.New("access revoked")
				}
				return nil
			})
			target := owner.target.Request()
			target.Runtime = c.isolatedResults.identity
			switch scenario {
			case "retired":
				owner.before = c.Close
			case "changed_preview":
				target.Revision = "old"
			case "foreign_workspace":
				target.Workspace = "/foreign"
			case "unregistered":
				target.ResultID = "iso_foreign"
			case "stale_runtime":
				target.Runtime = "old"
			}
			err := c.RecoverIsolatedIntegration(t.Context(), target)
			if scenario == "allowed" {
				if err != nil || !accessed || owner.calls != 1 {
					t.Fatalf("registered recovery failed: %v access=%v calls=%d", err, accessed, owner.calls)
				}
			} else if err == nil {
				t.Fatalf("%s recovery was accepted", scenario)
			}
			if scenario != "allowed" && scenario != "denied" && accessed {
				t.Fatal("stale recovery reached file authorization")
			}
		})
	}
}
