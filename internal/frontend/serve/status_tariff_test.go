package serve

import (
	"net/http/httptest"
	"testing"
	"time"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/config"
	"reasonix/internal/contract/pricing"
	"reasonix/internal/session/control"
)

func tariffStatus(t *testing.T, tariff func(time.Time) (pricing.Tariff, bool)) map[string]any {
	t.Helper()
	bc := NewBroadcaster()
	ctrl := control.New(control.Options{Sink: bc, SessionDir: testenv.TempDir(t), Tariff: tariff})
	srv := httptest.NewServer(operatorHandler(New(ctrl, bc, config.ServeConfig{})))
	t.Cleanup(srv.Close)
	return readStatus(t, srv)
}

// The window rides /status only where the kernel says a vendor schedule applies,
// and carries the flip time only when there is one ahead.
func TestStatusCarriesTheVendorsWindowOnlyWhereOneApplies(t *testing.T) {
	flip := time.Date(2026, 10, 12, 4, 0, 0, 0, time.UTC)
	got := tariffStatus(t, func(time.Time) (pricing.Tariff, bool) {
		return pricing.Tariff{Peak: true, ChangesAt: flip, Changes: true}, true
	})
	tariff, ok := got["tariff"].(map[string]any)
	if !ok || tariff["peak"] != true || tariff["changesAt"] != "2026-10-12T04:00:00Z" {
		t.Fatalf("status tariff = %v, want peak until 04:00Z", got["tariff"])
	}

	got = tariffStatus(t, func(time.Time) (pricing.Tariff, bool) { return pricing.Tariff{Peak: false}, true })
	tariff, _ = got["tariff"].(map[string]any)
	if _, has := tariff["changesAt"]; tariff["peak"] != false || has {
		t.Fatalf("a schedule with no flip ahead = %v, want off-peak and no changesAt", tariff)
	}

	if got := tariffStatus(t, nil); got["tariff"] != nil {
		t.Fatalf("a model with no vendor schedule carries %v", got["tariff"])
	}
}
