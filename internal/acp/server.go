package acp

import (
	"io"

	"reasonix/internal/rpcwire"
)

// ACP retains its established 32 MiB inbound allowance. Reasonix Remote uses a
// separate, symmetric 8 MiB limit when it constructs rpcwire.Conn.
const maxMessageBytes = 32 << 20

type RequestHandler = rpcwire.RequestHandler
type NotificationHandler = rpcwire.NotificationHandler
type RPCError = rpcwire.RPCError
type Conn = rpcwire.Conn

// rpcError is kept as a package-local alias because the ACP integration tests
// decode the historical error frame directly.
type rpcError = rpcwire.ErrorObject

// NewConn preserves ACP's public construction API and wire behavior while the
// transport mechanics live in the protocol-neutral rpcwire package.
func NewConn(r io.Reader, w io.Writer) *Conn {
	return rpcwire.NewConn(r, w, rpcwire.Options{
		Name:            "acp",
		MaxInboundBytes: maxMessageBytes,
	})
}
