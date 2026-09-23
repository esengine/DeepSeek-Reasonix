package providerbroker

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"reasonix/internal/provider"
)

// catalogTTL bounds how stale a remote session's model list may be. The list
// changes when someone edits config at home, which no remote call observes, so
// it is re-read rather than cached for the session.
const catalogTTL = 3 * time.Second

// Client is the remote end: a provider.Resolver whose catalog and completions
// come from the machine that holds the credentials. It is what a bootstrapped
// serve installs in place of boot's config-backed resolver.
type Client struct {
	endpoint Endpoint
	http     *http.Client
	now      func() time.Time

	mu      sync.Mutex
	catalog []provider.Descriptor
	read    time.Time
}

// Endpoint answers where the broker is and the token it expects. It is asked
// on every request, so a serve can follow a broker republished elsewhere
// without being restarted. An error fails that request and nothing else.
type Endpoint func() (baseURL, token string, err error)

// NewClient points a resolver at a broker. baseURL is the loopback address the
// -R forward publishes on this machine; hc may be nil.
func NewClient(baseURL, token string, hc *http.Client) *Client {
	return NewEndpointClient(func() (string, string, error) { return baseURL, token, nil }, hc)
}

// NewEndpointClient is NewClient for a broker whose address can move.
func NewEndpointClient(endpoint Endpoint, hc *http.Client) *Client {
	if hc == nil {
		hc = &http.Client{}
	}
	return &Client{endpoint: endpoint, http: hc, now: time.Now}
}

// target is the broker as the endpoint answers for it right now.
func (c *Client) target() (base, token string, err error) {
	base, token, err = c.endpoint()
	if err != nil {
		return "", "", fmt.Errorf("providerbroker: locate the broker: %w", err)
	}
	return strings.TrimRight(strings.TrimSpace(base), "/"), strings.TrimSpace(token), nil
}

// Catalog returns the local machine's model list. The interface has no error
// return, so a broker that cannot be reached answers with the last list that
// arrived — a momentary tunnel outage must not empty a running session's
// picker. Resolve is where an unreachable broker becomes visible.
func (c *Client) Catalog() []provider.Descriptor {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	fresh := c.read.IsZero() || c.now().Sub(c.read) >= catalogTTL
	cached := c.catalog
	c.mu.Unlock()
	if !fresh {
		return append([]provider.Descriptor(nil), cached...)
	}
	list, err := c.fetchCatalog(context.Background())
	if err != nil {
		return append([]provider.Descriptor(nil), cached...)
	}
	c.mu.Lock()
	c.catalog, c.read = list, c.now()
	c.mu.Unlock()
	return append([]provider.Descriptor(nil), list...)
}

func (c *Client) fetchCatalog(ctx context.Context) ([]provider.Descriptor, error) {
	base, token, err := c.target()
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+PathCatalog, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set(HeaderToken, token)
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, decodeWireError(resp)
	}
	var out catalogResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	return out.Providers, nil
}

// Resolve binds a selection to the broker. It performs no call: a provider
// that failed to build and one whose endpoint is unreachable are the same
// outage to a caller, and both should surface where a turn can report them.
func (c *Client) Resolve(selection provider.Selection) (provider.Provider, error) {
	if c == nil {
		return nil, fmt.Errorf("providerbroker: client is nil")
	}
	ref := strings.TrimSpace(selection.Ref)
	if ref == "" {
		return nil, fmt.Errorf("providerbroker: provider selection ref is required")
	}
	return &brokered{client: c, selection: selection, name: c.nameFor(ref)}, nil
}

// nameFor is what the provider calls itself in usage records and errors. The
// catalog's display name when it is known, and the ref's own instance segment
// when the broker has not answered yet.
func (c *Client) nameFor(ref string) string {
	for _, d := range c.Catalog() {
		if d.Ref == ref {
			if d.DisplayName != "" {
				return d.DisplayName
			}
			break
		}
	}
	if instance, _, ok := strings.Cut(ref, "/"); ok {
		return instance
	}
	return ref
}

type brokered struct {
	client    *Client
	selection provider.Selection
	name      string
}

func (b *brokered) Name() string { return b.name }

func (b *brokered) Stream(ctx context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	body, err := json.Marshal(streamRequest{Selection: b.selection, Request: req})
	if err != nil {
		return nil, err
	}
	base, token, err := b.client.target()
	if err != nil {
		return nil, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, base+PathStream, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set(HeaderToken, token)
	httpReq.Header.Set("Content-Type", "application/json")
	resp, err := b.client.http.Do(httpReq)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		return nil, decodeWireError(resp)
	}
	out := make(chan provider.Chunk)
	go b.pump(ctx, resp.Body, out)
	return out, nil
}

// pump turns the NDJSON body into the chunk channel the Provider contract
// promises. A body that ends without a terminal chunk ended mid-answer, which
// is an interrupted stream and not a clean finish — saying so is what lets the
// agent replay the request instead of committing a truncated turn.
func (b *brokered) pump(ctx context.Context, body io.ReadCloser, out chan<- provider.Chunk) {
	defer close(out)
	defer body.Close()
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 0, 64*1024), maxChunkLine)
	terminal := false
	for scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}
		var wc wireChunk
		if err := json.Unmarshal(line, &wc); err != nil {
			emit(ctx, out, provider.Chunk{Type: provider.ChunkError, Err: provider.StreamInterrupt(err, provider.StreamInterruptConnectionReset)})
			return
		}
		chunk := wc.decode()
		if chunk.Type == provider.ChunkDone || chunk.Type == provider.ChunkError {
			terminal = true
		}
		if !emit(ctx, out, chunk) {
			return
		}
	}
	if err := scanner.Err(); err != nil {
		emit(ctx, out, provider.Chunk{Type: provider.ChunkError, Err: provider.StreamInterrupt(err, provider.StreamInterruptConnectionReset)})
		return
	}
	if !terminal {
		err := fmt.Errorf("providerbroker: the broker stream ended before the completion did")
		emit(ctx, out, provider.Chunk{Type: provider.ChunkError, Err: provider.StreamInterrupt(err, provider.StreamInterruptConnectionReset)})
	}
}

// maxChunkLine bounds one NDJSON line. A single chunk is a delta, not a whole
// answer, and a line past this is a framing failure rather than a big token.
const maxChunkLine = 8 << 20

func emit(ctx context.Context, out chan<- provider.Chunk, c provider.Chunk) bool {
	select {
	case out <- c:
		return true
	case <-ctx.Done():
		return false
	}
}

// UnknownModelError is a ref the broker's machine has no model for. It says
// where the lookup ran, because the host reporting it keeps a config of its
// own that may well carry the ref — and that config is not where it resolves.
type UnknownModelError struct {
	Ref string
}

func (e *UnknownModelError) Error() string {
	return fmt.Sprintf("model %q is not configured on the machine this host resolves models through; "+
		"choose one of that machine's models, or set this host's provider to %q to use its own", e.Ref, "remote")
}

func (e *UnknownModelError) Unwrap() error { return provider.ErrUnknownModel }

func decodeWireError(resp *http.Response) error {
	data, err := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if err != nil || len(bytes.TrimSpace(data)) == 0 {
		return fmt.Errorf("providerbroker: %s", resp.Status)
	}
	var we wireError
	if json.Unmarshal(data, &we) != nil || we.Kind == "" {
		return fmt.Errorf("providerbroker: %s: %s", resp.Status, strings.TrimSpace(string(data)))
	}
	return we.decode()
}
