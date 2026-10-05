// Package neterr answers what a transport error is from the identity the
// operating system gave it, never from the sentence it rendered. A system
// error's text arrives in the machine's own language and is reworded between
// releases, so a word list that matches on an English host silently stops
// matching elsewhere. The judgement lives here once because three packages
// were each carrying their own copy of it.
package neterr

import (
	"context"
	"errors"
	"io"
	"net"
)

// IsConnReset reports whether err is a connection-level drop (peer reset,
// truncated body, closed socket) as opposed to a protocol or caller error. A
// stream cut this way mid-body can be replayed from scratch, unlike a decode
// or 4xx error. The common trigger is a local proxy (v2rayN/sing-box)
// idle-closing a long-lived SSE connection during a reasoner's first-token gap.
func IsConnReset(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	if errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, io.EOF) ||
		errors.Is(err, net.ErrClosed) || isAny(err, resetErrors) {
		return true
	}
	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		return false
	}
	var netErr net.Error
	return errors.As(err, &netErr)
}

func isAny(err error, targets []error) bool {
	for _, target := range targets {
		if errors.Is(err, target) {
			return true
		}
	}
	return false
}
