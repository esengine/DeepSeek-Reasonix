package feedback

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"reasonix/internal/safety/redirectguard"
	"strconv"
	"strings"
	"time"
)

const maxResponseBytes = 1 << 20

var errTransientResponse = fmt.Errorf("%w: unrecognised response", ErrUnavailable)

// wireAttachment and wireSubmit are the worker's POST /v1/feedback body.
type wireAttachment struct {
	Name        string `json:"name"`
	ContentType string `json:"contentType"`
	DataBase64  string `json:"dataBase64"`
}

type wireSubmit struct {
	IdempotencyKey string           `json:"idempotencyKey"`
	InstallID      string           `json:"installId"`
	Category       Category         `json:"category"`
	Body           string           `json:"body"`
	DisplayName    string           `json:"displayName"`
	Contact        string           `json:"contact,omitempty"`
	Env            Env              `json:"env"`
	Attachments    []wireAttachment `json:"attachments,omitempty"`
	TurnstileToken string           `json:"turnstileToken,omitempty"`
}

type wireReceipt struct {
	Receipt      string    `json:"receipt"`
	Status       Status    `json:"status"`
	InstallToken string    `json:"installToken"`
	CreatedAt    time.Time `json:"createdAt"`
}

type wireError struct {
	Error struct {
		Code string `json:"code"`
	} `json:"error"`
}

var codeSentinels = map[string]error{
	"feedback.too_large":          ErrTooLarge,
	"feedback.rate_limited":       ErrRateLimited,
	"feedback.invalid":            ErrInvalid,
	"feedback.disabled":           ErrDisabled,
	"feedback.duplicate":          ErrDuplicate,
	"feedback.bad_token":          ErrBadToken,
	"feedback.busy":               ErrBusy,
	"feedback.image_metadata":     ErrImageMetadata,
	"feedback.reply_limit":        ErrReplyLimit,
	"feedback.not_replyable":      ErrNotReplyable,
	"feedback.challenge_required": ErrChallengeRequired,
}

func (s *Service) post(ctx context.Context, body wireSubmit, token string) (wireReceipt, error) {
	raw, err := json.Marshal(body)
	if err != nil {
		return wireReceipt{}, err
	}
	var out wireReceipt
	err = s.withRetry(ctx, func() error {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.base+"/v1/feedback", bytes.NewReader(raw))
		if err != nil {
			return err
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Install-Id", body.InstallID)
		if token != "" {
			req.Header.Set("X-Install-Token", token)
		}
		return s.do(req, &out)
	}, true)
	return out, err
}

func (s *Service) get(ctx context.Context, id, token string, out any) error {
	return s.withRetry(ctx, func() error {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.base+"/v1/feedback/mine", nil)
		if err != nil {
			return err
		}
		req.Header.Set("X-Install-Id", id)
		req.Header.Set("X-Install-Token", token)
		return s.do(req, out)
	}, false)
}

// postReply sends one reply. It is never retried: replies carry no idempotency
// key, so a request that may have arrived must not be sent twice.
func (s *Service) postReply(ctx context.Context, id, token, receipt, body string) (ReplyReceipt, error) {
	raw, err := json.Marshal(struct {
		Body string `json:"body"`
	}{body})
	if err != nil {
		return ReplyReceipt{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.base+"/v1/feedback/"+url.PathEscape(receipt)+"/reply", bytes.NewReader(raw))
	if err != nil {
		return ReplyReceipt{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Install-Id", id)
	req.Header.Set("X-Install-Token", token)
	var out ReplyReceipt
	err = s.do(req, &out)
	return out, err
}

// do performs one request and turns whatever came back into a sentinel.
func (s *Service) do(req *http.Request, out any) error {
	resp, err := s.http.Do(req)
	if err != nil {
		if cerr := req.Context().Err(); cerr != nil {
			return cerr
		}
		if errors.Is(err, redirectguard.ErrRefused) {
			return fmt.Errorf("%w: %w", ErrUnavailable, redirectguard.ErrRefused)
		}
		return ErrOffline
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		if json.Unmarshal(body, out) != nil {
			return errTransientResponse
		}
		return nil
	}
	return refusalOf(resp, body)
}

func refusalOf(resp *http.Response, body []byte) error {
	var we wireError
	_ = json.Unmarshal(body, &we)
	sentinel, known := codeSentinels[we.Error.Code]
	if !known {
		switch resp.StatusCode {
		case http.StatusTooManyRequests:
			sentinel = ErrRateLimited
		case http.StatusRequestEntityTooLarge:
			sentinel = ErrTooLarge
		default:
			if resp.StatusCode >= 500 && resp.StatusCode < 600 {
				return errTransientResponse
			}
			return ErrUnavailable
		}
	}
	if errors.Is(sentinel, ErrRateLimited) {
		secs, _ := strconv.Atoi(strings.TrimSpace(resp.Header.Get("Retry-After")))
		return &RateLimitedError{After: time.Duration(secs) * time.Second}
	}
	return sentinel
}

// Only submits carry the idempotency key that makes replaying an ambiguous
// response safe; a transient answer gets at most one retry.
func (s *Service) withRetry(ctx context.Context, call func() error, submit bool) error {
	var err error
	for i := 0; ; i++ {
		err = call()
		retry := errors.Is(err, ErrOffline) || (submit && i == 0 && errors.Is(err, errTransientResponse))
		if !retry || i >= len(s.backoff) {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(s.backoff[i]):
		}
	}
}

// fingerprint identifies the content of a report, not the attempt: the same
// text sent twice from this machine is one report.
func (w wireSubmit) fingerprint() string {
	h := sha256.New()
	for _, part := range []string{string(w.Category), w.Body, w.DisplayName, w.Contact} {
		fmt.Fprintf(h, "%d:%s|", len(part), part)
	}
	for _, a := range w.Attachments {
		fmt.Fprintf(h, "%d:%s|", len(a.DataBase64), a.DataBase64)
	}
	return hex.EncodeToString(h.Sum(nil))
}
