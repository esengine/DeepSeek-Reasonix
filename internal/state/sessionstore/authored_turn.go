package sessionstore

import (
	"reasonix/internal/contract/provider"
)

// AuthoredTurnIdentity is what a host published for the message a turn is
// about, before any work that could produce it: the turn number, the session
// index it named, and the raw authored text both were derived from.
type AuthoredTurnIdentity struct {
	AuthoredTurn int
	MsgIndex     int
	Raw          string
}

// ClassifyTurn answers which authored turn msg belongs to, given the turns
// closed before it. Both projections of that number derive it here: the
// display-index sidecar counts a whole transcript with it, and a live turn
// start names its own pending message with it.
func ClassifyTurn(msg provider.Message, priorTurn int) TurnClassification {
	class := TurnClassification{AuthoredTurn: priorTurn}
	if msg.Role != provider.RoleUser {
		return class
	}
	content := UserMessageText(msg)
	if IsUserAuthoredTurn(content) {
		class.AuthoredTurn = priorTurn + 1
		class.StartsTurn = true
		return class
	}
	if _, isSteer := SteerText(content); isSteer {
		class.Steer = true
	} else if IsSyntheticUserText(content) {
		class.Synthetic = true
	}
	return class
}

// TurnClassification names the authored turn a message belongs to. StartsTurn
// marks the one that opens it; Steer and Synthetic say why a user message that
// opened none is still a user message.
type TurnClassification struct {
	AuthoredTurn int
	StartsTurn   bool
	Steer        bool
	Synthetic    bool
}
