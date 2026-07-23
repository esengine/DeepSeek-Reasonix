// Package transport defines the byte-stream boundary used by Remote Workbench.
// Desktop SSH (system OpenSSH or Go SSH) implements Transport; the workbench
// client never imports os/exec.
package transport

import (
	"context"
	"io"
)

// Stream is one already-authenticated bidirectional Remote byte stream
// (typically SSH stdio running `reasonix remote attach-workspace --stdio`).
type Stream interface {
	io.Reader
	io.Writer
	io.Closer
}

// Factory opens one fresh stream.
type Factory interface {
	Open(context.Context) (Stream, error)
}

// FactoryFunc adapts a function to Factory.
type FactoryFunc func(context.Context) (Stream, error)

func (f FactoryFunc) Open(ctx context.Context) (Stream, error) { return f(ctx) }
