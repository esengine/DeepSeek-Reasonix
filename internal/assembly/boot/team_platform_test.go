package boot

import (
	"errors"
	"testing"

	"reasonix/internal/session/team"
)

func requireTeamExecution(t *testing.T) {
	t.Helper()
	if err := team.CheckPlatform(); err != nil {
		t.Skip(err)
	}
}

func TestTeamBuildRefusesUnsupportedPlatformBeforeStartingRuntime(t *testing.T) {
	if team.CheckPlatform() == nil {
		t.Skip("the current platform supports team seats")
	}
	ctrl, err := Build(t.Context(), Options{TeamSeat: &fakeTeamSeat{}})
	if ctrl != nil || !errors.Is(err, team.ErrUnsupportedPlatform) {
		t.Fatalf("team build: %v, %v", ctrl, err)
	}
}
