package boot

import (
	"testing"

	"reasonix/internal/testenv"
)

func TestMain(m *testing.M) {
	testenv.RunWithIsolatedUserState(m)
}
