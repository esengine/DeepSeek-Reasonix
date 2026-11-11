package taskmonitor

import (
	"testing"

	"reasonix/internal/contract/process"
	"reasonix/internal/contract/process/proctest"
)

func TestTaskStateMapsOntoProcessStates(t *testing.T) {
	proctest.Total(t, ".", "TaskState", func(v string) (process.Class, bool) { return TaskState(v).ProcessClass() })
}
