package plugin

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"math"
	"net/http"
	"strconv"
	"strings"
)

// A 2026-07-28 request over Streamable HTTP mirrors parts of its body into
// headers for gateways. They are derived from the body here, so the two cannot
// disagree; a server that finds them disagreeing rejects the request.

// errBadParamHeader is a tool whose x-mcp-header annotations break the rules;
// the spec has the client leave such a tool out rather than guess.
var errBadParamHeader = errors.New("invalid x-mcp-header annotation")

// paramHeader is one tool argument mirrored into Mcp-Param-{Name}, found at
// the property path the schema annotated.
type paramHeader struct {
	name string
	path []string
}

const maxSafeInteger = 1<<53 - 1

// toolParamHeaders reads a tool's x-mcp-header annotations. Only a modern
// connection over HTTP uses them; every other transport ignores them.
func (c *Client) toolParamHeaders(schema json.RawMessage) ([]paramHeader, error) {
	if c.transport != "http" || c.modern.version == "" || len(schema) == 0 {
		return nil, nil
	}
	var root any
	if err := json.Unmarshal(schema, &root); err != nil {
		return nil, nil
	}
	var out []paramHeader
	seen := map[string]bool{}
	var walk func(node any, path []string, reachable bool) error
	walk = func(node any, path []string, reachable bool) error {
		switch n := node.(type) {
		case map[string]any:
			if raw, ok := n["x-mcp-header"]; ok {
				name, isString := raw.(string)
				switch {
				case !reachable || len(path) == 0:
					return fmt.Errorf("%w: not on a plain properties path", errBadParamHeader)
				case !isString || !validHeaderToken(name):
					return fmt.Errorf("%w: %v is not a header name", errBadParamHeader, raw)
				case seen[strings.ToLower(name)]:
					return fmt.Errorf("%w: %q used twice", errBadParamHeader, name)
				case !primitiveHeaderType(n["type"]):
					return fmt.Errorf("%w: %q is not a string, integer or boolean", errBadParamHeader, name)
				}
				seen[strings.ToLower(name)] = true
				out = append(out, paramHeader{name: name, path: append([]string(nil), path...)})
			}
			for key, child := range n {
				if key == "properties" {
					props, _ := child.(map[string]any)
					for prop, sub := range props {
						if err := walk(sub, append(path, prop), reachable); err != nil {
							return err
						}
					}
					continue
				}
				if err := walk(child, path, false); err != nil {
					return err
				}
			}
		case []any:
			for _, child := range n {
				if err := walk(child, path, false); err != nil {
					return err
				}
			}
		}
		return nil
	}
	if err := walk(root, nil, true); err != nil {
		return nil, err
	}
	return out, nil
}

func primitiveHeaderType(t any) bool {
	s, _ := t.(string)
	return s == "string" || s == "integer" || s == "boolean"
}

// validHeaderToken is RFC 9110's field-name token: 1*tchar.
func validHeaderToken(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r > 0x7e || !(r >= '0' && r <= '9' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || strings.ContainsRune("!#$%&'*+-.^_`|~", r)) {
			return false
		}
	}
	return true
}

type paramHeaderKey struct{}

// withParamHeaders carries one call's mirrored arguments to the transport.
func withParamHeaders(ctx context.Context, headers []paramHeader, args map[string]any) context.Context {
	if len(headers) == 0 {
		return ctx
	}
	out := http.Header{}
	for _, h := range headers {
		var v any = args
		for _, key := range h.path {
			m, ok := v.(map[string]any)
			if !ok {
				v = nil
				break
			}
			v = m[key]
		}
		if s, ok := headerScalar(v); ok {
			out.Set("Mcp-Param-"+h.name, encodeHeaderValue(s))
		}
	}
	return context.WithValue(ctx, paramHeaderKey{}, out)
}

// headerScalar is the header form of an argument, or false when the argument
// is absent, null, or not a value a header may carry.
func headerScalar(v any) (string, bool) {
	switch x := v.(type) {
	case string:
		return x, true
	case bool:
		return strconv.FormatBool(x), true
	case float64:
		if x != math.Trunc(x) || math.Abs(x) > maxSafeInteger {
			return "", false
		}
		return strconv.FormatInt(int64(x), 10), true
	case json.Number:
		if _, err := strconv.ParseInt(x.String(), 10, 64); err != nil {
			return "", false
		}
		return x.String(), true
	}
	return "", false
}

// encodeHeaderValue is a value as a header may carry it: plain when it is
// visible ASCII with no surrounding space, else the base64 sentinel form.
func encodeHeaderValue(s string) string {
	plain := s != "" && s == strings.TrimSpace(s) && !(strings.HasPrefix(s, "=?base64?") && strings.HasSuffix(s, "?="))
	for _, r := range s {
		if (r < 0x20 && r != '\t') || r > 0x7e {
			plain = false
			break
		}
	}
	if plain || s == "" {
		return s
	}
	return "=?base64?" + base64.StdEncoding.EncodeToString([]byte(s)) + "?="
}

// modernRequestHeaders are the headers a modern request carries, read from its
// body. A request without modern _meta gets none, and keeps legacy behavior.
func modernRequestHeaders(ctx context.Context, method string, params any) http.Header {
	p, ok := params.(map[string]any)
	if !ok {
		return nil
	}
	meta, _ := p["_meta"].(map[string]any)
	version, _ := meta[metaProtocolVersion].(string)
	if version == "" {
		return nil
	}
	h := http.Header{}
	h.Set("MCP-Protocol-Version", version)
	h.Set("Mcp-Method", method)
	switch method {
	case "tools/call", "prompts/get":
		if name, ok := p["name"].(string); ok {
			h.Set("Mcp-Name", encodeHeaderValue(name))
		}
	case "resources/read":
		if uri, ok := p["uri"].(string); ok {
			h.Set("Mcp-Name", encodeHeaderValue(uri))
		}
	}
	if extra, ok := ctx.Value(paramHeaderKey{}).(http.Header); ok && method == "tools/call" {
		maps.Copy(h, extra)
	}
	return h
}
