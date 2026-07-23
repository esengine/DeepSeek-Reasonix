package bot

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"reasonix/internal/config"
)

// Guards pairingMu + the atomic savePairingFile: concurrent offerPairing
// dispatch goroutines used to load-modify-save pairing.json without a lock and
// overwrite each other's requests. Run with -race.
func TestApproveTelegramPairingUsesConnectionAccess(t *testing.T) {
	t.Setenv("REASONIX_HOME", t.TempDir())
	cfg := config.Default()
	cfg.Bot.Connections = []config.BotConnectionConfig{{
		ID: "telegram-main", Provider: "telegram", Domain: "telegram", Enabled: true,
		Access: config.BotAccessConfig{Enabled: true, PairingEnabled: true},
	}}
	if err := cfg.SaveTo(config.UserConfigPath()); err != nil {
		t.Fatalf("save config: %v", err)
	}
	req, _, err := CreateOrRefreshPairingRequest(InboundMessage{
		Platform: PlatformTelegram, ConnectionID: "telegram-main", Domain: "telegram",
		ChatType: ChatDM, ChatID: "42", UserID: "123", UserName: "alice",
	}, PairingConfig{Enabled: true})
	if err != nil {
		t.Fatalf("create pairing: %v", err)
	}
	if _, err := ApprovePairingCode(req.Code); err != nil {
		t.Fatalf("approve pairing: %v", err)
	}
	got := config.LoadForEdit(config.UserConfigPath())
	access := got.Bot.Connections[0].Access
	if len(access.Users) != 1 || access.Users[0] != "123" || len(access.Admins) != 1 || len(access.Approvers) != 1 {
		t.Fatalf("Telegram connection access = %+v", access)
	}
	if len(got.Bot.Allowlist.TelegramUsers) != 0 {
		t.Fatalf("legacy Telegram allowlist unexpectedly changed: %+v", got.Bot.Allowlist.TelegramUsers)
	}
}

func TestCreateOrRefreshPairingRequestConcurrent(t *testing.T) {
	t.Setenv("REASONIX_HOME", t.TempDir())
	cfg := PairingConfig{Enabled: true, RequestTTL: time.Hour, MaxPendingPerPlatform: 64}

	const workers = 8
	var wg sync.WaitGroup
	errs := make(chan error, workers)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			msg := InboundMessage{
				Platform: PlatformFeishu,
				ChatType: ChatDM,
				ChatID:   fmt.Sprintf("chat-%d", i),
				UserID:   fmt.Sprintf("user-%d", i),
			}
			for j := 0; j < 5; j++ {
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
