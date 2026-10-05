package browser

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
)

// errConnClosed is the connection to the browser ending, as opposed to the
// browser answering a command with a protocol error.
var errConnClosed = errors.New("browser connection closed")

// protocolError is the browser refusing one command. It is a distinct type so
// a caller can tell "this node no longer exists" from "the browser is gone".
type protocolError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (e *protocolError) Error() string { return fmt.Sprintf("cdp %d: %s", e.Code, e.Message) }

func isProtocolError(err error) bool {
	_, ok := errors.AsType[*protocolError](err)
	return ok
}

type wireMessage struct {
	ID        int64           `json:"id,omitempty"`
	Method    string          `json:"method,omitempty"`
	Params    json.RawMessage `json:"params,omitempty"`
	SessionID string          `json:"sessionId,omitempty"`
	Result    json.RawMessage `json:"result,omitempty"`
	Error     *protocolError  `json:"error,omitempty"`
}

type event struct {
	Method string
	Params json.RawMessage
}

// conn multiplexes commands and events for the browser and every attached
// target over one transport. Events are delivered per session, in order, on a
// goroutine of their own: a handler may issue commands, whose replies arrive on
// the reader this would otherwise block.
type conn struct {
	tr transport

	mu      sync.Mutex
	next    int64
	pending map[int64]chan wireMessage
	queues  map[string]*eventQueue
	done    chan struct{}
}

func newConn(tr transport) *conn {
	c := &conn{
		tr:      tr,
		pending: map[int64]chan wireMessage{},
		queues:  map[string]*eventQueue{},
		done:    make(chan struct{}),
	}
	go c.readLoop()
	return c
}

func (c *conn) readLoop() {
	defer c.shutdown()
	for {
		raw, err := c.tr.read()
		if err != nil {
			return
		}
		var msg wireMessage
		if json.Unmarshal(raw, &msg) != nil {
			continue
		}
		c.mu.Lock()
		if msg.ID != 0 {
			reply, ok := c.pending[msg.ID]
			delete(c.pending, msg.ID)
			c.mu.Unlock()
			if ok {
				reply <- msg
			}
			continue
		}
		q := c.queues[msg.SessionID]
		c.mu.Unlock()
		if q != nil {
			q.push(event{Method: msg.Method, Params: msg.Params})
		}
	}
}

func (c *conn) shutdown() {
	c.mu.Lock()
	defer c.mu.Unlock()
	select {
	case <-c.done:
		return
	default:
	}
	close(c.done)
	for id, reply := range c.pending {
		close(reply)
		delete(c.pending, id)
	}
	for _, q := range c.queues {
		q.stop()
	}
	_ = c.tr.close()
}

func (c *conn) closed() <-chan struct{} { return c.done }

// call sends one command and decodes its result into out when out is non-nil.
func (c *conn) call(ctx context.Context, sessionID, method string, params, out any) error {
	body := map[string]any{"method": method}
	if params != nil {
		body["params"] = params
	}
	if sessionID != "" {
		body["sessionId"] = sessionID
	}
	reply := make(chan wireMessage, 1)
	c.mu.Lock()
	select {
	case <-c.done:
		c.mu.Unlock()
		return errConnClosed
	default:
	}
	c.next++
	id := c.next
	body["id"] = id
	c.pending[id] = reply
	c.mu.Unlock()

	raw, err := json.Marshal(body)
	if err != nil {
		c.forget(id)
		return err
	}
	if err := c.tr.write(raw); err != nil {
		c.forget(id)
		return errConnClosed
	}
	select {
	case msg, ok := <-reply:
		if !ok {
			return errConnClosed
		}
		if msg.Error != nil {
			return msg.Error
		}
		if out != nil && len(msg.Result) > 0 {
			return json.Unmarshal(msg.Result, out)
		}
		return nil
	case <-ctx.Done():
		c.forget(id)
		return ctx.Err()
	}
}

func (c *conn) forget(id int64) {
	c.mu.Lock()
	delete(c.pending, id)
	c.mu.Unlock()
}

// subscribe delivers every event for sessionID ("" is the browser itself) to
// handle, in arrival order, until unsubscribe or the connection ends.
func (c *conn) subscribe(sessionID string, handle func(event)) (unsubscribe func()) {
	q := newEventQueue(handle)
	c.mu.Lock()
	select {
	case <-c.done:
		q.stop()
	default:
		c.queues[sessionID] = q
	}
	c.mu.Unlock()
	return func() {
		c.mu.Lock()
		if c.queues[sessionID] == q {
			delete(c.queues, sessionID)
		}
		c.mu.Unlock()
		q.stop()
	}
}

// eventQueue is unbounded so the reader never waits on a slow handler; a
// browser emits events in bursts, not without end.
type eventQueue struct {
	mu      sync.Mutex
	items   []event
	wake    chan struct{}
	stopped chan struct{}
	once    sync.Once
}

func newEventQueue(handle func(event)) *eventQueue {
	q := &eventQueue{wake: make(chan struct{}, 1), stopped: make(chan struct{})}
	go func() {
		for {
			q.mu.Lock()
			batch := q.items
			q.items = nil
			q.mu.Unlock()
			for _, ev := range batch {
				handle(ev)
			}
			select {
			case <-q.wake:
			case <-q.stopped:
				return
			}
		}
	}()
	return q
}

func (q *eventQueue) push(ev event) {
	q.mu.Lock()
	q.items = append(q.items, ev)
	q.mu.Unlock()
	select {
	case q.wake <- struct{}{}:
	default:
	}
}

func (q *eventQueue) stop() { q.once.Do(func() { close(q.stopped) }) }
