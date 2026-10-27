package isolation

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"reasonix/internal/contract/tool"
)

func TestSettlementFenceProcessHelper(t *testing.T) {
	for i, arg := range os.Args {
		if arg != "--settlement-fence-helper" {
			continue
		}
		if len(os.Args) != i+4 {
			os.Exit(2)
		}
		s := NewStore(os.Args[i+1], os.Args[i+2], opened(t, os.Args[i+2]))
		_, err := s.Apply(t.Context(), os.Args[i+3])
		var refusal tool.Refusal
		if !errors.As(err, &refusal) || refusal.Code != CodeUnavailable || !strings.Contains(err.Error(), "settlement") {
			fmt.Fprintln(os.Stderr, "unexpected Apply outcome:", err)
			os.Exit(3)
		}
		fmt.Println("fenced")
		os.Exit(0)
	}
}

func TestSettlementFenceStopsANewProcessBeforeSourceMutation(t *testing.T) {
	s, e := finishedOperationResult(t)
	publishSettlementFence(t, s, e.ID, []byte(`{"version":999}`))
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, executable, "-test.run=^TestSettlementFenceProcessHelper$", "--",
		"--settlement-fence-helper", s.managedRoot, s.workspace, e.ID)
	out, err := cmd.CombinedOutput()
	if err != nil || strings.TrimSpace(string(out)) != "fenced" {
		t.Fatalf("child did not refuse fenced Apply: %v\n%s", err, out)
	}
	if got := read(t, s.workspace, "a.txt"); got != "one\n" {
		t.Fatalf("fenced child wrote source: %q", got)
	}
}
