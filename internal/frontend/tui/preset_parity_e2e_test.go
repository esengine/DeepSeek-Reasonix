package tui_test

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestRetiredPresetNoticeThroughKernel(t *testing.T) {
	c := inProcessKernel(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	stream := c.Subscribe(ctx)
	for _, command := range []string{"/preset delivery", "/work-mode balanced", "/profile light"} {
		if err := c.Submit(ctx, command); err != nil {
			t.Fatal(err)
		}
		found := false
		for !found {
			select {
			case update := <-stream:
				if update.Event.Kind == "notice" && strings.Contains(update.Event.Text, "Execution modes have been retired.") {
					found = true
				}
			case <-ctx.Done():
				t.Fatalf("%s: retirement notice did not reach frontend", command)
			}
		}
	}
}
