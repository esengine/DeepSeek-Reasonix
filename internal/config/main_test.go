package config

import (
	"os"
	"testing"

	"reasonix/internal/testenv"
)

func TestMain(m *testing.M) {
	if os.Getenv("REASONIX_CONFIG_LOCK_HELPER") == "1" {
		os.Exit(m.Run())
	}
	testenv.RunWithIsolatedUserState(m)
}
