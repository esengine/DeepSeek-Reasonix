package serve

import (
	"errors"
	"testing"
	"time"
)

func TestCloudPresenceTracksActiveControllersAndStableOrdinals(t *testing.T) {
	now := time.Date(2026, 9, 27, 12, 0, 0, 0, time.UTC)
	presence := newCloudPresence()
	presence.now = func() time.Time { return now }

	if got := presence.connect("browser-a", " Web   Studio "); got != 1 {
		t.Fatalf("first ordinal = %d, want 1", got)
	}
	now = now.Add(time.Minute)
	presence.touch("browser-a")
	if got := presence.connect("browser-b", "Web Studio"); got != 2 {
		t.Fatalf("second ordinal = %d, want 2", got)
	}

	views := presence.views()
	if len(views) != 2 || views[0].Name != "Web Studio" || views[0].Ordinal != 1 || !views[0].LastSeen.Equal(now) {
		t.Fatalf("views = %+v", views)
	}
	presence.disconnect("browser-a")
	views = presence.views()
	if len(views) != 1 || views[0].ID != "browser-b" {
		t.Fatalf("after disconnect = %+v", views)
	}
}

func TestCloudControllerCanBeRevokedFromDeviceShare(t *testing.T) {
	share := NewDeviceShare(nil)
	share.cloud.connect("browser-a", "Web Studio")
	called := ""
	share.setCloudDisconnect(func(id string) error {
		called = id
		share.cloud.disconnect(id)
		return nil
	})
	if !share.Revoke("browser-a") || called != "browser-a" {
		t.Fatalf("revoke = %q, want browser-a", called)
	}
	share.cloud.connect("browser-b", "Web Studio")
	share.setCloudDisconnect(func(string) error { return errors.New("relay unavailable") })
	if share.Revoke("browser-b") {
		t.Fatal("failed relay disconnect reported success")
	}
}

func TestHubCloudPresenceAppearsInShareStatus(t *testing.T) {
	share := NewDeviceShare(nil)
	hub := NewHub(HubOptions{Share: share})
	if got := hub.CloudControllerConnected("browser-a", "Web Studio"); got != 1 {
		t.Fatalf("ordinal = %d, want 1", got)
	}
	status := share.Status()
	if len(status.CloudDevices) != 1 || status.CloudDevices[0].ID != "browser-a" {
		t.Fatalf("cloud devices = %+v", status.CloudDevices)
	}
	hub.CloudControllerDisconnected("browser-a")
	if got := share.Status().CloudDevices; len(got) != 0 {
		t.Fatalf("cloud devices after disconnect = %+v", got)
	}
}

func TestCloudPresenceReusesLowestFreeOrdinal(t *testing.T) {
	p := newCloudPresence()
	if p.connect("a", "Web Studio") != 1 || p.connect("b", "Web Studio") != 2 || p.connect("c", "Web Studio") != 3 {
		t.Fatal("simultaneous devices must take 1, 2, 3")
	}
	p.disconnect("a")
	p.disconnect("b")
	if got := p.connect("d", "Web Studio"); got != 1 {
		t.Fatalf("after disconnect ordinal = %d, want lowest free 1", got)
	}
	if got := p.connect("c", "Web Studio"); got != 3 {
		t.Fatalf("reconnect of live id = %d, want it to keep 3", got)
	}
	if got := p.connect("e", "Web Studio"); got != 2 {
		t.Fatalf("hole fill = %d, want 2", got)
	}
}

func TestCloudPresenceSingleDeviceReloadsStayOne(t *testing.T) {
	p := newCloudPresence()
	for i, id := range []string{"c1", "c2", "c3", "c4"} {
		if got := p.connect(id, "Web Studio"); got != 1 {
			t.Fatalf("reload %d ordinal = %d, want 1", i, got)
		}
		p.disconnect(id)
	}
}

func TestCloudPresenceRepeatConnectKeepsIdentityAndEmptyIDDoesNotAllocate(t *testing.T) {
	now := time.Date(2026, 10, 3, 9, 0, 0, 0, time.UTC)
	p := newCloudPresence()
	p.now = func() time.Time { return now }
	p.connect("a", "Web Studio")
	now = now.Add(time.Minute)
	if got := p.connect("a", "Other"); got != 1 {
		t.Fatalf("repeat ordinal = %d", got)
	}
	v := p.views()[0]
	if v.Name != "Web Studio" || !v.LastSeen.Equal(now) || v.ConnectedAt.Equal(now) {
		t.Fatalf("repeat connect view = %+v", v)
	}
	if p.connect("  ", "x") != 0 {
		t.Fatal("empty id allocated")
	}
	p.touch("ghost")
	if len(p.views()) != 1 {
		t.Fatal("touch of unknown id allocated")
	}
}

func TestShareStatusReportsReusedOrdinalsSortedWithHoles(t *testing.T) {
	share := NewDeviceShare(nil)
	hub := NewHub(HubOptions{Share: share})
	hub.CloudControllerConnected("a", "Web Studio")
	hub.CloudControllerConnected("b", "Web Studio")
	hub.CloudControllerConnected("c", "Web Studio")
	hub.CloudControllerDisconnected("a")
	got := share.Status().CloudDevices
	if len(got) != 2 || got[0].ID != "b" || got[0].Ordinal != 2 || got[1].ID != "c" || got[1].Ordinal != 3 {
		t.Fatalf("status with hole = %+v", got)
	}
	hub.CloudControllerConnected("d", "Web Studio")
	got = share.Status().CloudDevices
	if len(got) != 3 || got[0].ID != "d" || got[0].Ordinal != 1 {
		t.Fatalf("status after fill = %+v", got)
	}
}
