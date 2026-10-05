package market

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
)

const (
	maxPublishBody = 64 << 10
	maxMineBody    = 1 << 20
	maxTags        = 8
)

var (
	// ErrSignedOut: no account session, or the registry no longer accepts it.
	ErrSignedOut = errors.New("market: not signed in")
	// ErrEmailUnverified: the account has not verified its email address.
	ErrEmailUnverified = errors.New("market: account email is not verified")
	// ErrNotOwner: the name is already published by another account.
	ErrNotOwner = errors.New("market: name belongs to another publisher")
	// ErrVersionExists: that version of the package is already published.
	ErrVersionExists = errors.New("market: version already published")
	// ErrRejected: the registry refused the submission's fields.
	ErrRejected = errors.New("market: submission refused")
	// ErrUnpublishable: a kind or source this client would never install.
	ErrUnpublishable = errors.New("market: not installable from the market")
	// ErrRateLimited: the registry asked the caller to slow down.
	ErrRateLimited = errors.New("market: too many submissions")
)

// RejectedError is a refusal the registry explained. Its message is the
// registry's client-safe text, shown as the detail of ErrRejected.
type RejectedError struct{ Message string }

func (e *RejectedError) Error() string { return "market: submission refused: " + e.Message }
func (e *RejectedError) Unwrap() error { return ErrRejected }

// Submission is one package a signed-in publisher offers for review. The
// registry files it under the account's handle and holds it until approved.
type Submission struct {
	Kind        string   `json:"kind"`
	Name        string   `json:"name"`
	Source      string   `json:"source"`
	Summary     string   `json:"summary,omitempty"`
	Description string   `json:"description,omitempty"`
	RepoURL     string   `json:"repoUrl,omitempty"`
	Version     string   `json:"version,omitempty"`
	Tags        []string `json:"tags,omitempty"`
	// Visibility "private" keeps the package to its publisher, out of review.
	Visibility string `json:"visibility,omitempty"`
}

// Published is the registry's receipt for a submission.
type Published struct {
	Package Package `json:"package"`
	Created bool    `json:"created"`
	Version string  `json:"version"`
}

// Publisher is what a caller needs to submit and track its own packages.
type Publisher interface {
	Publish(ctx context.Context, token string, s Submission) (Published, error)
	Mine(ctx context.Context, token string) ([]Package, error)
	Owner
}

// Installer names the install_source kind a listing kind installs through.
// A theme ships inside a plugin package.
func Installer(kind string) string {
	if kind == "theme" {
		return "plugin"
	}
	return kind
}

// Normalize trims the submission and refuses what the market could never
// install: a submission is only worth reviewing if its approved version can
// pass installable, so plugin and theme sources must already name a commit.
func (s Submission) Normalize() (Submission, error) {
	s.Kind = strings.TrimSpace(s.Kind)
	s.Name = strings.ToLower(strings.TrimSpace(s.Name))
	s.Source = strings.TrimSpace(s.Source)
	s.Summary = strings.TrimSpace(s.Summary)
	s.Description = strings.TrimSpace(s.Description)
	s.RepoURL = strings.TrimSpace(s.RepoURL)
	s.Version = strings.TrimSpace(s.Version)
	tags := make([]string, 0, len(s.Tags))
	for _, t := range s.Tags {
		if t = strings.TrimSpace(t); t != "" {
			tags = append(tags, t)
		}
	}
	s.Tags = tags
	// Public is the registry's default and goes unsaid, so an ordinary
	// submission stays readable by a registry that predates the field.
	switch s.Visibility = strings.TrimSpace(s.Visibility); s.Visibility {
	case "", "private":
	case "public":
		s.Visibility = ""
	default:
		return s, &RejectedError{Message: "visibility: public or private"}
	}
	if len(s.Tags) > maxTags {
		return s, &RejectedError{Message: fmt.Sprintf("tags: at most %d", maxTags)}
	}
	if !slugPart(s.Name) {
		return s, &RejectedError{Message: "name: use 1–64 chars: letters, digits, '.', '_', '-'"}
	}
	if err := sourceInstallable(s.Kind, s.Source); err != nil {
		return s, fmt.Errorf("%w: %w", ErrUnpublishable, err)
	}
	return s, nil
}

// Publish submits s for review under the account token belongs to.
func (c *Client) Publish(ctx context.Context, token string, s Submission) (Published, error) {
	if strings.TrimSpace(token) == "" {
		return Published{}, ErrSignedOut
	}
	s, err := s.Normalize()
	if err != nil {
		return Published{}, err
	}
	payload, _ := json.Marshal(s)
	var out Published
	if err := c.authed(ctx, http.MethodPost, "/v1/packages", token, payload, maxPublishBody, &out); err != nil {
		return Published{}, err
	}
	return out, nil
}

// Mine lists every package the account has submitted, in every review state.
func (c *Client) Mine(ctx context.Context, token string) ([]Package, error) {
	if strings.TrimSpace(token) == "" {
		return nil, ErrSignedOut
	}
	var out struct {
		Packages []Package `json:"packages"`
	}
	if err := c.authed(ctx, http.MethodGet, "/v1/me/packages", token, nil, maxMineBody, &out); err != nil {
		return nil, err
	}
	if out.Packages == nil {
		out.Packages = []Package{}
	}
	return out.Packages, nil
}

func (c *Client) authed(ctx context.Context, method, path, token string, payload []byte, limit int64, into any) error {
	resp, body, err := c.send(ctx, method, path, nil, strings.TrimSpace(token), payload, limit)
	if err != nil {
		return err
	}
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return decode(body, into)
	}
	return registryRefusal(resp.StatusCode, body)
}

// registryRefusal projects the registry's own error code; the status alone
// cannot tell an unverified email from a name owned by someone else.
func registryRefusal(status int, body []byte) error {
	var wire struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	_ = json.Unmarshal(body, &wire)
	switch code := wire.Error.Code; {
	case code == "accounts_unavailable":
		return fmt.Errorf("%w: the identity service did not answer", ErrUnreachable)
	case status == http.StatusUnauthorized:
		return ErrSignedOut
	case status == http.StatusNotFound:
		return ErrNotFound
	case code == "not_private":
		return ErrNotPrivate
	case code == "email_unverified":
		return ErrEmailUnverified
	case code == "not_owner":
		return ErrNotOwner
	case code == "own_package":
		return ErrOwnPackage
	case code == "version_exists":
		return ErrVersionExists
	case status == http.StatusTooManyRequests:
		return ErrRateLimited
	case code == "invalid_input" || code == "invalid_json":
		return &RejectedError{Message: clip(wire.Error.Message, 300)}
	}
	return fmt.Errorf("%w: HTTP %d %s", ErrBadResponse, status, clip(wire.Error.Code, 60))
}

func clip(s string, n int) string {
	s = strings.TrimSpace(s)
	if r := []rune(s); len(r) > n {
		return string(r[:n]) + "…"
	}
	return s
}
