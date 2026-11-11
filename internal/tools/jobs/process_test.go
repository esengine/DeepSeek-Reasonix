package jobs

import (
	"testing"

	"reasonix/internal/contract/process"
	"reasonix/internal/contract/process/proctest"
)

func TestStatusMapsOntoProcessStates(t *testing.T) {
	proctest.Total(t, ".", "Status", func(v string) (process.Class, bool) { return Status(v).ProcessClass() })
}
