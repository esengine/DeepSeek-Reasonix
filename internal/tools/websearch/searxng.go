package websearch

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"

	"reasonix/internal/contract/config"
)

// These caps bound what one search puts into the model's context and memory:
// ten results of at most 500 bytes each is a few thousand tokens however many
// results the instance returns. They are protections, not product limits; whether
// a user should be able to change them is tracked as a follow-up.
const (
	searxngMaxBody    = 2 << 20
	searxngMaxHits    = 10
	searxngMaxSnippet = 500
)

// Codes for why the user's own instance did not answer. SearXNG owns the first:
// it refuses a format its settings do not list with 403.
const (
	CodeSearxngJSONDisabled = "search.searxng_json_disabled"
	CodeSearxngUnreachable  = "search.searxng_unreachable"
	CodeSearxngNotSearxng   = "search.searxng_not_searxng"
	CodeSearxngRedirected   = "search.searxng_redirected"
	CodeSearxngStatus       = "search.searxng_status"
)

// Searxng queries the user's own SearXNG instance through its JSON format. It is
// free, so it is the first backend a chain offers.
type Searxng struct {
	base   *url.URL
	client *http.Client
}

// NewSearxng validates the instance address: an http(s) origin, optionally with
// a path prefix, and nothing that could redirect a query elsewhere.
func NewSearxng(raw string, client *http.Client) (*Searxng, error) {
	if err := config.ValidateSearxngURL(raw); err != nil {
		return nil, err
	}
	u, _ := url.Parse(strings.TrimSpace(raw))
	if client == nil {
		client = http.DefaultClient
	}
	return &Searxng{base: u, client: client}, nil
}

func (*Searxng) Name() string { return "searxng" }
func (*Searxng) Paid() bool   { return false }

type searxResult struct {
	Title   string `json:"title"`
	URL     string `json:"url"`
	Content string `json:"content"`
}

// searxResponse keeps results raw so an answer without the key, which is not
// SearXNG's, differs from an instance that found nothing.
type searxResponse struct {
	Results json.RawMessage `json:"results"`
}

func (s *Searxng) Search(ctx context.Context, query string) ([]Hit, error) {
	endpoint := *s.base
	endpoint.Path = strings.TrimRight(endpoint.Path, "/") + "/search"
	endpoint.RawQuery = url.Values{"q": {query}, "format": {"json"}}.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return nil, &BackendError{Backend: s.Name(), Kind: FailTransport, Err: err}
	}
	req.Header.Set("Accept", "application/json")
	resp, err := s.client.Do(req)
	if err != nil {
		return nil, &BackendError{Backend: s.Name(), Kind: FailTransport, Code: CodeSearxngUnreachable, Err: err}
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, &BackendError{Backend: s.Name(), Kind: FailStatus, Status: resp.StatusCode, Code: searxngStatusCode(resp.StatusCode)}
	}
	var body searxResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, searxngMaxBody)).Decode(&body); err != nil || body.Results == nil {
		return nil, &BackendError{Backend: s.Name(), Kind: FailDecode, Code: CodeSearxngNotSearxng, Err: err}
	}
	var found []searxResult
	if err := json.Unmarshal(body.Results, &found); err != nil {
		return nil, &BackendError{Backend: s.Name(), Kind: FailDecode, Code: CodeSearxngNotSearxng, Err: err}
	}
	hits := make([]Hit, 0, searxngMaxHits)
	for _, r := range found {
		if r.URL == "" || len(hits) == searxngMaxHits {
			continue
		}
		hits = append(hits, Hit{Title: r.Title, URL: r.URL, Snippet: truncateBytes(r.Content, searxngMaxSnippet)})
	}
	if len(hits) == 0 {
		return nil, &BackendError{Backend: s.Name(), Kind: FailEmpty}
	}
	return hits, nil
}

func searxngStatusCode(status int) string {
	switch {
	case status == http.StatusForbidden:
		return CodeSearxngJSONDisabled
	case status >= 300 && status < 400:
		return CodeSearxngRedirected
	}
	return CodeSearxngStatus
}
