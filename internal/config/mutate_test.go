package config

import (
	"fmt"
	"sync"
	"testing"
)

// TestLockUserConfigEditsSerializesRMW drives concurrent load-modify-save
// cycles through the edit lock and checks no writer's change is dropped.
// Without the lock, two editors load the same base config, each append their
// own connection, and the second save silently erases the first one's entry —
// the bot auto-session persistence vs. settings-save race this lock exists for.
func TestLockUserConfigEditsSerializesRMW(t *testing.T) {
	// Point the user config at a temp home: SaveTo renders bot connections only
	// for user-scope paths (project configs save incrementally without them).
	home := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	path := UserConfigPath()
	if path == "" {
		t.Fatal("UserConfigPath is empty with REASONIX_HOME set")
	}

	const writers = 8
	var wg sync.WaitGroup
	for i := 0; i < writers; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			unlock := LockUserConfigEdits()
			defer unlock()
			cfg := LoadForEdit(path)
			cfg.Bot.Connections = append(cfg.Bot.Connections, BotConnectionConfig{
				ID:       fmt.Sprintf("conn-%d", n),
				Provider: "qq",
				Enabled:  true,
			})
			if err := cfg.SaveTo(path); err != nil {
				t.Errorf("save: %v", err)
			}
		}(i)
	}
	wg.Wait()

	cfg := LoadForEdit(path)
	if got := len(cfg.Bot.Connections); got != writers {
		t.Fatalf("connections = %d, want %d (concurrent read-modify-write dropped updates)", got, writers)
	}
}

// TestConcurrentBotAndSettingsWritersKeepBothFields reproduces the reviewed
// P1 scenario: a bot auto-session mapping writer and a settings writer race
// on the user config. Both hold LockUserConfigEdits around their
// load-modify-save cycle, so neither may ever overwrite the other's field
// with a stale copy. Each writer also checks, under the lock, that its own
// previous round survived — any single lost update fails the test, not just
// one on the final round. Fault check: removing either writer's lock/unlock
// pair makes this test fail (at least intermittently) with "previous ...
// update lost".
func TestConcurrentBotAndSettingsWritersKeepBothFields(t *testing.T) {
	home := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	path := UserConfigPath()
	if path == "" {
		t.Fatal("UserConfigPath is empty with REASONIX_HOME set")
	}

	const rounds = 40
	start := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(2)

	// Bot mapping writer: rewrites Bot.Connections like the botruntime
	// auto-session persistence path.
	go func() {
		defer wg.Done()
		<-start
		for i := 1; i <= rounds; i++ {
			unlock := LockUserConfigEdits()
			cfg := LoadForEdit(path)
			if i > 1 {
				wantID := fmt.Sprintf("conn-%d", i-1)
				if len(cfg.Bot.Connections) != 1 || cfg.Bot.Connections[0].ID != wantID {
					unlock()
					t.Errorf("round %d: previous bot update lost: got %+v, want single connection %q", i, cfg.Bot.Connections, wantID)
					return
				}
			}
			cfg.Bot.Connections = []BotConnectionConfig{{
				ID:       fmt.Sprintf("conn-%d", i),
				Provider: "feishu",
				Enabled:  true,
			}}
			err := cfg.SaveTo(path)
			unlock()
			if err != nil {
				t.Errorf("bot writer save: %v", err)
				return
			}
		}
	}()

	// Settings writer: bumps an agent field like a desktop settings-page save.
	go func() {
		defer wg.Done()
		<-start
		for i := 1; i <= rounds; i++ {
			unlock := LockUserConfigEdits()
			cfg := LoadForEdit(path)
			if i > 1 && cfg.Agent.MaxSteps != i-1 {
				unlock()
				t.Errorf("round %d: previous settings update lost: MaxSteps = %d, want %d", i, cfg.Agent.MaxSteps, i-1)
				return
			}
			cfg.Agent.MaxSteps = i
			err := cfg.SaveTo(path)
			unlock()
			if err != nil {
				t.Errorf("settings writer save: %v", err)
				return
			}
		}
	}()

	close(start)
	wg.Wait()
	if t.Failed() {
		return
	}

	final := LoadForEdit(path)
	wantID := fmt.Sprintf("conn-%d", rounds)
	if len(final.Bot.Connections) != 1 || final.Bot.Connections[0].ID != wantID {
		t.Fatalf("bot writer's last update lost: got %+v, want single connection %q", final.Bot.Connections, wantID)
	}
	if final.Agent.MaxSteps != rounds {
		t.Fatalf("settings writer's last update lost: MaxSteps = %d, want %d", final.Agent.MaxSteps, rounds)
	}
}
