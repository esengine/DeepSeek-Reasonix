package protocol

import (
	"bytes"
	"encoding/json"
	"fmt"

	"reasonix/internal/ext/extensioncontract"
)

// Frame limits are part of the contract: a frontend renders the fallback where
// it cannot draw a frame, and the props cross to an isolated document as-is.
const (
	MaxFrameFallbackBytes = 600
	MaxFramePropsBytes    = 64 << 10
)

// UIFramePayload names a document the plugin package ships and the data it is
// shown with. Markup never travels here: the host serves the declared file
// from the installed package, so this payload carries only JSON.
type UIFramePayload struct {
	Frame string `json:"frame" validate:"nonempty"`
	// Slot is where the frame would like to stand, as for a view.
	Slot  string `json:"slot,omitempty"`
	Title string `json:"title" validate:"nonempty"`
	// Fallback is what a frontend that cannot draw a frame shows instead.
	Fallback string          `json:"fallback" validate:"nonempty"`
	Props    json.RawMessage `json:"props,omitempty"`
}

// Validate checks the identity, the bounds, and that props is an object.
func (p UIFramePayload) Validate() error {
	if !extensioncontract.ValidFrameID(p.Frame) {
		return fmt.Errorf("frame id %q is not a lowercase slug of at most 32 characters", p.Frame)
	}
	if len(p.Slot) > MaxViewSlotID {
		return fmt.Errorf("frame slot is longer than %d characters", MaxViewSlotID)
	}
	if len(p.Title) > MaxViewLabel {
		return fmt.Errorf("frame title is longer than %d characters", MaxViewLabel)
	}
	if len(p.Fallback) > MaxFrameFallbackBytes {
		return fmt.Errorf("frame fallback is longer than %d bytes", MaxFrameFallbackBytes)
	}
	if len(p.Props) == 0 {
		return nil
	}
	if len(p.Props) > MaxFramePropsBytes {
		return fmt.Errorf("frame props are larger than %d bytes", MaxFramePropsBytes)
	}
	if trimmed := bytes.TrimSpace(p.Props); len(trimmed) == 0 || trimmed[0] != '{' {
		return fmt.Errorf("frame props must be a JSON object")
	}
	return nil
}
