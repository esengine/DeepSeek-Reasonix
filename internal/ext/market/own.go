package market

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"reasonix/internal/ext/installsource"
)

const maxOwnedBody = 512 << 10

var (
	// ErrNotPrivate: only a private package can be submitted for review.
	ErrNotPrivate = errors.New("market: package is not private")
	// ErrUnpreviewed: an unreviewed install must carry the digest its preview
	// showed, since no reviewer has bound one.
	ErrUnpreviewed = errors.New("market: unreviewed install without a previewed digest")
)

// OwnDetail is the publisher's own view of a package in any review state, with
// its latest version as the publisher submitted it.
type OwnDetail struct {
	Package Package  `json:"package"`
	Latest  *Version `json:"latest"`
}

// Owner reads and moves the signed-in account's own packages. The registry
// answers only for packages that account published.
type Owner interface {
	Owned(ctx context.Context, token, slug string) (OwnDetail, error)
	Submit(ctx context.Context, token, slug string) (Package, error)
}

func ownPath(slug, suffix string) (string, error) {
	handle, name, err := SplitSlug(slug)
	if err != nil {
		return "", err
	}
	return "/v1/me/packages/" + url.PathEscape(handle) + "/" + url.PathEscape(name) + suffix, nil
}

// Owned reads one of the account's own packages; another account's package is
// ErrNotFound, exactly like a missing one.
func (c *Client) Owned(ctx context.Context, token, slug string) (OwnDetail, error) {
	if strings.TrimSpace(token) == "" {
		return OwnDetail{}, ErrSignedOut
	}
	path, err := ownPath(slug, "")
	if err != nil {
		return OwnDetail{}, err
	}
	var raw struct {
		Package  Package `json:"package"`
		Versions []struct {
			Version     string `json:"version"`
			Source      string `json:"source"`
			ContentHash string `json:"content_hash"`
			RiskLevel   string `json:"risk_level"`
			CreatedAt   string `json:"created_at"`
		} `json:"versions"`
	}
	if err := c.authed(ctx, http.MethodGet, path, token, nil, maxOwnedBody, &raw); err != nil {
		return OwnDetail{}, err
	}
	if raw.Package.Slug != strings.TrimSpace(slug) {
		return OwnDetail{}, ErrNotFound
	}
	out := OwnDetail{Package: raw.Package}
	for _, v := range raw.Versions {
		if v.Version == raw.Package.LatestVersion {
			out.Latest = &Version{Version: v.Version, Source: v.Source, ContentHash: v.ContentHash, RiskLevel: v.RiskLevel, CreatedAt: v.CreatedAt}
			break
		}
	}
	return out, nil
}

// Submit moves one of the account's private packages into the review queue.
func (c *Client) Submit(ctx context.Context, token, slug string) (Package, error) {
	if strings.TrimSpace(token) == "" {
		return Package{}, ErrSignedOut
	}
	path, err := ownPath(slug, "/submit")
	if err != nil {
		return Package{}, err
	}
	var out struct {
		Package Package `json:"package"`
	}
	if err := c.authed(ctx, http.MethodPost, path, token, nil, maxPublishBody, &out); err != nil {
		return Package{}, err
	}
	return out.Package, nil
}

// PlanOwn previews the latest version of the account's own package. A live
// version keeps its reviewer's pin; any other has none, so the preview's own
// digest becomes the pin the install must match, and a source that cannot be
// pinned is refused here.
func (s *Service) PlanOwn(ctx context.Context, token string, req Request) (Outcome, error) {
	pkg, v, err := s.owned(ctx, token, req)
	if err != nil {
		return Outcome{Version: v}, err
	}
	if reviewed(pkg, v) {
		return s.execute(ctx, pkg, v, v.ContentHash, false, false, req, false)
	}
	return s.previewPinned(ctx, pkg, v, true, req, false)
}

// InstallOwn applies the plan req.PlanID names, refusing material that differs
// from the reviewer's pin or, without one, from the digest the person previewed.
func (s *Service) InstallOwn(ctx context.Context, token string, req Request) (Outcome, error) {
	pkg, v, err := s.owned(ctx, token, req)
	if err != nil {
		return Outcome{Version: v}, err
	}
	digest := strings.TrimSpace(req.Digest)
	if reviewed(pkg, v) {
		// Approved since the preview: what the person confirmed is not the pin.
		if digest != "" && digest != v.ContentHash {
			return Outcome{Version: v}, fmt.Errorf("%w: %s was approved since it was previewed", ErrVersionChanged, pkg.Slug)
		}
		return s.execute(ctx, pkg, v, v.ContentHash, false, false, req, true)
	}
	return s.previewPinned(ctx, pkg, v, true, req, true)
}

// reviewed: approval overwrites a live version's hash with the reviewer's own,
// while any other state's hash is only the publisher's claim.
func reviewed(pkg Package, v Version) bool {
	return pkg.Status == "active" && installsource.IsContentDigest(v.ContentHash)
}

// owned resolves the request against the registry's answer for this account,
// never against anything the caller sent about the package.
func (s *Service) owned(ctx context.Context, token string, req Request) (Package, Version, error) {
	if s.Owner == nil {
		return Package{}, Version{}, ErrSignedOut
	}
	d, err := s.Owner.Owned(ctx, token, req.Slug)
	if err != nil {
		return Package{}, Version{}, err
	}
	if d.Latest == nil {
		return d.Package, Version{}, fmt.Errorf("%w: no row for version %q", ErrBadSource, d.Package.LatestVersion)
	}
	v := *d.Latest
	if err := sourceInstallable(d.Package.Kind, v.Source); err != nil {
		return d.Package, v, err
	}
	if req.Version != "" && req.Version != v.Version {
		return d.Package, v, fmt.Errorf("%w: shown %s, latest now %s", ErrVersionChanged, req.Version, v.Version)
	}
	return d.Package, v, nil
}
