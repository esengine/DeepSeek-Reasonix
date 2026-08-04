package cli

import (
	"slices"
	"testing"

	"charm.land/bubbles/v2/textarea"
)

func TestComposerWordMotionAcceptsCtrlArrows(t *testing.T) {
	ti := textarea.New()
	configureChatTextarea(&ti)

	for _, tc := range []struct {
		motion string
		keys   []string
		ctrl   string
		alt    string
	}{
		{"forward", ti.KeyMap.WordForward.Keys(), "ctrl+right", "alt+right"},
		{"backward", ti.KeyMap.WordBackward.Keys(), "ctrl+left", "alt+left"},
	} {
		if !slices.Contains(tc.keys, tc.ctrl) {
			t.Errorf("word %s is missing %s: %v", tc.motion, tc.ctrl, tc.keys)
		}
		if !slices.Contains(tc.keys, tc.alt) {
			t.Errorf("word %s dropped %s: %v", tc.motion, tc.alt, tc.keys)
		}
	}
}
