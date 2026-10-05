package skill

import (
	"testing"

	"reasonix/internal/base/testenv"
)

func TestMain(m *testing.M) {
	testenv.RunWithIsolatedUserState(m)
}
