package schedule

import (
	"testing"

	"reasonix/internal/contract/process"
	"reasonix/internal/contract/process/proctest"
)

func TestRunStateMapsOntoProcessStates(t *testing.T) {
	proctest.Total(t, ".", "RunState", func(v string) (process.Class, bool) { return RunState(v).ProcessClass() })
}
