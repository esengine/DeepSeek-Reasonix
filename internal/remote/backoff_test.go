package remote

import (
	"testing"
	"time"
)

func TestBackoffDelayCeilingGrowsAndCaps(t *testing.T) {
	p := BackoffPolicy{Initial: time.Second, Factor: 2, Max: 60 * time.Second}
	want := []time.Duration{
		1 * time.Second,
		2 * time.Second,
		4 * time.Second,
		8 * time.Second,
		16 * time.Second,
		32 * time.Second,
		60 * time.Second, // 64 capped
		60 * time.Second,
	}
	for i, w := range want {
		if got := p.delay(i); got != w {
			t.Errorf("delay(%d) = %v, want %v", i, got, w)
		}
	}
}

func TestBackoffDefaults(t *testing.T) {
	var p BackoffPolicy
	if p.delay(0) != time.Second {
		t.Errorf("default initial = %v", p.delay(0))
	}
	if p.delay(100) != 60*time.Second {
		t.Errorf("default cap = %v", p.delay(100))
	}
}

func TestClassifyDialError(t *testing.T) {
	authy := classifyDialError(errString("ssh: handshake failed: ssh: unable to authenticate, attempted methods [none publickey], no supported methods remain"))
	if !isAuthErr(authy) {
		t.Errorf("auth failure not classified as ErrAuthFailed: %v", authy)
	}
	transient := classifyDialError(errString("dial tcp 10.0.0.1:22: connect: connection refused"))
	if isAuthErr(transient) {
		t.Errorf("transient error misclassified as auth: %v", transient)
	}
	if classifyDialError(nil) != nil {
		t.Error("nil error should classify to nil")
	}
}

type errString string

func (e errString) Error() string { return string(e) }

func isAuthErr(err error) bool {
	type iser interface{ Is(error) bool }
	if x, ok := err.(iser); ok {
		return x.Is(ErrAuthFailed)
	}
	return false
}
