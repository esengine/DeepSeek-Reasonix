package market

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"
)

const maxVoteBody = 4 << 10

var (
	// ErrOwnPackage: publishers do not vote on their own packages.
	ErrOwnPackage = errors.New("market: cannot vote on your own package")
	// ErrBadVote: a vote other than +1, -1 or 0.
	ErrBadVote = errors.New("market: a vote is +1, -1 or 0")
)

// Vote is the caller's vote on one package and the tally it leaves.
type Vote struct {
	Value         int      `json:"value"`
	UpCount       int      `json:"upCount"`
	DownCount     int      `json:"downCount"`
	ApprovalRate  *float64 `json:"approvalRate"`
	CanVote       bool     `json:"canVote"`
	Own           bool     `json:"own"`
	EmailVerified bool     `json:"emailVerified"`
}

// Voting is what the registry offers a signed-in account.
type Voting interface {
	MyVote(ctx context.Context, token, slug string) (Vote, error)
	Vote(ctx context.Context, token, slug string, value int) (Vote, error)
}

// InstallReporter tells the registry one install landed.
type InstallReporter interface {
	ReportInstall(ctx context.Context, slug, installKey string) error
}

func votePath(slug, leaf string) (string, error) {
	handle, name, err := SplitSlug(slug)
	if err != nil {
		return "", err
	}
	return "/v1/packages/" + url.PathEscape(handle) + "/" + url.PathEscape(name) + "/" + leaf, nil
}

func (c *Client) MyVote(ctx context.Context, token, slug string) (Vote, error) {
	if strings.TrimSpace(token) == "" {
		return Vote{}, ErrSignedOut
	}
	path, err := votePath(slug, "vote")
	if err != nil {
		return Vote{}, err
	}
	var v Vote
	err = c.authed(ctx, http.MethodGet, path, token, nil, maxVoteBody, &v)
	return v, err
}

func (c *Client) Vote(ctx context.Context, token, slug string, value int) (Vote, error) {
	if value < -1 || value > 1 {
		return Vote{}, ErrBadVote
	}
	if strings.TrimSpace(token) == "" {
		return Vote{}, ErrSignedOut
	}
	path, err := votePath(slug, "vote")
	if err != nil {
		return Vote{}, err
	}
	payload, _ := json.Marshal(map[string]int{"value": value})
	var v Vote
	err = c.authed(ctx, http.MethodPost, path, token, payload, maxVoteBody, &v)
	return v, err
}

// ReportInstall sends the package slug and an anonymous key, nothing else:
// no account token, no content, no version.
func (c *Client) ReportInstall(ctx context.Context, slug, installKey string) error {
	path, err := votePath(slug, "installed")
	if err != nil {
		return err
	}
	payload, _ := json.Marshal(map[string]string{"installId": installKey})
	resp, body, err := c.send(ctx, http.MethodPost, path, nil, "", payload, maxVoteBody)
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return registryRefusal(resp.StatusCode, body)
	}
	return nil
}
