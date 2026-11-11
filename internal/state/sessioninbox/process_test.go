package sessioninbox

import (
	"testing"

	"reasonix/internal/contract/process"
	"reasonix/internal/contract/process/proctest"
)

func TestInboxStateMapsOntoProcessStates(t *testing.T) {
	proctest.Total(t, ".", "InboxState", func(v string) (process.Class, bool) { return InboxState(v).ProcessClass() })
}
