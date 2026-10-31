package teamroster

import (
	"errors"
	"fmt"
)

var (
	// ErrAgentCapacity bounds the roster and the projection each agent receives.
	ErrAgentCapacity = errors.New("the team has reached its agent capacity")
	// ErrGroupCapacity bounds how many groups the team stores.
	ErrGroupCapacity = errors.New("the team has reached its group capacity")
	// ErrEmpty is a required field left blank.
	ErrEmpty = errors.New("required field is empty")
	// ErrTooLong is a field past its length limit.
	ErrTooLong = errors.New("field is too long")
	// ErrInvalid is a field whose value this build does not accept.
	ErrInvalid = errors.New("field value is invalid")
	// ErrNameTaken is a name another agent, or another group, already uses.
	ErrNameTaken = errors.New("name is already taken")
	// ErrUnknownAgent is a reference to an agent the roster does not hold.
	ErrUnknownAgent = errors.New("no such agent")
	// ErrUnknownGroup is a reference to a group the roster does not hold.
	ErrUnknownGroup = errors.New("no such group")
	// ErrUnknownSchedule is a reference to a scheduled task the team does not hold.
	ErrUnknownSchedule = errors.New("no such scheduled task")
	// ErrNotMember is a group admin, or a member session, outside the group.
	ErrNotMember = errors.New("agent is not a member of the group")
	// ErrGroupSize is a group outside MinMembers..MaxMembers.
	ErrGroupSize = errors.New("group size is out of range")
	// ErrLastMain is a change that would leave a non-empty team with no main agent.
	ErrLastMain = errors.New("the team needs at least one main agent")
	// ErrUnknownExchange is a reference to an exchange the book does not hold.
	ErrUnknownExchange = errors.New("no such exchange")
	// ErrExchangeSettled is a state change asked of an exchange that already ended.
	ErrExchangeSettled = errors.New("exchange has already settled")
	// ErrRequestReused is a request id the sender already used for a different message.
	ErrRequestReused = errors.New("request id was already used for a different message")
	// ErrConflict is an update held to a revision the roster has moved past.
	ErrConflict = errors.New("roster changed since it was read")
	// ErrCorrupt is a roster file that does not decode or does not validate.
	ErrCorrupt = errors.New("roster file is corrupt")
	// ErrNewerSchema is a roster written by a newer build; it reads but refuses writes.
	ErrNewerSchema = errors.New("roster was written by a newer version")
	// ErrReadOnly is a second process asking to be the team's writer.
	ErrReadOnly = errors.New("the team is open for writing in another process")
)

// FieldError names the field a validation failure is about. Its cause is one
// of ErrEmpty, ErrTooLong, ErrInvalid or ErrNameTaken.
type FieldError struct {
	Field string
	Err   error
}

func (e *FieldError) Error() string { return fmt.Sprintf("%s: %v", e.Field, e.Err) }

func (e *FieldError) Unwrap() error { return e.Err }

func fieldErr(field string, err error) error { return &FieldError{Field: field, Err: err} }
