package team

import (
	"context"
	"time"
)

// SeatRef names a session an agent speaks through: its own chat when Group is
// empty, otherwise its seat in that group.
type SeatRef struct{ Agent, Group string }

// DeliveryKind says what a delivery carries.
type DeliveryKind string

const (
	DeliverMessage   DeliveryKind = "message"
	DeliverReply     DeliveryKind = "reply"
	DeliverGroupTurn DeliveryKind = "group_turn"
	DeliverSchedule  DeliveryKind = "schedule"
)

// Delivery is one item the host queues in a seat's inbox. Key makes it
// idempotent across retries and restarts. Serving names the exchanges the turn
// it starts answers and Step the group turn it is; the host hands both back in
// that turn's Settlement.
type Delivery struct {
	Key      string
	Kind     DeliveryKind
	From     string
	Exchange string
	Group    string
	Step     string
	Display  string
	Text     string
	Serving  []string
	Steer    bool
	Ceiling  string
}

// Panes is the host's view of the sessions agents speak through.
type Panes interface {
	// Ensure opens the seat's session, or returns the one already open.
	Ensure(ctx context.Context, seat SeatRef) (Pane, error)
	// Live returns the seat's session only if it is open now.
	Live(seat SeatRef) (Pane, bool)
}

// Pane is one open session.
type Pane interface {
	Deliver(d Delivery) error
	Cancel()
}

// Outcome is how a turn ended, as the host observed it.
type Outcome string

const (
	OutcomeCompleted   Outcome = "completed"
	OutcomeFailed      Outcome = "failed"
	OutcomeCancelled   Outcome = "cancelled"
	OutcomeInterrupted Outcome = "interrupted"
)

// Settlement is the host's report of a turn: the exchanges it served, the
// exchanges whose replies it read (Consumed), the group turns it was, how it
// ended, why when it did not complete, and the answer it ended with.
type Settlement struct {
	Serving   []string
	Consumed  []string
	Steps     []string
	Outcome   Outcome
	Code      string
	FinalText string
}

// ObservationKind is a kind of activity the host saw in a seat's session.
type ObservationKind string

const (
	ObserveEvent        ObservationKind = "event"
	ObserveToolStarted  ObservationKind = "tool_started"
	ObserveToolFinished ObservationKind = "tool_finished"
)

// Observation is one piece of activity the host saw, which the group round's
// timers are judged on.
type Observation struct {
	Kind ObservationKind
	At   time.Time
}

// Keys of the inbox Extra a team delivery is stored under, so the turn that
// runs it can tell the team what it served.
const (
	ExtraKind     = "team.kind"
	ExtraExchange = "team.exchange"
	ExtraServing  = "team.serving"
	ExtraStep     = "team.step"
	ExtraGroup    = "team.group"
	ExtraFrom     = "team.from"
	ExtraCeiling  = "team.ceiling"
)

// Seat is one session's place on the team: the service, and which agent, in
// which group, the session speaks for.
type Seat struct {
	Service *Service
	Ref     SeatRef
}

// Projection renders the seat's standing blocks.
func (s Seat) Projection() (Projection, error) { return s.Service.Projection(s.Ref) }

// Team returns the service and the seat's reference.
func (s Seat) Team() (*Service, SeatRef) { return s.Service, s.Ref }
