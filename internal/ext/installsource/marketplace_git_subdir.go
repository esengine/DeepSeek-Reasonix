package installsource

import (
	"context"
	"fmt"
	"net/url"
	"path/filepath"
	"strings"

	"reasonix/internal/platform/gitcmd"
)

func (t *Tool) marketplaceGitSubdir(ctx context.Context, source claudeMarketplaceURLSource) (root, sourceURL, commit string, cleanup func(), err error) {
	repoURL := strings.TrimSpace(source.URL)
	src, ok := parseGitHubRepoSource(repoURL)
	if !ok || src.Branch != "" || src.Path != "" {
		return "", "", "", nil, newErr(ErrInvalidManifest, "git-subdir url must identify a GitHub repository")
	}
	path := strings.TrimSpace(source.Path)
	if path == "" || strings.Contains(path, "\\") || !filepath.IsLocal(filepath.FromSlash(path)) {
		return "", "", "", nil, newErr(ErrInvalidManifest, "git-subdir path must be a relative directory inside the repository")
	}
	path = filepath.ToSlash(filepath.Clean(filepath.FromSlash(path)))
	pathURL := &url.URL{Scheme: "https", Host: "github.com", Path: fmt.Sprintf("/%s/%s/tree/main/%s", src.Owner, src.Repo, path)}
	if roundTrip, ok := parseGitHubRepoSource(pathURL.String()); !ok || roundTrip.Path != path {
		return "", "", "", nil, newErr(ErrInvalidManifest, "git-subdir path cannot be represented as a GitHub tree URL")
	}
	sha := strings.TrimSpace(source.SHA)
	if sha != "" && !fullGitSHA.MatchString(sha) {
		return "", "", "", nil, newErr(ErrInvalidManifest, "git-subdir sha must be a full 40-character commit SHA")
	}
	if !gitcmd.Available() {
		return "", "", "", nil, newErr(ErrBinaryMissing, "git-subdir sources require Git to resolve and fetch the approved commit")
	}
	ref := strings.TrimSpace(source.Ref)
	if ref != "" {
		check := pluginGitCommand(ctx, "check-ref-format", "--allow-onelevel", ref)
		if strings.HasPrefix(ref, "-") || check.Run() != nil {
			return "", "", "", nil, newErr(ErrInvalidManifest, "git-subdir ref is not a valid Git ref")
		}
	}
	cloneRoot, commit, release, err := t.pluginSource(ctx, repoURL, "copy")
	if err != nil {
		return "", "", "", nil, err
	}
	defer func() {
		if err != nil {
			release()
		}
	}()
	if sha != "" {
		if !strings.EqualFold(commit, sha) {
			if err = checkoutPluginCommit(ctx, cloneRoot, sha); err != nil {
				return "", "", "", nil, newErr(ErrSourceUnreadable, "%v", err)
			}
		}
		commit = strings.ToLower(sha)
	} else if ref != "" {
		commit, err = checkoutMarketplaceRef(ctx, cloneRoot, ref)
		if err != nil {
			return "", "", "", nil, err
		}
	}
	if !fullGitSHA.MatchString(commit) {
		return "", "", "", nil, newErr(ErrSourceUnreadable, "git-subdir source did not resolve to a commit SHA")
	}
	root, err = pluginRootFromClone(cloneRoot, path)
	if err != nil {
		return "", "", "", nil, err
	}
	pathURL.Path = fmt.Sprintf("/%s/%s/tree/%s/%s", src.Owner, src.Repo, strings.ToLower(commit), path)
	sourceURL = pathURL.String()
	return root, sourceURL, strings.ToLower(commit), release, nil
}

func checkoutMarketplaceRef(ctx context.Context, root, ref string) (string, error) {
	fetch := pluginGitCommand(ctx, "-C", root, "fetch", "--depth=1", "--", "origin", ref)
	if out, err := fetch.CombinedOutput(); err != nil {
		return "", newErr(ErrSourceUnreadable, "fetch marketplace ref %s: %v: %s", ref, err, strings.TrimSpace(string(out)))
	}
	checkout := pluginGitCommand(ctx, "-C", root, "checkout", "--detach", "FETCH_HEAD")
	if out, err := checkout.CombinedOutput(); err != nil {
		return "", newErr(ErrSourceUnreadable, "checkout marketplace ref %s: %v: %s", ref, err, strings.TrimSpace(string(out)))
	}
	rev := pluginGitCommand(ctx, "-C", root, "rev-parse", "HEAD")
	out, err := rev.Output()
	if err != nil {
		return "", newErr(ErrSourceUnreadable, "resolve marketplace ref %s: %v", ref, err)
	}
	return strings.TrimSpace(string(out)), nil
}
