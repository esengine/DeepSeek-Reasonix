package serve

import (
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/platform/telemetry"
)

func TestMain(m *testing.M) {
	telemetry.Retired = false
	testenv.RunWithIsolatedUserState(m)
}
