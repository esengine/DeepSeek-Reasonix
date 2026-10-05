package plugin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"

	"reasonix/internal/contract/tool"
)

// The 2026-07-28 revision drops the initialize handshake: every request states
// its protocol version, client identity and capabilities in _meta, and a server
// is asked which era it speaks with server/discover. A client that also serves
// legacy servers probes first and falls back to initialize on anything that is
// not a recognizably modern answer.
const (
	modernProtocolVersion = "2026-07-28"
	discoverMethod        = "server/discover"

	metaProtocolVersion    = "io.modelcontextprotocol/protocolVersion"
	metaClientInfo         = "io.modelcontextprotocol/clientInfo"
	metaClientCapabilities = "io.modelcontextprotocol/clientCapabilities"

	codeHeaderMismatch             = -32020
	codeMissingClientCapability    = -32021
	codeUnsupportedProtocolVersion = -32022

	// maxInputRounds bounds a multi round-trip request; a server that keeps
	// asking is not converging. The other two bound what one round echoes.
	maxInputRounds    = 4
	maxInputRequests  = 16
	maxRequestStateSz = 1 << 20
	// modernProbeWait bounds the probe: a legacy server may answer a method
	// sent before initialize with nothing at all, and every startup of one
	// pays this wait before falling back.
	modernProbeWait = 3 * time.Second
)

// ErrMCPInputRequired is a modern server asking, mid-call, for input this
// client cannot give it (an elicitation or a sampling request).
var ErrMCPInputRequired = errors.New("MCP server needs input this client does not provide")

// errMCPInputOverBounds is one input round asking for more than a client echoes.
var errMCPInputOverBounds = errors.New("MCP input round over this client's bounds")

// modernSession is what the probe learned. An empty version means the server
// was brought up with the legacy initialize handshake.
type modernSession struct {
	version string
}

// modernVersions are the revisions this client speaks without a handshake.
var modernVersions = []string{modernProtocolVersion}

func (c *Client) clientCapabilities() map[string]any {
	capabilities := map[string]any{"elicitation": elicitCapability()}
	if len(mcpRoots(c.spec.WorkspaceRoot)) > 0 {
		capabilities["roots"] = map[string]any{}
	}
	return capabilities
}

func (c *Client) clientMeta(version string) map[string]any {
	return map[string]any{
		metaProtocolVersion:    version,
		metaClientInfo:         map[string]any{"name": "reasonix", "version": "dev"},
		metaClientCapabilities: c.clientCapabilities(),
	}
}

// withMeta is params with meta merged into its _meta, leaving the caller's map
// untouched and keeping any _meta already there (a progress token).
func withMeta(params any, meta map[string]any) (map[string]any, error) {
	out := map[string]any{}
	switch p := params.(type) {
	case nil:
	case map[string]any:
		maps.Copy(out, p)
	default:
		raw, err := json.Marshal(p)
		if err != nil {
			return nil, err
		}
		if err := json.Unmarshal(raw, &out); err != nil {
			return nil, err
		}
	}
	merged := map[string]any{}
	if existing, ok := out["_meta"].(map[string]any); ok {
		maps.Copy(merged, existing)
	}
	maps.Copy(merged, meta)
	out["_meta"] = merged
	return out, nil
}

type discoverResult struct {
	SupportedVersions []string                   `json:"supportedVersions"`
	Capabilities      map[string]json.RawMessage `json:"capabilities"`
	Instructions      string                     `json:"instructions"`
}

// probe asks which era the server speaks. It returns the modern version both
// sides share, or "" to fall back to initialize. It fails only when the server
// is modern and shares no revision with this client: falling back then would
// be speaking a protocol the server said it does not.
func (c *Client) probe(ctx context.Context, t transport) (string, *discoverResult, error) {
	wait := modernProbeWait
	if deadline, ok := ctx.Deadline(); ok {
		wait = min(wait, time.Until(deadline)/2)
	}
	pctx, cancel := context.WithTimeout(ctx, wait)
	defer cancel()
	res, err := t.call(pctx, discoverMethod, map[string]any{"_meta": c.clientMeta(modernProtocolVersion)})
	if err == nil {
		var d discoverResult
		if json.Unmarshal(res, &d) != nil || len(d.SupportedVersions) == 0 {
			return "", nil, nil // not a DiscoverResult: a legacy server echoing something
		}
		return pickEra(d.SupportedVersions, &d)
	}
	if ctx.Err() != nil {
		return "", nil, ctx.Err()
	}
	// The modern codes sit in JSON-RPC's implementation-defined range, where a
	// legacy server may use them for its own reasons. Only a list of revisions
	// marks the answer as modern; anything else gets the handshake.
	var rpc *rpcError
	if !errors.As(err, &rpc) || rpc.Code != codeUnsupportedProtocolVersion {
		return "", nil, nil
	}
	var data struct {
		Supported []string `json:"supported"`
	}
	if json.Unmarshal(rpc.Data, &data) != nil || len(data.Supported) == 0 {
		return "", nil, nil
	}
	return pickEra(data.Supported, nil)
}

