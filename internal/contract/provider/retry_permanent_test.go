package provider

import (
	"context"
	"crypto/x509"
	"errors"
	"net"
	"net/http"
	"os"
	"syscall"
	"testing"
	"time"
)

// 1.x failed a request to an endpoint that refuses connections at once;
// backing off through MaxRetries turned that into a minute and a half.
func TestSendWithRetryFailsFastWhenTheEndpointRefuses(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	_ = ln.Close()

	calls := 0
	cl := &http.Client{Transport: rtFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		return http.DefaultTransport.RoundTrip(r)
	})}
	start := time.Now()
	_, err = SendWithRetry(context.Background(), cl, SendOptions{Provider: "p"}, func(ctx context.Context) (*http.Request, error) {
		return http.NewRequestWithContext(ctx, http.MethodPost, "http://"+addr+"/v1/chat/completions", nil)
	})
	if err == nil {
		t.Fatal("a refused connection reported success")
	}
	if calls != 1 || time.Since(start) > 5*time.Second {
		t.Fatalf("refused connection took %d attempts over %s, want one attempt", calls, time.Since(start))
	}
}

func TestPermanentTransportErrReadsIdentity(t *testing.T) {
	refused := &net.OpError{Op: "dial", Net: "tcp", Err: os.NewSyscallError("connect", syscall.ECONNREFUSED)}
	reset := &net.OpError{Op: "read", Net: "tcp", Err: os.NewSyscallError("read", syscall.ECONNRESET)}
	for _, tc := range []struct {
		err  error
		want bool
	}{
		{refused, true},
		{&net.DNSError{Err: "no such host", Name: "nowhere.invalid", IsNotFound: true}, true},
		{x509.UnknownAuthorityError{}, true},
		{reset, false},
		{&net.DNSError{Err: "server misbehaving", Name: "x", IsTemporary: true}, false},
		{errors.New("connection refused"), false},
	} {
		if got := permanentTransportErr(tc.err); got != tc.want {
			t.Errorf("permanentTransportErr(%v) = %v, want %v", tc.err, got, tc.want)
		}
	}
}

func TestSendWithRetryStillRetriesAReset(t *testing.T) {
	calls := 0
	cl := &http.Client{Transport: rtFunc(func(*http.Request) (*http.Response, error) {
		calls++
		return nil, &net.OpError{Op: "read", Net: "tcp", Err: os.NewSyscallError("read", syscall.ECONNRESET)}
	})}
	_, err := SendWithRetry(WithRetryLimit(context.Background(), 1), cl, SendOptions{Provider: "p"}, newDummyReq)
	if err == nil || calls != 2 {
		t.Fatalf("reset: err=%v calls=%d, want an error after 2 attempts", err, calls)
	}
}
