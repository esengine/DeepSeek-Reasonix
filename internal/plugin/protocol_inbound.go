package plugin

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"reasonix/internal/tool"
)

// progressTransport is optional so lightweight transports used by embedders and
// tests remain valid. Native transports implement it and route a server's
// notifications/progress message to the matching tools/call context.
type progressTransport interface {
	registerProgress(token string, sink tool.ProgressFunc) func()
}

type progressRouter struct {
	mu    sync.Mutex
	sinks map[string]tool.ProgressFunc
}

func (r *progressRouter) registerProgress(token string, sink tool.ProgressFunc) func() {
	if token == "" || sink == nil {
		return func() {}
	}
	r.mu.Lock()
	if r.sinks == nil {
		r.sinks = map[string]tool.ProgressFunc{}
	}
	r.sinks[token] = sink
	r.mu.Unlock()
	return func() {
		r.mu.Lock()
		delete(r.sinks, token)
		r.mu.Unlock()
	}
}

func (r *progressRouter) dispatchProgress(params json.RawMessage) bool {
	var p struct {
		ProgressToken any      `json:"progressToken"`
		Progress      *float64 `json:"progress"`
		Total         *float64 `json:"total"`
		Message       string   `json:"message"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return false
	}
	token := progressTokenKey(p.ProgressToken)
	if token == "" {
		return false
	}
	r.mu.Lock()
	sink := r.sinks[token]
	r.mu.Unlock()
	if sink == nil {
		return false
	}
	sink(formatMCPProgress(p.Message, p.Progress, p.Total))
	return true
}

func progressTokenKey(token any) string {
	switch value := token.(type) {
	case string:
		return value
	case float64:
		return strconv.FormatFloat(value, 'f', -1, 64)
	case json.Number:
		return value.String()
	default:
		return ""
	}
}

func formatMCPProgress(message string, progress, total *float64) string {
	label := strings.TrimSpace(message)
	if label == "" {
		label = "MCP progress"
	}
	formatNumber := func(value float64) string {
		return strconv.FormatFloat(value, 'f', -1, 64)
	}
	switch {
	case progress != nil && total != nil:
		return fmt.Sprintf("%s (%s/%s)\n", label, formatNumber(*progress), formatNumber(*total))
	case progress != nil:
		return fmt.Sprintf("%s (%s)\n", label, formatNumber(*progress))
	default:
		return label + "\n"
	}
}

type mcpRoot struct {
	URI  string `json:"uri"`
	Name string `json:"name,omitempty"`
}

func mcpRoots(workspaceRoot string) []mcpRoot {
	root := strings.TrimSpace(workspaceRoot)
	if root == "" {
		return nil
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil
	}
	clean := filepath.Clean(abs)
	path := filepath.ToSlash(clean)
	fileURL := &url.URL{Scheme: "file"}
	if strings.HasPrefix(path, "//") {
		parts := strings.SplitN(strings.TrimPrefix(path, "//"), "/", 2)
		fileURL.Host = parts[0]
		if len(parts) == 2 {
			fileURL.Path = "/" + parts[1]
		} else {
			fileURL.Path = "/"
		}
	} else {
		if volume := filepath.VolumeName(clean); volume != "" && !strings.HasPrefix(path, "/") {
			path = "/" + path
		}
		fileURL.Path = path
	}
	name := filepath.Base(clean)
	if name == "." {
		name = clean
	}
	return []mcpRoot{{URI: fileURL.String(), Name: name}}
}

type inboundMessage struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

func decodeInboundMessage(payload []byte) (inboundMessage, bool) {
	var message inboundMessage
	if err := json.Unmarshal(payload, &message); err != nil {
		return inboundMessage{}, false
	}
	return message, true
}

func isNotificationID(id json.RawMessage) bool {
	id = bytes.TrimSpace(id)
	return len(id) == 0 || bytes.Equal(id, []byte("null"))
}

func serverRequestReply(id json.RawMessage, method string, roots []mcpRoot) any {
	response := struct {
		JSONRPC string          `json:"jsonrpc"`
		ID      json.RawMessage `json:"id"`
		Result  any             `json:"result,omitempty"`
		Error   *rpcError       `json:"error,omitempty"`
	}{JSONRPC: "2.0", ID: append(json.RawMessage(nil), id...)}
	switch method {
	case "ping":
		response.Result = map[string]any{}
	case "roots/list":
		response.Result = map[string]any{"roots": roots}
	default:
		response.Error = &rpcError{Code: -32601, Message: "Method not found"}
	}
	return response
}
