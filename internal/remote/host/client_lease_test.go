package host

import (
	"errors"
	"testing"
	"time"

	"reasonix/internal/remote/protocol"
)

type leaseTestClock struct{ now time.Time }

func (c *leaseTestClock) Now() time.Time          { return c.now }
func (c *leaseTestClock) Advance(d time.Duration) { c.now = c.now.Add(d) }

func newLeaseTestManager(clock *leaseTestClock) *LeaseManager {
	sequence := 0
	return NewLeaseManager(LeaseManagerOptions{
		Now: clock.Now,
		NewLeaseID: func() (protocol.LeaseID, error) {
			sequence++
			return protocol.LeaseID("lease-test-" + string(rune('0'+sequence))), nil
		},
	})
}

func TestLeaseAcquireResumeReplacesTransportGeneration(t *testing.T) {
	clock := &leaseTestClock{now: time.Unix(100, 0)}
	m := newLeaseTestManager(clock)
	first, err := m.Acquire("client-a", "")
	if err != nil {
		t.Fatal(err)
	}
	if first.Resumed || first.Binding.Generation != 1 || first.TTL != 30*time.Second || first.PingInterval != 10*time.Second {
		t.Fatalf("first grant = %+v", first)
	}

	clock.Advance(12 * time.Second)
	second, err := m.Acquire("client-a", first.Binding.LeaseID)
	if err != nil {
		t.Fatal(err)
	}
	if !second.Resumed || second.Binding.Generation != 2 || second.Binding.LeaseID != first.Binding.LeaseID {
		t.Fatalf("resumed grant = %+v", second)
	}
	assertRemoteCode(t, m.Validate(first.Binding, false), protocol.ErrStaleConnection)
	if err := m.Validate(second.Binding, false); err != nil {
		t.Fatalf("replacement binding: %v", err)
	}
}

func TestLeaseBusyDoesNotExposeHolderAndReportsRemainingTTL(t *testing.T) {
	clock := &leaseTestClock{now: time.Unix(200, 0)}
	m := newLeaseTestManager(clock)
	first, err := m.Acquire("client-a", "")
	if err != nil {
		t.Fatal(err)
	}
	clock.Advance(7 * time.Second)
	_, err = m.Acquire("client-b", first.Binding.LeaseID)
	remote := assertRemoteCode(t, err, protocol.ErrHostBusy)
	if remote.Data.RetryAfterMs == nil || *remote.Data.RetryAfterMs != 23_000 {
		t.Fatalf("retryAfterMs = %v", remote.Data.RetryAfterMs)
	}
	if remote.Data.Expected != "" || remote.Data.Actual != "" || remote.Data.Target != nil {
		t.Fatalf("HOST_BUSY leaked holder data: %+v", remote.Data)
	}

	_, err = m.Acquire("client-a", "")
	assertRemoteCode(t, err, protocol.ErrHostBusy)
}

func TestLeaseExpiryAllowsFreshAcquireWithoutCancellingExternalWork(t *testing.T) {
	clock := &leaseTestClock{now: time.Unix(300, 0)}
	m := newLeaseTestManager(clock)
	first, err := m.Acquire("client-a", "")
	if err != nil {
		t.Fatal(err)
	}
	workStillRunning := true
	clock.Advance(30 * time.Second)
	if m.Held() {
		t.Fatal("lease should expire at the TTL boundary")
	}
	second, err := m.Acquire("client-b", first.Binding.LeaseID)
	if err != nil {
		t.Fatal(err)
	}
	if second.Resumed || second.Binding.LeaseID == first.Binding.LeaseID {
		t.Fatalf("fresh grant = %+v", second)
	}
	if !workStillRunning {
		t.Fatal("lease state must not own or cancel runtime work")
	}
	assertRemoteCode(t, m.Validate(first.Binding, false), protocol.ErrLeaseNotHeld)
}

func TestLeasePingRenewsAndDetachReleases(t *testing.T) {
	clock := &leaseTestClock{now: time.Unix(400, 0)}
	m := newLeaseTestManager(clock)
	grant, err := m.Acquire("client-a", "")
	if err != nil {
		t.Fatal(err)
	}
	clock.Advance(25 * time.Second)
	if ttl, err := m.Ping(grant.Binding, grant.Binding.LeaseID); err != nil || ttl != 30*time.Second {
		t.Fatalf("ping ttl=%v err=%v", ttl, err)
	}
	clock.Advance(29 * time.Second)
	if !m.Held() {
		t.Fatal("ping should renew the lease")
	}
	if err := m.Detach(grant.Binding, grant.Binding.LeaseID); err != nil {
		t.Fatal(err)
	}
	if m.Held() {
		t.Fatal("detach should release immediately")
	}
	assertRemoteCode(t, m.PingError(grant.Binding), protocol.ErrLeaseNotHeld)
}

func TestLeaseRejectsWrongWireLeaseBeforeBinding(t *testing.T) {
	clock := &leaseTestClock{now: time.Unix(500, 0)}
	m := newLeaseTestManager(clock)
	grant, err := m.Acquire("client-a", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Ping(grant.Binding, "lease-wrong"); err == nil {
		t.Fatal("wrong ping lease accepted")
	} else {
		assertRemoteCode(t, err, protocol.ErrLeaseNotHeld)
	}
	if err := m.Detach(grant.Binding, "lease-wrong"); err == nil {
		t.Fatal("wrong detach lease accepted")
	} else {
		assertRemoteCode(t, err, protocol.ErrLeaseNotHeld)
	}
}

func TestLeaseIDIsNeverReusedAfterExpiry(t *testing.T) {
	now := time.Unix(5000, 0)
	generated := []protocol.LeaseID{"lease-issued", "lease-issued", "lease-fresh"}
	manager := NewLeaseManager(LeaseManagerOptions{
		Now: func() time.Time { return now },
		NewLeaseID: func() (protocol.LeaseID, error) {
			id := generated[0]
			generated = generated[1:]
			return id, nil
		},
		TTL: time.Second,
	})

	first, err := manager.Acquire("client-same", "")
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(time.Second)
	second, err := manager.Acquire("client-same", "")
	if err != nil {
		t.Fatal(err)
	}
	if second.Binding.LeaseID != "lease-fresh" {
		t.Fatalf("fresh leaseId = %q, want collision retry", second.Binding.LeaseID)
	}
	if second.Binding.Generation != 1 {
		t.Fatalf("fresh generation = %d, want 1", second.Binding.Generation)
	}
	assertRemoteCode(t, manager.Validate(first.Binding, false), protocol.ErrLeaseNotHeld)
}

func (m *LeaseManager) PingError(binding LeaseBinding) error {
	_, err := m.Ping(binding, binding.LeaseID)
	return err
}

func assertRemoteCode(t *testing.T, err error, code protocol.ReasonixErrorCode) *protocol.RemoteError {
	t.Helper()
	if err == nil {
		t.Fatalf("expected %s", code)
	}
	var remote *protocol.RemoteError
	if !errors.As(err, &remote) {
		t.Fatalf("error %T %v is not RemoteError", err, err)
	}
	if remote.Code != code {
		t.Fatalf("code = %s, want %s", remote.Code, code)
	}
	return remote
}
