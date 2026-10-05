package browser

import (
	"context"
	"errors"
)

// Endpoint is a browser a host provides in place of one this package launches:
// the host carries whole protocol messages each way and owns what they drive.
type Endpoint interface {
	ReadMessage() ([]byte, error)
	WriteMessage([]byte) error
	Close() error
}

// EndpointDialer reaches the host's browser for a profile. ErrNoHost means no
// host is attached right now, and a browser is launched instead.
type EndpointDialer func(ctx context.Context, profile string) (Endpoint, error)

// ErrNoHost is an EndpointDialer finding nothing to connect to.
var ErrNoHost = errors.New("no host browser is attached")

type endpointTransport struct{ ep Endpoint }

func (t endpointTransport) read() ([]byte, error) { return t.ep.ReadMessage() }
func (t endpointTransport) write(m []byte) error  { return t.ep.WriteMessage(m) }
func (t endpointTransport) close() error          { return t.ep.Close() }

// attachEndpoint drives a hosted browser with the same connection a launched
// one gets, and makes the same demands of it before it counts as started.
func attachEndpoint(ctx context.Context, spec LaunchSpec, ep Endpoint) (*engine, error) {
	ctx, cancel := context.WithTimeout(ctx, launchTimeout)
	defer cancel()
	e := &engine{spec: spec, exited: make(chan struct{}), listeners: map[int]func(event){}}
	e.conn = newConn(endpointTransport{ep})
	e.unsub = e.conn.subscribe("", e.dispatch)
	for _, cmd := range []struct {
		method string
		params any
	}{
		{"Browser.getVersion", nil},
		{"Target.setDiscoverTargets", map[string]any{"discover": true}},
		{"Browser.setDownloadBehavior", map[string]any{"behavior": "deny", "eventsEnabled": true}},
	} {
		if err := e.conn.call(ctx, "", cmd.method, cmd.params, nil); err != nil {
			e.kill()
			return nil, fail(CodeEngineFailed, "the host browser did not answer %s: %v", cmd.method, err)
		}
	}
	return e, nil
}
