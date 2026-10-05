package neterr

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"testing"
)

func TestIsConnReset(t *testing.T) {
	if IsConnReset(nil) {
		t.Error("nil is not a conn reset")
	}
	if IsConnReset(context.Canceled) || IsConnReset(context.DeadlineExceeded) {
		t.Error("ctx cancel/deadline must not look like a recoverable reset")
	}
	if IsConnReset(errors.New("decode stream: invalid character")) {
		t.Error("a plain protocol error must not be treated as a conn reset")
	}
	for _, err := range []error{
		io.ErrUnexpectedEOF,
		&net.OpError{Op: "read", Err: resetErrors[0]},
		fmt.Errorf("read stream: %w", &net.OpError{Op: "read", Err: errors.New("wsarecv: forcibly closed")}),
	} {
		if !IsConnReset(err) {
			t.Errorf("want conn reset for %v", err)
		}
	}
}

func TestIsConnResetExcludesDNS(t *testing.T) {
	for _, dns := range []*net.DNSError{
		{Err: "no such host", Name: "api.deepseek.com", IsNotFound: true},
		{Err: "i/o timeout", Name: "api.deepseek.com", IsTimeout: true, IsTemporary: true},
	} {
		err := fmt.Errorf("dial: %w", &net.OpError{Op: "dial", Err: dns})
		if IsConnReset(err) {
			t.Errorf("DNS failure %v is not a connection reset", dns)
		}
	}
}
