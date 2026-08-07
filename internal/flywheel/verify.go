package flywheel

import (
	"context"
	"os/exec"
	"strings"
	"time"
)

// VerifyRun executes a verification command in dir and returns the Verify
// result — the real source for Trajectory.Verify (docs/DATA_FLYWHEEL.md §2.3).
// kind ∈ {"go_test","go_build","build","smoke"}; cmd is the argv, e.g.
// {"go","test","./..."} or {"python","-m","pytest"}.
func VerifyRun(ctx context.Context, dir, kind string, cmd []string, timeout time.Duration) *Verify {
	if ctx == nil {
		ctx = context.Background()
	}
	if len(cmd) == 0 {
		return &Verify{Kind: kind, OK: false, Detail: "no command"}
	}
	if timeout <= 0 {
		timeout = 2 * time.Minute
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	c := exec.CommandContext(runCtx, cmd[0], cmd[1:]...)
	c.Dir = dir
	out, err := c.CombinedOutput()
	detail := strings.TrimSpace(string(out))
	if len(detail) > 2048 {
		detail = detail[:2048]
	}
	v := &Verify{Kind: kind, OK: err == nil && runCtx.Err() == nil, Detail: detail}
	if err != nil && runCtx.Err() != nil {
		v.Detail = "timeout: " + v.Detail
	}
	return v
}

// GoTestVerify is a convenience for the most common verification.
func GoTestVerify(ctx context.Context, dir string, timeout time.Duration) *Verify {
	return VerifyRun(ctx, dir, "go_test", []string{"go", "test", "./...", "-count=1", "-short"}, timeout)
}
