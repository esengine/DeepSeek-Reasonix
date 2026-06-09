package clipboard

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

var errUnsupported = fmt.Errorf("clipboard not supported on this platform")

var readCmd, writeCmd []string

const cmdTimeout = 2 * time.Second

func timeout(d time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), d)
}

func Read() (string, error) {
	if len(readCmd) == 0 {
		return "", errUnsupported
	}
	ctx, cancel := timeout(cmdTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, readCmd[0], readCmd[1:]...)
	out, err := cmd.Output()
	if err != nil {
		if ctx.Err() != nil {
			return "", fmt.Errorf("clipboard read timed out after %v", cmdTimeout)
		}
		return "", fmt.Errorf("clipboard read (%s) failed: %w", strings.Join(readCmd, " "), err)
	}
	return string(out), nil
}

func Write(text string) error {
	if len(writeCmd) == 0 {
		return errUnsupported
	}
	ctx, cancel := timeout(cmdTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, writeCmd[0], writeCmd[1:]...)
	cmd.Stdin = strings.NewReader(text)
	out, err := cmd.CombinedOutput()
	if err != nil {
		if ctx.Err() != nil {
			return fmt.Errorf("clipboard write timed out after %v", cmdTimeout)
		}
		return fmt.Errorf("clipboard write (%s) failed: %w%s", strings.Join(writeCmd, " "), err, suffix(out))
	}
	return nil
}

func suffix(out []byte) string {
	if len(out) == 0 {
		return ""
	}
	return ": " + strings.TrimSpace(string(out))
}

func Probe() error {
	return Write("")
}
