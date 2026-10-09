package installsource

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"reasonix/internal/base/secrets"
)

type unsupportedMarketplaceObject struct {
	message string
	warning string
	cause   error
}

func (e *unsupportedMarketplaceObject) Error() string { return e.message }
func (e *unsupportedMarketplaceObject) Unwrap() error { return e.cause }

func (t *Tool) marketplaceObjectSource(ctx context.Context, body json.RawMessage) (root, source, commit string, cleanup func(), err error) {
	var pinned claudeMarketplaceURLSource
	objectErr := json.Unmarshal(body, &pinned)
	if objectErr == nil && pinned.Source == "git-subdir" {
		root, source, commit, cleanup, err = t.marketplaceGitSubdir(ctx, pinned)
		if err != nil && ctx.Err() != nil {
			err = ctx.Err()
		}
		if err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
			err = &unsupportedMarketplaceObject{message: err.Error(), warning: err.Error(), cause: err}
		}
		return
	}
	if objectErr != nil || pinned.Source != "url" || !fullGitSHA.MatchString(strings.TrimSpace(pinned.SHA)) {
		return "", "", "", nil, &unsupportedMarketplaceObject{
			message: "object source requires source=url, a GitHub URL, and a full 40-character SHA",
			warning: "object source is not a pinned GitHub URL",
		}
	}
	if _, ok := parseGitHubRepoSource(strings.TrimSpace(pinned.URL)); !ok {
		return "", "", "", nil, &unsupportedMarketplaceObject{
			message: fmt.Sprintf("pinned URL %q is not a GitHub repository", secrets.RedactEndpoint(pinned.URL)),
			warning: "pinned URL is not a GitHub repository",
		}
	}
	var resolvedCommit string
	root, resolvedCommit, cleanup, err = t.pluginSource(ctx, pinned.URL, "copy")
	if err != nil {
		return "", "", "", nil, err
	}
	if !strings.EqualFold(resolvedCommit, pinned.SHA) {
		if err := checkoutPluginCommit(ctx, root, pinned.SHA); err != nil {
			cleanup()
			return "", "", "", nil, err
		}
	}
	return root, strings.TrimSpace(pinned.URL), strings.ToLower(strings.TrimSpace(pinned.SHA)), cleanup, nil
}
