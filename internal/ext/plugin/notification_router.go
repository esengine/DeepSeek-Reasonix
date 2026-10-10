// Routing the notifications a server sends on its own. Progress has its own
// router because it is addressed to one call; everything else is addressed to
// the connection, so it is delivered by method to whoever registered for it.
package plugin

import (
	"encoding/json"
	"sync"
)

// notificationFunc receives one notification's params. It runs on the
// transport's read goroutine, so it must not block on I/O.
type notificationFunc func(params json.RawMessage)

// notificationTransport is implemented by transports that read server-initiated
// messages. A transport without it carries no notifications, so a client that
// depends on one stays with what it has.
type notificationTransport interface {
	registerNotification(method string, handler notificationFunc) func()
}

type notificationRouter struct {
	mu       sync.Mutex
	nextID   uint64
	handlers map[string]map[uint64]notificationFunc
}

func (r *notificationRouter) registerNotification(method string, handler notificationFunc) func() {
	if method == "" || handler == nil {
		return func() {}
	}
	r.mu.Lock()
	if r.handlers == nil {
		r.handlers = map[string]map[uint64]notificationFunc{}
	}
	if r.handlers[method] == nil {
		r.handlers[method] = map[uint64]notificationFunc{}
	}
	r.nextID++
	id := r.nextID
	r.handlers[method][id] = handler
	r.mu.Unlock()
	return func() {
		r.mu.Lock()
		delete(r.handlers[method], id)
		r.mu.Unlock()
	}
}

// dispatchNotification delivers params to every handler registered for method,
// off the router lock so a handler can register or drop one.
func (r *notificationRouter) dispatchNotification(method string, params json.RawMessage) bool {
	r.mu.Lock()
	handlers := make([]notificationFunc, 0, len(r.handlers[method]))
	for _, handler := range r.handlers[method] {
		handlers = append(handlers, handler)
	}
	r.mu.Unlock()
	for _, handler := range handlers {
		handler(params)
	}
	return len(handlers) > 0
}
