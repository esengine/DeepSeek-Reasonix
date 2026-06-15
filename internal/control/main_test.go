package control

import (
	"os"
	"testing"

	"go.uber.org/goleak"
)

func TestMain(m *testing.M) {
	if os.Getenv("REASONIX_TEST_CODEGRAPH_MCP") == "1" {
		runCodegraphMCPHelper()
		os.Exit(0)
	}
	goleak.VerifyTestMain(m)
}
