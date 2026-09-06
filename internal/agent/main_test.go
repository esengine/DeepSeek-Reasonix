package agent

import (
	"context"
	"testing"
	"time"

	"go.uber.org/goleak"
)

func TestMain(m *testing.M) {
	// Stream body retries use multi-second backoff in production; collapse it
	// in package tests so recovery suites stay deterministic and fast.
	streamRetrySleep = func(ctx context.Context, _ int) bool {
		return ctx.Err() == nil
	}
	recoverySleep = func(ctx context.Context, _ time.Duration) bool { return ctx.Err() == nil }
	goleak.VerifyTestMain(m)
}
