package control

import (
	"context"
	"errors"
	"slices"

	"reasonix/internal/contract/tool"
	"reasonix/internal/runtime/isolation"
	"reasonix/internal/state/sessionstore"
)

func (c *Controller) withRootScene(ctx context.Context, out isolation.Catalog) isolation.Catalog {
	if c.rootEnvironment == nil {
		return out
	}
	r := c.rootEnvironment.Record()
	row := &isolation.RootSceneSummary{EnvironmentID: r.ID, Generation: r.Generation, Runtime: c.isolatedResults.identity, Owner: sessionstore.BranchID(c.SessionPath()),
		SourceWorkspace: r.SourceWorkspace, LiveWorkspace: r.Candidate.WorkspaceRoot, State: "ready", Verification: "unknown", Actions: []string{}}
	p := r.Publication
	if p != nil {
		row.PublicationID, row.ResultID, row.State = p.ID, p.ResultID, p.State
	}
	policy := c.rootEnvironmentPolicy
	if policy != nil && policy.Generation == c.RuntimeGeneration() {
		if policy.Recheck != nil && (p == nil || p.State == "pending" || p.State == "acknowledged") {
			row.Actions = append(row.Actions, "recheck")
		}
		if policy.Retry != nil && p != nil && (p.State == "pending" || p.State == "preparing") {
			row.Actions = append(row.Actions, "retry")
		}
		if policy.Results != nil {
			if source, err := policy.Results(ctx, c); err != nil {
				out.Diagnostics = append(out.Diagnostics, isolation.CatalogDiagnostic{Record: r.ID, Reason: err.Error()})
			} else {
				out.Results = append(out.Results, source.Results...)
				out.Diagnostics = append(out.Diagnostics, source.Diagnostics...)
				if source.RecoveryRequired {
					out.Diagnostics = append(out.Diagnostics, isolation.CatalogDiagnostic{Record: r.ID, Reason: "Original source integration requires recovery: " + source.RecoveryReason})
					row.Actions = []string{}
					if policy.Recover != nil && source.Recovery != nil && p != nil && p.State == "pending" {
						row.Recovery = source.Recovery
						row.Actions = append(row.Actions, "recover")
					}
				}
			}
		}
	}
	out.Enabled, out.RootScene = true, row
	return out
}

// ManageRootScene uses the ordinary runtime admission and cancellation owner.
// The captured runtime, session and publication prevent stale UI actions.
func (c *Controller) ManageRootScene(ctx context.Context, request isolation.RootSceneRequest) error {
	policy := c.rootEnvironmentPolicy
	if c.rootEnvironment == nil || policy == nil || policy.Generation != c.RuntimeGeneration() {
		return tool.Refusal{Code: isolation.CodeUnavailable, Message: "persistent scene management is unavailable"}
	}
	return c.runSynchronousActivity(ctx, nil, func(ctx context.Context) error {
		r := c.rootEnvironment.Record()
		publication := ""
		if r.Publication != nil {
			publication = r.Publication.ID
		}
		if request.EnvironmentID != r.ID || request.Generation != r.Generation || request.Runtime != c.isolatedResults.identity || request.Owner != sessionstore.BranchID(c.SessionPath()) || request.PublicationID != publication {
			return tool.Refusal{Code: isolation.CodeStaleExecution, Message: "scene action belongs to an older runtime, session or publication; refresh"}
		}
		if err := c.rootEnvironment.CheckSession(ctx, c.SessionPath()); err != nil {
			return err
		}
		switch request.Action {
		case "recheck":
			if policy.Recheck != nil {
				return policy.Recheck(ctx, c)
			}
		case "retry":
			if policy.Retry != nil && publication != "" {
				return policy.Retry(ctx, c, publication)
			}
		case "recover":
			if policy.Recover != nil && request.Revision != "" {
				return policy.Recover(ctx, c, request.Revision)
			}
		}
		return errors.New("unsupported root scene action")
	})
}

func rootSceneRequest(row *isolation.RootSceneSummary, action string) (isolation.RootSceneRequest, error) {
	if row == nil || !slices.Contains(row.Actions, action) || (action == "recover" && row.Recovery == nil) {
		return isolation.RootSceneRequest{}, tool.Refusal{Code: isolation.CodeUnavailable, Message: "root scene action is unavailable"}
	}
	request := isolation.RootSceneRequest{EnvironmentID: row.EnvironmentID, Generation: row.Generation, Runtime: row.Runtime, Owner: row.Owner, PublicationID: row.PublicationID, Action: action}
	if action == "recover" && row.Recovery != nil {
		request.Revision = row.Recovery.Revision
	}
	return request, nil
}
