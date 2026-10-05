package market

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const (
	registryHost    = "crash.reasonix.io"
	requestTimeout  = 15 * time.Second
	maxListBody     = 1 << 20
	maxDetailBody   = 512 << 10
	defaultPageSize = 24
)

var (
	// ErrUnreachable: the registry could not be reached at all.
	ErrUnreachable = errors.New("market: registry unreachable")
	// ErrBadResponse: the registry answered with something other than the
	// documented JSON — a non-2xx status, a redirect, or an oversized body.
	ErrBadResponse = errors.New("market: registry answered unexpectedly")
	// ErrNotFound: no package the caller may read has that slug.
	ErrNotFound = errors.New("market: no such package")
	// ErrBadSlug: the slug is not <handle>/<name>.
	ErrBadSlug = errors.New("market: not a package slug")
	// ErrFilterUnsupported: an installable-only listing was asked for and the
	// registry answered without applying that filter.
	ErrFilterUnsupported = errors.New("market: registry cannot filter to installable packages")
)

var slugSegment = regexp.MustCompile(`^[A-Za-z0-9._-]{1,64}$`)

func slugPart(s string) bool { return slugSegment.MatchString(s) && s != "." && s != ".." }

// Package is one listed capability as the registry describes it. Every field is
// the publisher's claim; none of it decides what gets installed.
type Package struct {
	Kind          string   `json:"kind"`
	Handle        string   `json:"handle"`
	Name          string   `json:"name"`
	Slug          string   `json:"slug"`
	Summary       string   `json:"summary"`
	Description   string   `json:"description"`
	Homepage      string   `json:"homepage"`
	RepoURL       string   `json:"repoUrl"`
	Tags          []string `json:"tags"`
	LatestVersion string   `json:"latestVersion"`
	InstallCount  int      `json:"installCount"`
	StarCount     int      `json:"starCount"`
	UpCount       int      `json:"upCount"`
	DownCount     int      `json:"downCount"`
	// Nil while nobody has voted, which is not the same as zero approval.
	ApprovalRate *float64 `json:"approvalRate"`
	Score        float64  `json:"score"`
	Verified     bool     `json:"verified"`
	Status       string   `json:"status"`
	UpdatedAt    string   `json:"updatedAt"`
	// Pinned is the registry's word that the latest version carries a reviewed
	// digest; nil when it did not say. Install still checks the digest itself.
	Pinned *bool `json:"pinned,omitempty"`
}

// Version is one immutable published version row.
type Version struct {
	Version     string `json:"version"`
	Source      string `json:"source"`
	ContentHash string `json:"contentHash"`
	RiskLevel   string `json:"riskLevel"`
	CreatedAt   string `json:"createdAt"`
}

// Detail is a package with its approved version, nil when the registry holds
// no version row for the one a reviewer let through.
type Detail struct {
	Package  Package  `json:"package"`
	Approved *Version `json:"approved"`
}

// Query narrows a listing. Unknown kinds and sorts are the registry's to
// refuse; this side only bounds the numbers.
type Query struct {
	Kind   string
	Q      string
	Sort   string
	Offset int
	// Pinned asks the registry for installable packages only. It filters
	// server-side so pages stay whole.
	Pinned bool
}

// Page is one listing page.
type Page struct {
	Packages []Package `json:"packages"`
	Limit    int       `json:"limit"`
	Offset   int       `json:"offset"`
}

// Registry is what a caller needs from the community registry.
type Registry interface {
	List(ctx context.Context, q Query) (Page, error)
	Detail(ctx context.Context, slug string) (Detail, error)
}

// Client talks to the one registry host. Nothing about the host comes from
// configuration: a listing that can be pointed elsewhere is a listing anyone
// with write access to a settings file can rewrite.
type Client struct {
	http *http.Client
	base url.URL
}

// NewClient wraps hc (the caller's proxy-aware client) for the registry.
func NewClient(hc *http.Client) *Client {
	return newClient(url.URL{Scheme: "https", Host: registryHost}, hc)
}

func newClient(base url.URL, hc *http.Client) *Client {
	if hc == nil {
		hc = &http.Client{}
	}
	c := *hc
	c.Timeout = requestTimeout
	c.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &Client{http: &c, base: base}
}

