package extensioncontract

import (
	"strings"
	"testing"
)

func TestFrameIDsAreLowercaseSlugs(t *testing.T) {
	for _, id := range []string{"a", "board", "task-board-2", strings.Repeat("a", 32)} {
		if !ValidFrameID(id) {
			t.Errorf("%q refused", id)
		}
	}
	for _, id := range []string{"", "2board", "-x", "Board", "a_b", "a.b", "../x", strings.Repeat("a", 33)} {
		if ValidFrameID(id) {
			t.Errorf("%q accepted", id)
		}
	}
}
