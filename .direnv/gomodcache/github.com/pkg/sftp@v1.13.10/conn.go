package sftp

import (
	"context"
	"encoding"
	"fmt"
	"io"
	"sync"
)

// conn implements a bidirectional channel on which client and server
// connections are multiplexed.
type conn struct {
	io.Reader
	io.WriteCloser
	// this is the same allocator used in packet manager
	alloc      *allocator
	sync.Mutex // used to serialise writes to sendPacket
}

// the orderID is used in server mode if the allocator is enabled.
// For the client mode just pass 0.
// It returns io.EOF if the connection is closed and
// there are no more packets to read.
func (c *conn) recvPacket(orderID uint32) (fxp, []byte, error) {
	return recvPacket(c, c.alloc, orderID)
}

func (c *conn) sendPacket(m encoding.BinaryMarshaler) error {
	c.Lock()
	defer c.Unlock()

	return sendPacket(c, m)
}

func (c *conn) Close() error {
	c.Lock()
	defer c.Unlock()
	return c.WriteCloser.Close()
}

type clientConn struct {
	conn
	wg sync.WaitGroup

	wait func() error // if non-nil, call this during Wait() to get a possible remote status error.

	sync.Mutex                          // protects inflight
	inflight   map[uint32]chan<- result // outstanding requests

	closed chan struct{}
	err    error
}

// Wait blocks until the conn has shut down, and return the error
// causing the shutdown. It can be called concurrently from multiple
// goroutines.
func (c *clientConn) Wait() error {
	<-c.closed

	if c.wait == nil {
		// Only return this error if c.wait won't return something more useful.
		return c.err
	}

	if err := c.wait(); err != nil {

		// TODO: when https://github.com/golang/go/issues/35025 is fixed,
		// we can remove this if block entirely.
		// Right now, it’s always going to return this, so it is not useful.
		// But we have this code here so that as soon as the ssh library is updated,
		// we can return a possibly more useful error.
		if err.Error() == "ssh: session not started" {
			return c.err
		}

		return err
	}

	// c.wait returned no error; so, let's return something maybe more useful.
	return c.err
}

// Close closes the SFTP session.
func (c *clientConn) Close() error {
	defer c.wg.Wait()
	return c.conn.Close()
}

// recv continuously reads from the server and forwards responses to the
// appropriate channel.
func (c *clientConn) recv() error {
	defer c.conn.Close()

	for {
		typ, data, err := c.recvPacket(0)
		if err != nil {
			return err
		}
		sid, _, err := unmarshalUint32Safe(data)
		if err != nil {
			return err
		}

		ch, ok := c.getChannel(sid)
		if !ok {
			// This is an unexpected occurrence. Send the error
			// back to all listeners so that they terminate
			// gracefully.
			return fmt.Errorf("sid not found: %d", sid)
		}

		ch <- result{typ: typ, data: data}
	}
}

func (c *clientConn) putChannel(ch chan<- result, sid uint32) bool {
	c.Lock()
	defer c.Unlock()

	select {
	case <-c.closed:
		// already closed with broadcastErr, return error on chan.
		ch <- result{err: ErrSSHFxConnectionLost}
		return false
	default:
	}

	c.inflight[sid] = ch
	return true
}

func (c *clientConn) getChannel(sid uint32) (chan<- result, bool) {
	c.Lock()
	defer c.Unlock()

	ch, ok := c.inflight[sid]
	delete(c.inflight, sid)

	return ch, ok
}

// result captures the result of receiving the a packet from the server
type result struct {
	typ  fxp
	data []byte
	err  error
}

type idmarshaler interface {
	id() uint32
	encoding.BinaryMarshaler
}

func (c *clientConn) sendPacket(ctx context.Context, ch chan result, p idmarshaler) (fxp, []byte, error) {
	if cap(ch) < 1 {
		ch = make(chan result, 1)
	}

	c.dispatchRequest(ch, p)

	select {
	case <-ctx.Done():
		return 0, nil, ctx.Err()
	case s := <-ch:
		return s.typ, s.data, s.err
	}
}

// dispatchRequest should ideally only be called by race-detection tests outside of this file,
// where you have to ensure two packets are in flight sequentially after each other.
func (c *clientConn) dispatchRequest(ch chan<- result, p idmarshaler) {
	sid := p.id()

	if !c.putChannel(ch, sid) {
		// already closed.
		return
	}

	if err := c.conn.sendPacket(p); err != nil {
		if ch, ok := c.getChannel(sid); ok {
			ch <- result{err: err}
		}
	}
}

// broadcastErr sends an error to all goroutines waiting for a response.
func (c *clientConn) broadcastErr(err error) {
	c.Lock()
	defer c.Unlock()

	bcastRes := result{err: ErrSSHFxConnectionLost}
	for sid, ch := range c.inflight {
		ch <- bcastRes

		// Replace the chan in inflight,
		// we have hijacked this chan,
		// and this guarantees always-only-once sending.
		c.inflight[sid] = make(chan<- result, 1)
	}

	c.err = err
	close(c.closed)
}

type serverConn struct {
	conn
}

func (s *serverConn) sendError(id uint32, err error) error {
	return s.sendPacket(statusFromError(id, err))
}
