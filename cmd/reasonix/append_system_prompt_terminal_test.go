//go:build !windows

package main

import (
	"context"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/creack/pty"
)

func runAppendPromptTUI(t *testing.T, cli *contractCLI) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()
	cmd := cli.command(ctx, "--append-system-prompt-file", cli.promptFile)
	terminal, err := pty.StartWithSize(cmd, &pty.Winsize{Rows: 40, Cols: 120})
	if err != nil {
		t.Fatalf("start interactive terminal: %v", err)
	}
	defer terminal.Close()
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	chunks := make(chan string, 128)
	go func() {
		defer close(chunks)
		buffer := make([]byte, 8192)
		for {
			n, err := terminal.Read(buffer)
			if n > 0 {
				select {
				case chunks <- string(buffer[:n]):
				case <-ctx.Done():
					return
				}
			}
			if err != nil {
				return
			}
		}
	}()
	var output strings.Builder
	waitFor := func(text string) {
		t.Helper()
		for !strings.Contains(output.String(), text) {
			select {
			case chunk, ok := <-chunks:
				if !ok {
					t.Fatalf("TUI closed before %q appeared: %.2000s", text, output.String())
				}
				output.WriteString(chunk)
			case err := <-done:
				t.Fatalf("TUI exited before %q appeared: %v; %.2000s", text, err, output.String())
			case <-ctx.Done():
				t.Fatalf("TUI did not reach %q: %.2000s", text, output.String())
			}
		}
	}
	// The first rendered model label confirms that startup finished before
	// sending a task through the real terminal input loop.
	waitFor("model-a")
	if _, err := terminal.WriteString("first interactive task\r"); err != nil {
		t.Fatal(err)
	}
	waitFor("contract response")
	assertContractRequest(t, cli.request(t), contractPrivatePrompt, "first interactive task")
	if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("TUI shutdown: %v", err)
		}
	case <-ctx.Done():
		t.Fatal("TUI did not shut down after SIGTERM")
	}
	assertContractPrivate(t, output.String(), cli.promptFile)
}
