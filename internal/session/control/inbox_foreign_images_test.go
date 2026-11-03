package control

import (
	"context"
	"encoding/json"
	"testing"

	"reasonix/internal/state/sessioninbox"
)

func TestAQueuedLineCarryingOneXImagesIsHeldNotSentWithoutThem(t *testing.T) {
	c := pauseTestController(t)
	st, err := c.ensureInbox()
	if err != nil {
		t.Fatal(err)
	}
	rec, err := st.Enqueue(sessioninbox.EnqueueRequest{Intent: sessioninbox.IntentFollowup, Source: "test", Envelope: sessioninbox.PromptEnvelope{
		DisplayText: "what is in this picture", SubmitText: "what is in this picture",
		ImageInputs: json.RawMessage(`[{"kind":"attachment","attachment":{"content":{"sha256":"ab"},"name":"shot.png"}}]`),
	}})
	if err != nil {
		t.Fatal(err)
	}
	if err := c.RunInboxTurn(context.Background(), rec.ItemID); err == nil {
		t.Fatal("a line whose images this build cannot read was run")
	}
	meta, _, err := st.ReadItem(rec.ItemID)
	if err != nil {
		t.Fatal(err)
	}
	if meta.State != sessioninbox.StateBlocked || meta.BlockCode != sessioninbox.BlockForeignImages {
		t.Fatalf("item = state %q code %q, want blocked with %q", meta.State, meta.BlockCode, sessioninbox.BlockForeignImages)
	}
}

func TestSteeringWithOneXImagesIsRefused(t *testing.T) {
	env := sessioninbox.PromptEnvelope{SubmitText: "x", ImageInputs: json.RawMessage(`[{"kind":"url","url":"https://example.test/a.png"}]`)}
	if _, _, block, err := applyInboxReferences(env); err != nil || block == "" {
		t.Fatalf("block = %q, err = %v; want a stated block", block, err)
	}
}
