package uihub

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"

	"reasonix/internal/contract/event"
	"reasonix/internal/ext/extension/protocol"
)

func TestReviewFrameLimitAlsoAppliesWhenAStatusBecomesAFrame(t *testing.T) {
	h := newTestHub(&eventRecorder{})
	h.DeclareFrames("alpha", framePackage(t))
	client := h.HandlerFor("alpha")
	for i := range maxLiveFrames {
		if _, err := client.Publish(context.Background(), framePublish(fmt.Sprintf("f%d", i), "board", nil)); err != nil {
			t.Fatal(err)
		}
	}
	p := framePublish("was-status", "board", nil)
	p.Kind = protocol.UISurfaceStatus
	p.Payload = mustRaw(t, protocol.UIStatusPayload{Label: "Waiting"})
	if _, err := client.Publish(context.Background(), p); err != nil {
		t.Fatal(err)
	}
	_, err := client.Publish(context.Background(), framePublish("was-status", "board", nil))
	if !errors.Is(err, ErrFrameLimit) {
		t.Fatalf("seventh live frame accepted by replacing a status: %v; surfaces=%d", err, len(h.Surfaces()))
	}
}

func TestFrameLimitIsEnforcedByTheSurfaceCommitOwner(t *testing.T) {
	h := newTestHub(&eventRecorder{})
	h.HandlerFor("alpha")
	h.DeclareFrames("alpha", framePackage(t))
	for i := range maxLiveFrames + 1 {
		accepted, err := h.rememberSurface(&event.ExtensionSurfacePayload{
			PluginID: "alpha", SessionID: "sess-1", Generation: 7,
			SurfaceID: fmt.Sprintf("s%d", i), Kind: event.ExtensionSurfaceFrame,
			Frame: &event.ExtensionFrameView{Frame: "board"},
		})
		if i < maxLiveFrames && (!accepted || err != nil) {
			t.Fatalf("slot %d: %v %v", i, accepted, err)
		}
		if i == maxLiveFrames && (accepted || !errors.Is(err, ErrFrameLimit)) {
			t.Fatalf("commit owner accepted seventh frame: %v %v", accepted, err)
		}
	}
}

func TestFrameSlotsAreReleasedByKindChangesAndRetirement(t *testing.T) {
	for _, retire := range []string{"status", "crash", "generation", "session"} {
		t.Run(retire, func(t *testing.T) {
			h := newTestHub(&eventRecorder{})
			h.DeclareFrames("alpha", framePackage(t))
			client := h.HandlerFor("alpha")
			for i := range maxLiveFrames {
				if _, err := client.Publish(context.Background(), framePublish(fmt.Sprintf("s%d", i), "board", nil)); err != nil {
					t.Fatal(err)
				}
			}
			p := framePublish("replacement", "board", nil)
			switch retire {
			case "status":
				status := framePublish("s0", "board", nil)
				status.Kind = protocol.UISurfaceStatus
				status.Payload = mustRaw(t, protocol.UIStatusPayload{Label: "Waiting"})
				if _, err := client.Publish(context.Background(), status); err != nil {
					t.Fatal(err)
				}
			case "crash":
				h.ClientCrashed("alpha")
				client = h.HandlerFor("alpha")
			case "generation":
				h.BindGeneration("sess-1", 8)
				p.Generation = 8
			case "session":
				h.BindGeneration("sess-2", 7)
				p.SessionID = "sess-2"
			}
			if result, err := client.Publish(context.Background(), p); err != nil || !result.Accepted {
				t.Fatalf("released slot refused: %+v %v", result, err)
			}
		})
	}
}

func TestReviewConcurrentPublicationsCannotReserveTheSameLastFrameSlot(t *testing.T) {
	h := newTestHub(&eventRecorder{})
	h.DeclareFrames("alpha", framePackage(t))
	client := h.HandlerFor("alpha")
	for i := range maxLiveFrames - 1 {
		if _, err := client.Publish(context.Background(), framePublish(fmt.Sprintf("f%d", i), "board", nil)); err != nil {
			t.Fatal(err)
		}
	}
	start := make(chan struct{})
	var wg sync.WaitGroup
	var accepted atomic.Int32
	for i := range 16 {
		wg.Go(func() {
			<-start
			result, err := client.Publish(context.Background(), framePublish(fmt.Sprintf("race-%d", i), "board", nil))
			if err == nil && result.Accepted {
				accepted.Add(1)
			} else if !errors.Is(err, ErrFrameLimit) {
				t.Errorf("publish: %+v %v", result, err)
			}
		})
	}
	close(start)
	wg.Wait()
	if n := len(h.Surfaces()); accepted.Load() != 1 || n != maxLiveFrames {
		t.Fatalf("accepted=%d surfaces=%d", accepted.Load(), n)
	}
}
