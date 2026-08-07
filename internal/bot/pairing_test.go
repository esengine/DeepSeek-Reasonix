package bot

import (
	"fmt"
	"sync"
	"testing"
	"time"
)

// Guards pairingMu + the atomic savePairingFile: concurrent offerPairing
// dispatch goroutines used to load-modify-save pairing.json without a lock and
// overwrite each other's requests. Run with -race.
func TestCreateOrRefreshPairingRequestConcurrent(t *testing.T) {
	t.Setenv("REASONIX_HOME", t.TempDir())
	cfg := PairingConfig{Enabled: true, RequestTTL: time.Hour, MaxPendingPerPlatform: 64}

	const workers = 8
	var wg sync.WaitGroup
	errs := make(chan error, workers)
	for i := range workers {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			msg := InboundMessage{
				Platform: PlatformFeishu,
				ChatType: ChatDM,
				ChatID:   fmt.Sprintf("chat-%d", i),
				UserID:   fmt.Sprintf("user-%d", i),
			}
			for range 5 {
				if _, _, err := CreateOrRefreshPairingRequest(msg, cfg); err != nil {
					errs <- err
					return
				}
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatalf("concurrent pairing request failed: %v", err)
	}

	reqs, err := ListPairingRequests()
	if err != nil {
		t.Fatalf("list pairing requests: %v", err)
	}
	if len(reqs) != workers {
		t.Fatalf("pairing store lost concurrent writes: got %d requests, want %d", len(reqs), workers)
	}
}
