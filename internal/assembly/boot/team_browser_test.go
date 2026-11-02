package boot

import (
	"strings"
	"testing"

	"reasonix/internal/session/team"
)

type seatAt struct{ ref team.SeatRef }

func (s seatAt) Projection() (team.Projection, error) { return team.Projection{}, nil }
func (s seatAt) Team() (*team.Service, team.SeatRef)  { return nil, s.ref }

// One agent's logins are never another's, while every seat of one agent — its
// own chat and each group — shares that agent's.
func TestAnAgentsBrowserRunsOnItsOwnProfile(t *testing.T) {
	root := "/work/launch"
	plain := browserProfileKey(root, nil)
	ada := browserProfileKey(root, seatAt{team.SeatRef{Agent: "a-0123456789abcdef"}})
	adaInGroup := browserProfileKey(root, seatAt{team.SeatRef{Agent: "a-0123456789abcdef", Group: "g-0123456789abcdef"}})
	ben := browserProfileKey(root, seatAt{team.SeatRef{Agent: "a-fedcba9876543210"}})
	if ada == plain || ada == ben || ada != adaInGroup {
		t.Fatalf("profiles: plain %q, Ada %q, Ada in a group %q, Ben %q", plain, ada, adaInGroup, ben)
	}
	if strings.ContainsAny(ada, `/\:`) {
		t.Fatalf("an agent's profile name is not one path segment: %q", ada)
	}
}
