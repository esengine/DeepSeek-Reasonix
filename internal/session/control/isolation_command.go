package control

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"

	"reasonix/internal/contract/tool"
	"reasonix/internal/runtime/isolation"
)

type IsolationResultsAPI interface {
	IsolationResults(context.Context) (isolation.Catalog, error)
	PreviewIsolatedDiscard(context.Context, string) (isolation.DiscardPreview, error)
	DiscardIsolated(context.Context, IsolatedDiscardRequest) error
	RecoverIsolatedIntegration(context.Context, isolation.RecoveryRequest) error
}

// IsolationCommandText is driven only by host-authored management input. The
// confirmation carries the same immutable preview as the graphical interface.
func IsolationCommandText(ctx context.Context, api IsolationResultsAPI, args string) (string, error) {
	fields := strings.Fields(args)
	if len(fields) > 0 && fields[0] == "root" {
		return rootSceneCommand(ctx, api, fields[1:])
	}
	if len(fields) == 0 || (len(fields) == 1 && fields[0] == "list") {
		catalog, err := api.IsolationResults(ctx)
		if err != nil {
			return "", err
		}
		if !catalog.Enabled {
			return "Isolated result management is unavailable.", nil
		}
		var text strings.Builder
		writeRootSceneSummary(&text, catalog.RootScene)
		for _, row := range catalog.Results {
			if row.CleanupComplete {
				continue
			}
			writeIsolatedResultSummary(&text, row)
		}
		for _, diagnostic := range catalog.Diagnostics {
			fmt.Fprintf(&text, "%s: %s\n", diagnostic.Record, diagnostic.Reason)
		}
		if catalog.RecoveryRequired {
			fmt.Fprintln(&text, "Source integration needs recovery. Preview with /isolated recover.")
		}
		if text.Len() == 0 {
			return "No retained isolated results need attention.", nil
		}
		return text.String(), nil
	}
	if fields[0] == "discard" {
		if len(fields) == 4 && fields[2] == "--confirm" {
			err := api.DiscardIsolated(ctx, IsolatedDiscardRequest{ID: fields[1], Revision: fields[3]})
			return "Discard completed; source workspace files were not changed.", err
		}
		if len(fields) == 2 {
			preview, err := api.PreviewIsolatedDiscard(ctx, fields[1])
			if err != nil {
				return "", err
			}
			return fmt.Sprintf("Discard preview: %s (%s), %d observed entries\n%s\nThis deletes candidate contents, including ignored files and task outputs. To confirm this exact preview:\n/isolated discard %s --confirm %s", preview.ID, preview.State, preview.Entries, preview.CandidateRoot, preview.ID, preview.Revision), nil
		}
	}
	if fields[0] == "recover" {
		if len(fields) == 3 && fields[1] == "--confirm" {
			raw, err := base64.RawURLEncoding.DecodeString(fields[2])
			var target isolation.RecoveryRequest
			if err != nil || len(raw) > 64<<10 || json.Unmarshal(raw, &target) != nil {
				return "", tool.Refusal{Code: isolation.CodeStaleExecution, Message: "invalid recovery preview; generate a new preview"}
			}
			err = api.RecoverIsolatedIntegration(ctx, target)
			return "Source journal recovery completed. Candidate results are retained.", err
		}
		if len(fields) <= 2 {
			catalog, err := api.IsolationResults(ctx)
			if err != nil {
				return "", err
			}
			target := catalog.Recovery
			if len(fields) == 2 {
				target = isolatedRowRecovery(catalog, fields[1])
			} else if !catalog.RecoveryRequired {
				return "No source integration requires recovery.", nil
			}
			if target == nil {
				return "", tool.Refusal{Code: isolation.CodeRecoveryRequired, Message: "recovery target is unavailable; journal retained: " + catalog.RecoveryReason}
			}
			raw, err := json.Marshal(target.Request())
			if err != nil {
				return "", err
			}
			return "Recovery preview: " + target.Workspace + "\n" + strings.Join(target.Paths, "\n") + "\nOnly unchanged integration postimages may be restored. To confirm this exact journal:\n/isolated recover --confirm " + base64.RawURLEncoding.EncodeToString(raw), nil
		}
	}
	return "Usage: /isolated [list] | discard <id> [--confirm <revision>] | recover [<id>] | recover --confirm <preview>", nil
}

func isolatedRowRecovery(catalog isolation.Catalog, id string) *isolation.RecoveryTarget {
	for _, row := range catalog.Results {
		if row.ID == id {
			return row.Recovery
		}
	}
	return nil
}

func writeIsolatedResultSummary(text *strings.Builder, row isolation.ResultSummary) {
	fmt.Fprintf(text, "%s  %s  %s  verification=%s\n  %s\n", row.ID, row.State, row.Activity, row.Verification, row.CandidateRoot)
	if row.ParentID != "" {
		fmt.Fprintf(text, "  parent=%s target=%s\n", row.ParentID, row.TargetWorkspace)
	}
	if row.Reason != "" {
		fmt.Fprintln(text, "  "+row.Reason)
	}
	if row.Recovery != nil {
		fmt.Fprintf(text, "  Preview recovery with /isolated recover %s\n", row.ID)
	}
}
