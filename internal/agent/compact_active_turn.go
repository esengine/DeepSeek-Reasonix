package agent

import (
	"slices"

	"reasonix/internal/provider"
)

// activeTurnKeepRounds is how many of the active turn's newest assistant
// rounds stay verbatim when an overflow forces a fold inside the turn.
const activeTurnKeepRounds = 2

// activeTurnFoldBoundary returns where an overflow rescue may end its fold
// inside the active turn: after the prompt and every completed round except
// the newest keep rounds, on a replay-safe unit boundary. A turn with too few
// rounds to split returns active, keeping the whole turn verbatim.
func activeTurnFoldBoundary(msgs []provider.Message, active, end int) int {
	if active < 0 || end <= active+1 || end > len(msgs) {
		return active
	}
	body := msgs[active+1 : end]
	keep := activeTurnKeepRounds
	for _, unit := range slices.Backward(extractMessageUnits(body)) {
		if body[unit.lo].Role != provider.RoleAssistant {
			continue
		}
		keep--
		if keep < 0 {
			return active + 1 + unit.hi
		}
	}
	return active
}
