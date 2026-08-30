package plugin

import (
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

// notificationTransport is implemented by supervised SDK transports that can
// receive server notifications. The callback must stay non-blocking because the
// SDK dispatches notification handlers independently from request completion.
type notificationTransport interface {
	registerNotification(method string, callback func(json.RawMessage)) func()
}

type progressRouter struct {
	mu    sync.Mutex
	sinks map[string]tool.ProgressFunc
}

type notificationRouter struct {
	mu        sync.Mutex
	nextID    uint64
	listeners map[string]map[uint64]func(json.RawMessage)
}

func (r *notificationRouter) registerNotification(method string, callback func(json.RawMessage)) func() {
	method = strings.TrimSpace(method)
	if method == "" || callback == nil {
		return func() {}
	}
	r.mu.Lock()
	if r.listeners == nil {
		r.listeners = map[string]map[uint64]func(json.RawMessage){}
	}
	r.nextID++
	id := r.nextID
	if r.listeners[method] == nil {
		r.listeners[method] = map[uint64]func(json.RawMessage){}
	}
	r.listeners[method][id] = callback
	r.mu.Unlock()
	return func() {
		r.mu.Lock()
		delete(r.listeners[method], id)
		if len(r.listeners[method]) == 0 {
			delete(r.listeners, method)
		}
		r.mu.Unlock()
	}
}

func (r *notificationRouter) dispatchNotification(method string, params json.RawMessage) {
	r.mu.Lock()
	listeners := make([]func(json.RawMessage), 0, len(r.listeners[method]))
	for _, callback := range r.listeners[method] {
		listeners = append(listeners, callback)
	}
	r.mu.Unlock()
	for _, callback := range listeners {
		callback(append(json.RawMessage(nil), params...))
	}
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

func (r *progressRouter) clear() {
	r.mu.Lock()
	r.sinks = nil
	r.mu.Unlock()
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
	if after, ok := strings.CutPrefix(path, "//"); ok {
		parts := strings.SplitN(after, "/", 2)
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