// pickEra chooses from what a modern server says it supports: a modern
// revision both sides speak, else a legacy one it still serves.
func pickEra(supported []string, d *discoverResult) (string, *discoverResult, error) {
	for _, v := range modernVersions {
		if slices.Contains(supported, v) {
			return v, d, nil
		}
	}
	for _, v := range supportedProtocolVersions {
		if slices.Contains(supported, v) {
			return "", nil, nil
		}
	}
	return "", nil, fmt.Errorf("%w (server supports %s)", ErrUnsupportedProtocolVersion, strings.Join(supported, ", "))
}

// connect brings a new connection up in whichever era the server speaks.
func (c *Client) connect(ctx context.Context) error {
	// The HTTP+SSE transport belongs to the 2024-11-05 revision; nothing modern
	// is served over it.
	if c.transport == "sse" {
		return c.initialize(ctx)
	}
	version, d, err := c.probe(ctx, c.t)
	if err != nil {
		return fmt.Errorf("plugin %q: %w", c.name, err)
	}
	if version == "" {
		return c.initialize(ctx)
	}
	c.modern.version = version
	if d != nil {
		_, c.hasTools = d.Capabilities["tools"]
		_, c.hasPrompts = d.Capabilities["prompts"]
		_, c.hasResources = d.Capabilities["resources"]
		c.instructions = strings.TrimSpace(d.Instructions)
	}
	return nil
}

// handshakeOn readies a replacement connection. A modern server has no
// session to open, so there is nothing to send.
func (c *Client) handshakeOn(ctx context.Context, next transport) error {
	if c.modern.version != "" {
		return nil
	}
	return c.initializeSessionOn(ctx, next, false)
}

// callModern sends one modern request and follows it through any rounds the
// server needs more input for, answering the ones this client can.
func (c *Client) callModern(ctx context.Context, t transport, method string, params any) (json.RawMessage, error) {
	p, err := withMeta(params, c.clientMeta(c.modern.version))
	if err != nil {
		return nil, err
	}
	for round := 0; ; round++ {
		res, err := c.callOnce(ctx, t, method, p)
		if err != nil {
			return nil, err
		}
		var r struct {
			ResultType    string                     `json:"resultType"`
			InputRequests map[string]json.RawMessage `json:"inputRequests"`
			RequestState  *string                    `json:"requestState"`
		}
		if json.Unmarshal(res, &r) != nil || r.ResultType != "input_required" {
			return res, nil
		}
		if round+1 >= maxInputRounds {
			return nil, fmt.Errorf("plugin %q: %s still needed input after %d rounds", c.name, method, maxInputRounds)
		}
		if len(r.InputRequests) > maxInputRequests || (r.RequestState != nil && len(*r.RequestState) > maxRequestStateSz) {
			return nil, fmt.Errorf("plugin %q: %s asked for %d inputs with %s of state: %w", c.name, method, len(r.InputRequests), stateSize(r.RequestState), errMCPInputOverBounds)
		}
		responses, err := c.answerInputRequests(ctx, r.InputRequests)
		if err != nil {
			return nil, fmt.Errorf("plugin %q: %s: %w", c.name, method, err)
		}
		next := maps.Clone(p)
		next["inputResponses"] = responses
		if r.RequestState != nil {
			next["requestState"] = *r.RequestState
		} else {
			delete(next, "requestState")
		}
		p = next
	}
}

// answerInputRequests answers the workspace roots itself and puts a form in
// front of the person. Anything else is a capability it never declared.
func (c *Client) answerInputRequests(ctx context.Context, requests map[string]json.RawMessage) (map[string]any, error) {
	out := make(map[string]any, len(requests))
	for key, raw := range requests {
		var req struct {
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
		}
		_ = json.Unmarshal(raw, &req)
		switch req.Method {
		case "roots/list":
			out[key] = map[string]any{"roots": mcpRoots(c.spec.WorkspaceRoot)}
		case elicitMethod:
			e, _ := tool.ElicitorFrom(ctx)
			result, err := elicit(ctx, e, c.name, req.Params)
			if err != nil {
				return nil, err
			}
			out[key] = result
		default:
			return nil, fmt.Errorf("%w: %s", ErrMCPInputRequired, req.Method)
		}
	}
	return out, nil
}

func stateSize(s *string) string {
	if s == nil {
		return "no"
	}
	return fmt.Sprintf("%d bytes", len(*s))
}
