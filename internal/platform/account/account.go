// Package account is the client for id.reasonix.io, the identity provider the
// forum, the crash dashboard and (later) the skill registry all share.
//
// Signing in uses the device flow: the client prints a short code the person
// approves in their own browser. No embedded login form — a window without an
// address bar teaches people to type their password wherever it is asked — and
// no local callback port, which has nothing to open on an SSH or container
// host. An account is never required to run Reasonix.
package account

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"reasonix/internal/contract/provider"
)

// DefaultBaseURL is the identity provider every Reasonix service consumes.
const DefaultBaseURL = "https://id.reasonix.io"

// Poll outcomes the device flow reports in-band rather than as failures.
var (
	ErrPending      = errors.New("account: authorization pending")
	ErrSlowDown     = errors.New("account: polling too fast")
	ErrDenied       = errors.New("account: the sign-in request was denied")
	ErrExpired      = errors.New("account: the sign-in request expired")
	ErrInvalidGrant = errors.New("account: unknown or already-used device code")
	ErrUnauthorized = errors.New("account: not signed in")
)

// Error is a failure the server chose to expose: by its own contract the code
// and message carry no internal detail, so both are safe to show a user.
type Error struct {
	Status  int
	Code    string
	Message string
}

func (e *Error) Error() string {
	if e.Message != "" {
		return e.Message
	}
	return fmt.Sprintf("account: request failed (%d %s)", e.Status, e.Code)
}

// User is the signed-in identity. Role and Status are the dashboard's gate,
// not a product concept — hosts show the handle and email, nothing else.
type User struct {
	ID            int64  `json:"id"`
	Handle        string `json:"handle"`
	Email         string `json:"email"`
	EmailVerified bool   `json:"emailVerified"`
	DisplayName   string `json:"displayName"`
	Role          string `json:"role"`
	Status        string `json:"status"`
}

// Label is what to call this person on screen.
func (u User) Label() string {
	if name := strings.TrimSpace(u.DisplayName); name != "" {
		return name
	}
	return u.Handle
}

// DeviceGrant is one pending sign-in: DeviceCode is the client's secret,
// UserCode is what the person types on the approval page.
type DeviceGrant struct {
	DeviceCode              string `json:"deviceCode"`
	UserCode                string `json:"userCode"`
	VerificationURI         string `json:"verificationUri"`
	VerificationURIComplete string `json:"verificationUriComplete"`
	Interval                int    `json:"interval"`
	ExpiresIn               int    `json:"expiresIn"`
}

// Client talks to the accounts API. HTTP carries the caller's proxy and
// timeout policy; account itself has no opinion about the network.
type Client struct {
	BaseURL   string
	UserAgent string
	HTTP      *http.Client
}

// New returns a client for baseURL, falling back to the shared provider.
func New(baseURL, userAgent string, httpClient *http.Client) *Client {
	if strings.TrimSpace(baseURL) == "" {
		baseURL = DefaultBaseURL
	}
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}
	return &Client{BaseURL: strings.TrimRight(baseURL, "/"), UserAgent: userAgent, HTTP: httpClient}
}

// StartDevice opens a sign-in and returns the codes it is waiting on.
func (c *Client) StartDevice(ctx context.Context) (*DeviceGrant, error) {
	var out DeviceGrant
	if err := c.do(ctx, http.MethodPost, "/device/start", "", struct{}{}, &out); err != nil {
		return nil, err
	}
	if strings.TrimSpace(out.DeviceCode) == "" || strings.TrimSpace(out.UserCode) == "" {
		return nil, errors.New("account: sign-in service returned no device code")
	}
	if out.VerificationURI == "" {
		out.VerificationURI = c.BaseURL + "/device"
	}
	return &out, nil
}

