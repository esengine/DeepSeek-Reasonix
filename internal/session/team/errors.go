package team

import "errors"

var (
	// ErrConfigurationApproval is a standing change without fresh human approval.
	ErrConfigurationApproval = errors.New("changing standing configuration requires user approval")
	// ErrSelf is an agent messaging itself.
	ErrSelf = errors.New("an agent cannot message itself")
	// ErrEmptyMessage is a message with no text.
	ErrEmptyMessage = errors.New("message is empty")
	// ErrMessageTooLong is a message past MaxMessageRunes.
	ErrMessageTooLong = errors.New("message is too long")
	// ErrUpstream is work handed to an agent that is waiting on the sender.
	ErrUpstream = errors.New("that agent is waiting on this work; answer it instead of messaging it")
	// ErrDepth is a handoff chain already as deep as MaxDepth.
	ErrDepth = errors.New("this work has been handed on as many times as allowed")
	// ErrOpenLimit is a sender with MaxOpenPerSender messages still unanswered.
	ErrOpenLimit = errors.New("too many messages from this agent are still unanswered")
	// ErrRateLimited is a sender past its send rate or its fan-out for one turn.
	ErrRateLimited = errors.New("this agent is sending messages too fast")
	// ErrNotInGroup is a group-turn tool called outside a group turn.
	ErrNotInGroup = errors.New("this is not a turn in a group")
	// ErrNotYourTurn is a group-turn tool called by a member that is not speaking.
	ErrNotYourTurn = errors.New("it is not this member's turn in the group")
	// ErrHandSize is hand_turn naming no one, or more than MaxHand members.
	ErrHandSize = errors.New("hand the turn to between one and four members")
	// ErrNotMain is a call only a main agent may make.
	ErrNotMain = errors.New("only a main agent can do that")
	// ErrSelfOnly is a member changing an agent other than itself.
	ErrSelfOnly = errors.New("an agent that is not a main agent can only change itself")
	// ErrNotAdmin is a change to a group by an agent that is neither its admin nor a main agent.
	ErrNotAdmin = errors.New("only the group's admin or a main agent can change the group")
	// ErrCreateLimit is a turn past MaxCreatesPerTurn new agents.
	ErrCreateLimit = errors.New("too many agents created in one turn")
	// ErrArchived is a post to a group that has been archived.
	ErrArchived = errors.New("the group is archived")
	// ErrUnavailable is a receiver whose session could not be opened or reached.
	ErrUnavailable = errors.New("that agent's session could not be reached")
)
