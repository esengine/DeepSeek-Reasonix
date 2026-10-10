package jobs

import (
	"os"
	"testing"

	"go.uber.org/goleak"
)

func TestMain(m *testing.M) {
	if os.Getenv(jobsHolderEnv) != "" {
		mgr := NewManager(nil)
		os.Stdout.WriteString(mgr.scratch.Path() + "\n")
		select {}
	}
	goleak.VerifyTestMain(m)
}