// PollDevice asks whether the person has approved yet. The waiting states come
// back as ErrPending/ErrSlowDown so a caller can drive its own loop.
func (c *Client) PollDevice(ctx context.Context, deviceCode string) (string, *User, error) {
	var out struct {
		Status       string `json:"status"`
		SessionToken string `json:"sessionToken"`
		User         *User  `json:"user"`
	}
	err := c.do(ctx, http.MethodPost, "/device/poll", "", map[string]string{"deviceCode": deviceCode}, &out)
	if err != nil {
		var apiErr *Error
		if errors.As(err, &apiErr) {
			switch apiErr.Code {
			case "access_denied":
				return "", nil, ErrDenied
			case "expired_token":
				return "", nil, ErrExpired
			case "invalid_grant":
				return "", nil, ErrInvalidGrant
			}
		}
		return "", nil, err
	}
	switch out.Status {
	case "complete":
		if out.SessionToken == "" {
			return "", nil, errors.New("account: sign-in completed without a session token")
		}
		return out.SessionToken, out.User, nil
	case "slow_down":
		return "", nil, ErrSlowDown
	default:
		return "", nil, ErrPending
	}
}

// WaitForApproval polls until the person approves, denies, or the grant
// expires. It honours the server's interval and its slow_down hint; ctx
// cancellation (a Ctrl-C at the prompt) returns immediately.
func (c *Client) WaitForApproval(ctx context.Context, grant *DeviceGrant) (string, *User, error) {
	interval := time.Duration(max(grant.Interval, 1)) * time.Second
	deadline := time.Now().Add(time.Duration(max(grant.ExpiresIn, 60)) * time.Second)
	for {
		select {
		case <-ctx.Done():
			return "", nil, ctx.Err()
		case <-time.After(interval):
		}
		token, user, err := c.PollDevice(ctx, grant.DeviceCode)
		switch {
		case err == nil:
			return token, user, nil
		case errors.Is(err, ErrSlowDown):
			interval += 5 * time.Second
		case errors.Is(err, ErrPending):
		default:
			return "", nil, err
		}
		if time.Now().After(deadline) {
			return "", nil, ErrExpired
		}
	}
}

// Me resolves who a token belongs to. A rejected token answers
// ErrUnauthorized so a host can tell "signed out" from "service is down".
func (c *Client) Me(ctx context.Context, token string) (*User, error) {
	if strings.TrimSpace(token) == "" {
		return nil, ErrUnauthorized
	}
	var out struct {
		User *User `json:"user"`
	}
	if err := c.do(ctx, http.MethodGet, "/me", token, nil, &out); err != nil {
		var apiErr *Error
		if errors.As(err, &apiErr) && (apiErr.Status == http.StatusUnauthorized || apiErr.Status == http.StatusForbidden) {
			return nil, ErrUnauthorized
		}
		return nil, err
	}
	if out.User == nil {
		return nil, errors.New("account: identity service returned no user")
	}
	return out.User, nil
}

// Logout revokes the session server-side. A token the server already forgot is
// not an error: the caller's goal is for it to stop working.
func (c *Client) Logout(ctx context.Context, token string) error {
	if strings.TrimSpace(token) == "" {
		return nil
	}
	err := c.do(ctx, http.MethodPost, "/auth/logout", token, struct{}{}, nil)
	var apiErr *Error
	if errors.As(err, &apiErr) && (apiErr.Status == http.StatusUnauthorized || apiErr.Status == http.StatusForbidden) {
		return nil
	}
	return err
}

func (c *Client) do(ctx context.Context, method, path, token string, body, out any) error {
	return c.doLimit(ctx, method, path, token, body, out, 1<<20)
}

func (c *Client) doLimit(ctx context.Context, method, path, token string, body, out any, limit int64) error {
	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(encoded)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.BaseURL+path, reader)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if c.UserAgent != "" {
		req.Header.Set("User-Agent", c.UserAgent)
	} else {
		provider.ApplyClientIdentity(req)
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	payload, err := io.ReadAll(io.LimitReader(resp.Body, limit))
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return decodeError(resp.StatusCode, payload)
	}
	if out == nil {
		return nil
	}
	return json.Unmarshal(payload, out)
}

func decodeError(status int, payload []byte) error {
	var wire struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	_ = json.Unmarshal(payload, &wire)
	return &Error{Status: status, Code: wire.Error.Code, Message: wire.Error.Message}
}
