package websearch

import (
	"context"
	"errors"
	"net/http"
	"time"
)

const searxngProbeTimeout = 8 * time.Second

// SearxngProbe is what one test query to an instance showed. Code is empty when
// the instance answered; Status is the HTTP status when that is what refused.
type SearxngProbe struct {
	OK     bool   `json:"ok"`
	Code   string `json:"code,omitempty"`
	Status int    `json:"status,omitempty"`
}

// ProbeSearxng sends one search to the address and reports why it did not
// answer. It follows no redirect, so the address the user typed is the only
// host it ever contacts, and it is bounded in time and in body size.
func ProbeSearxng(ctx context.Context, raw string) (SearxngProbe, error) {
	client := &http.Client{
		Timeout:       searxngProbeTimeout,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	backend, err := NewSearxng(raw, client)
	if err != nil {
		return SearxngProbe{}, err
	}
	_, err = backend.Search(ctx, "searxng")
	var failed *BackendError
	switch {
	case err == nil, errors.As(err, &failed) && failed.Kind == FailEmpty:
		return SearxngProbe{OK: true}, nil
	case errors.As(err, &failed):
		return SearxngProbe{Code: failed.Code, Status: failed.Status}, nil
	}
	return SearxngProbe{Code: CodeSearxngUnreachable}, nil
}
