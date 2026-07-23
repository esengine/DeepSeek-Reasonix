package serve

import (
	"os/exec"
	"testing"
)

func TestServeIndexBrowserBehavior(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("Node.js is optional outside desktop development; run `node --test index_behavior_test.mjs` for the browser behavior contract")
	}
	cmd := exec.Command(node, "--test", "index_behavior_test.mjs")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("node browser behavior harness: %v\n%s", err, out)
	}
}
