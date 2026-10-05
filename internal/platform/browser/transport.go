package browser

import (
	"bufio"
	"bytes"
	"context"
	"io"
	"sync"

	"github.com/gorilla/websocket"
)

// transport carries whole CDP messages. Chrome speaks the same JSON over a
// pair of pipes or a WebSocket; only the framing differs.
type transport interface {
	read() ([]byte, error)
	write([]byte) error
	close() error
}

// pipeTransport frames messages with a trailing NUL, the --remote-debugging-pipe
// protocol. Nothing listens on a port, so no other local process can drive the
// browser.
type pipeTransport struct {
	r       *bufio.Reader
	w       io.Writer
	closers []io.Closer
	wmu     sync.Mutex
}

func newPipeTransport(r io.Reader, w io.Writer, closers ...io.Closer) *pipeTransport {
	return &pipeTransport{r: bufio.NewReaderSize(r, 1<<20), w: w, closers: closers}
}

func (p *pipeTransport) read() ([]byte, error) {
	msg, err := p.r.ReadBytes(0)
	if err != nil {
		return nil, err
	}
	return bytes.TrimSuffix(msg, []byte{0}), nil
}

func (p *pipeTransport) write(msg []byte) error {
	p.wmu.Lock()
	defer p.wmu.Unlock()
	_, err := p.w.Write(append(msg, 0))
	return err
}

func (p *pipeTransport) close() error {
	var first error
	for _, c := range p.closers {
		if err := c.Close(); err != nil && first == nil {
			first = err
		}
	}
	return first
}

// wsTransport is the port-based endpoint, used where a child cannot inherit
// extra pipe descriptors.
type wsTransport struct {
	c   *websocket.Conn
	wmu sync.Mutex
}

func dialWS(ctx context.Context, url string) (*wsTransport, error) {
	dialer := websocket.Dialer{ReadBufferSize: 1 << 16, WriteBufferSize: 1 << 16}
	c, _, err := dialer.DialContext(ctx, url, nil)
	if err != nil {
		return nil, err
	}
	return &wsTransport{c: c}, nil
}

func (t *wsTransport) read() ([]byte, error) {
	_, msg, err := t.c.ReadMessage()
	return msg, err
}

func (t *wsTransport) write(msg []byte) error {
	t.wmu.Lock()
	defer t.wmu.Unlock()
	return t.c.WriteMessage(websocket.TextMessage, msg)
}

func (t *wsTransport) close() error { return t.c.Close() }
