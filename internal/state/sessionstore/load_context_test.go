package sessionstore

import (
	"context"
	"errors"
	"path/filepath"
	"reasonix/internal/base/testenv"
	"testing"
	"time"
)

func TestLoadSessionContextCancelsWhileSaveOwnsPath(t *testing.T) {
	path := filepath.Join(testenv.TempDir(t), "session.jsonl")
	if err := NewSession("system").Save(path); err != nil {
		t.Fatal(err)
	}
	unlock := lockSessionSavePath(path)
	defer unlock()
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() { _, err := LoadSessionContext(ctx, path); result <- err }()
	cancel()
	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("error=%v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("canceled reader still waiting on save")
	}
}

func TestCanceledReplayDoesNotReturnDamagedPrefix(t *testing.T) {
	path := filepath.Join(testenv.TempDir(t), "session.jsonl")
	if err := NewSession("system").Save(path); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	limits := defaultSessionReplayLimits
	limits.ctx = ctx
	_, _, damaged, err := loadSessionMessagesWithLimits(path, limits)
	if !errors.Is(err, context.Canceled) || damaged {
		t.Fatalf("damaged=%v err=%v", damaged, err)
	}
}
