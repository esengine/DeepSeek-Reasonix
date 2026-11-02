package teamroster

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
	"time"

	"reasonix/internal/base/filelock"
)

func TestAvatarUploadOwnsItsFileUntilPublication(t *testing.T) {
	s := Store{Dir: t.TempDir()}
	var a, b string
	_, err := s.Update(t.Context(), func(r *Roster) error {
		a = r.AddAgent(Agent{Name: "Ada", Main: true, CreatedBy: ByUser}).ID
		b = r.AddAgent(Agent{Name: "Ben", CreatedBy: ByUser}).ID
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	unlock, err := filelock.Acquire(t.Context(), filepath.Join(s.Dir, FileName+".lock"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if unlock != nil {
			unlock()
		}
	}()
	done := make(chan error, 1)
	go func() { done <- s.SetAgentPhoto(t.Context(), a, pngBytes) }()
	name := func(data []byte, ext string) string {
		sum := sha256.Sum256(data)
		return hex.EncodeToString(sum[:8]) + ext
	}
	pending, _ := s.AvatarPath(name(pngBytes, ".png"))
	deadline := time.After(3 * time.Second)
	tick := time.NewTicker(time.Millisecond)
	defer tick.Stop()
	for !exists(pending) {
		select {
		case <-deadline:
			t.Fatal("first upload did not reach publication boundary")
		case <-tick.C:
		}
	}
	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()
	if err := s.SetAgentPhoto(ctx, b, gifBytes); err == nil {
		t.Fatal("second writer passed the pending upload")
	}
	blocked, _ := s.AvatarPath(name(gifBytes, ".gif"))
	if _, err := os.Stat(blocked); !os.IsNotExist(err) {
		t.Fatalf("blocked upload wrote its file: %v", err)
	}
	unlock()
	unlock = nil
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if err := s.SetAgentPhoto(t.Context(), b, gifBytes); err != nil {
		t.Fatal(err)
	}
	if !exists(pending) || !exists(blocked) {
		t.Fatal("publishing the second avatar pruned a referenced upload")
	}
}
