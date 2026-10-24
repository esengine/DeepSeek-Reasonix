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

type RootSceneAPI interface {
	ManageRootScene(context.Context, isolation.RootSceneRequest) error
}

func writeRootSceneSummary(text *strings.Builder, root *isolation.RootSceneSummary) {
	if root == nil {
		return
	}
	fmt.Fprintf(text, "Persistent scene %s (%s)\n  live: %s\n  source: %s\n  publication: %s result: %s verification=%s\nUse /isolated root recheck to verify and integrate, or /isolated root retry to retain the old export and start a new one.\n", root.EnvironmentID, root.State, root.LiveWorkspace, root.SourceWorkspace, root.PublicationID, root.ResultID, root.Verification)
}

func rootSceneCommand(ctx context.Context, api IsolationResultsAPI, args []string) (string, error) {
	owner, ok := api.(RootSceneAPI)
	if !ok {
		return "", tool.Refusal{Code: isolation.CodeUnavailable, Message: "root scene management is unavailable"}
	}
	if len(args) == 3 && args[0] == "--request" && args[1] == "v1" {
		raw, err := base64.RawURLEncoding.DecodeString(args[2])
		var request isolation.RootSceneRequest
		if err != nil || len(raw) > 64<<10 || json.Unmarshal(raw, &request) != nil {
			return "", tool.Refusal{Code: isolation.CodeStaleExecution, Message: "invalid scene request; refresh"}
		}
		err = owner.ManageRootScene(ctx, request)
		return "Persistent scene action completed. Refresh the retained results for verification and integration status.", err
	}
	if len(args) == 1 && (args[0] == "recheck" || args[0] == "retry" || args[0] == "recover") {
		catalog, err := api.IsolationResults(ctx)
		if err != nil {
			return "", err
		}
		request, err := rootSceneRequest(catalog.RootScene, args[0])
		if err != nil {
			return "", err
		}
		if args[0] == "recover" {
			raw, err := json.Marshal(request)
			if err != nil {
				return "", err
			}
			return "Source recovery preview: " + catalog.RootScene.SourceWorkspace + "\n" + strings.Join(catalog.RootScene.Recovery.Paths, "\n") + "\nOnly unchanged transaction postimages can be restored. Confirm this exact preview:\n/isolated root --request v1 " + base64.RawURLEncoding.EncodeToString(raw), nil
		}
		return "Persistent scene action completed. The live worktree is retained.", owner.ManageRootScene(ctx, request)
	}
	return "Usage: /isolated root recheck | root retry | root recover", nil
}