// SplitSlug validates a <handle>/<name> slug.
func SplitSlug(slug string) (string, string, error) {
	handle, name, ok := strings.Cut(strings.TrimSpace(slug), "/")
	if !ok || !slugPart(handle) || !slugPart(name) {
		return "", "", fmt.Errorf("%w: %q", ErrBadSlug, slug)
	}
	return handle, name, nil
}

func (c *Client) List(ctx context.Context, q Query) (Page, error) {
	v := url.Values{}
	if k := strings.TrimSpace(q.Kind); k != "" {
		v.Set("kind", k)
	}
	if s := strings.TrimSpace(q.Q); s != "" {
		v.Set("q", s)
	}
	if s := strings.TrimSpace(q.Sort); s != "" {
		v.Set("sort", s)
	}
	if q.Pinned {
		v.Set("pinned", "1")
	}
	v.Set("limit", strconv.Itoa(defaultPageSize))
	v.Set("offset", strconv.Itoa(max(0, min(q.Offset, 10000))))
	var page Page
	if err := c.get(ctx, "/v1/packages", v, maxListBody, &page); err != nil {
		return Page{}, err
	}
	kept := page.Packages[:0]
	for _, p := range page.Packages {
		if q.Pinned && (p.Pinned == nil || !*p.Pinned) {
			return Page{}, ErrFilterUnsupported
		}
		if p.Status == "active" {
			kept = append(kept, p)
		}
	}
	page.Packages = kept
	return page, nil
}

func (c *Client) Detail(ctx context.Context, slug string) (Detail, error) {
	handle, name, err := SplitSlug(slug)
	if err != nil {
		return Detail{}, err
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
	path := "/v1/packages/" + url.PathEscape(handle) + "/" + url.PathEscape(name)
	if err := c.get(ctx, path, nil, maxDetailBody, &raw); err != nil {
		return Detail{}, err
	}
	if raw.Package.Status != "active" || raw.Package.Slug != handle+"/"+name {
		return Detail{}, ErrNotFound
	}
	out := Detail{Package: raw.Package}
	// The registry serves a package only while it is active, and approval fixes
	// latestVersion to the reviewed row, so that row is the approved one.
	for _, v := range raw.Versions {
		if v.Version == raw.Package.LatestVersion {
			out.Approved = &Version{Version: v.Version, Source: v.Source, ContentHash: v.ContentHash, RiskLevel: v.RiskLevel, CreatedAt: v.CreatedAt}
			break
		}
	}
	return out, nil
}

func (c *Client) get(ctx context.Context, path string, query url.Values, limit int64, into any) error {
	resp, body, err := c.send(ctx, http.MethodGet, path, query, "", nil, limit)
	if err != nil {
		return err
	}
	if resp.StatusCode == http.StatusNotFound {
		return ErrNotFound
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("%w: HTTP %d", ErrBadResponse, resp.StatusCode)
	}
	return decode(body, into)
}

// send makes one request to the fixed registry host and reads at most limit
// bytes of the answer. A token, when given, travels only on this request.
func (c *Client) send(ctx context.Context, method, path string, query url.Values, token string, payload []byte, limit int64) (*http.Response, []byte, error) {
	u := c.base
	u.Path = path
	u.RawQuery = query.Encode()
	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()
	var reader io.Reader
	if payload != nil {
		reader = bytes.NewReader(payload)
	}
	req, err := http.NewRequestWithContext(ctx, method, u.String(), reader)
	if err != nil {
		return nil, nil, fmt.Errorf("%w: %w", ErrBadResponse, err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "reasonix-market/1.0")
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, nil, fmt.Errorf("%w: %w", ErrUnreachable, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, nil, fmt.Errorf("%w: %w", ErrUnreachable, err)
	}
	if int64(len(body)) > limit {
		return nil, nil, fmt.Errorf("%w: body exceeds %d bytes", ErrBadResponse, limit)
	}
	return resp, body, nil
}

func decode(body []byte, into any) error {
	if err := json.Unmarshal(body, into); err != nil {
		return fmt.Errorf("%w: %w", ErrBadResponse, err)
	}
	return nil
}
