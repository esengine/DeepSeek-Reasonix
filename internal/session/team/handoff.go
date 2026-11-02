package team

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"reasonix/internal/state/teamroster"
)

// MessageAgent sends req from the calling agent and returns once the receiver's
// inbox holds it; the answer arrives later, as a reply delivered to the seat
// the call came from.
func (s *Service) MessageAgent(ctx context.Context, c Caller, req MessageRequest) (MessageReceipt, error) {
	defer s.changed()
	if strings.TrimSpace(req.Message) == "" {
		return MessageReceipt{}, ErrEmptyMessage
	}
	if utf8.RuneCountInString(req.Message) > MaxMessageRunes {
		return MessageReceipt{}, ErrMessageTooLong
	}
	if req.ReplyTo == "" {
		req.ReplyTo = teamroster.ReplyToSender
	}
	r, err := s.store.Load()
	if err != nil {
		return MessageReceipt{}, err
	}
	from, to, err := resolvePair(r, c, req.To)
	if err != nil {
		return MessageReceipt{}, err
	}
	book, err := s.store.Exchanges()
	if err != nil {
		return MessageReceipt{}, err
	}
	if !retried(book, from.ID, req.RequestID) {
		if err := s.admit(book, c, from.ID, to.ID); err != nil {
			return MessageReceipt{}, err
		}
	}
	var parent string
	if n := len(c.Serving); n > 0 {
		parent = c.Serving[n-1]
	}
	x, existed, err := s.store.OpenExchange(ctx, teamroster.Exchange{
		From: from.ID, To: to.ID, RequestID: req.RequestID, ReplyTo: req.ReplyTo,
		Parent: parent, Group: c.Group, Ceiling: c.Ceiling, OpenedAt: s.now(),
	}, req.Message)
	if err != nil {
		return MessageReceipt{}, err
	}
	receipt := MessageReceipt{Exchange: x.ID, To: to.ID, ToName: to.Name, State: SendDelivered}
	if existed && x.State != teamroster.ExchangeQueued {
		receipt.State = SendDuplicate
		return receipt, nil
	}
	return receipt, s.deliverMessage(ctx, *from, x, req.Message, req.Steer)
}

func resolvePair(r teamroster.Roster, c Caller, to string) (from, target *teamroster.Agent, err error) {
	from, ok := r.Agent(c.Agent)
	if !ok {
		return nil, nil, teamroster.ErrUnknownAgent
	}
	target, ok = r.Agent(to)
	if !ok {
		target, ok = r.AgentNamed(to)
	}
	if !ok {
		return nil, nil, teamroster.ErrUnknownAgent
	}
	if target.ID == from.ID {
		return nil, nil, ErrSelf
	}
	if c.Group != "" {
		g, ok := r.Group(c.Group)
		if !ok {
			return nil, nil, teamroster.ErrUnknownGroup
		}
		if !g.IsMember(target.ID) {
			return nil, nil, teamroster.ErrNotMember
		}
	}
	return from, target, nil
}

func retried(book []teamroster.Exchange, from, requestID string) bool {
	if requestID == "" {
		return false
	}
	for _, x := range book {
		if x.From == from && x.RequestID == requestID {
			return true
		}
	}
	return false
}

// admit applies the tree rules and the send bounds to a new message.
func (s *Service) admit(book []teamroster.Exchange, c Caller, from, to string) error {
	byID := make(map[string]teamroster.Exchange, len(book))
	open := 0
	for _, x := range book {
		byID[x.ID] = x
		if x.From == from && !x.State.Settled() {
			open++
		}
	}
	depth := 0
	for _, id := range c.Serving {
		d := 0
		for x, ok := byID[id]; ok && d <= len(byID); x, ok = byID[x.Parent] {
			if x.From == to {
				return ErrUpstream
			}
			d++
		}
		depth = max(depth, d)
	}
	switch {
	case depth >= MaxDepth:
		return ErrDepth
	case open >= MaxOpenPerSender:
		return ErrOpenLimit
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.sends.admit(from, c.Turn, s.now()) {
		return ErrRateLimited
	}
	return nil
}

func (s *Service) deliverMessage(ctx context.Context, from teamroster.Agent, x teamroster.Exchange, body string, steer bool) error {
	pane, err := s.panes.Ensure(ctx, SeatRef{Agent: x.To})
	if err == nil {
		display, text := renderMessage(from, x, body)
		err = pane.Deliver(Delivery{
			Key: "team-" + x.ID, Kind: DeliverMessage, From: from.ID, Exchange: x.ID,
			Display: display, Text: text, Serving: []string{x.ID}, Steer: steer, Ceiling: x.Ceiling,
		})
	}
	if err != nil {
		_, stateErr := s.store.SetExchangeState(ctx, x.ID, teamroster.ExchangeFailed, s.now())
		return errors.Join(fmt.Errorf("%w: %w", ErrUnavailable, err), stateErr)
	}
	_, err = s.store.SetExchangeState(ctx, x.ID, teamroster.ExchangeDelivered, s.now())
	return err
}

// Settled applies the host's report of a turn in seat. Replies the turn read
// are marked returned first, so an exchange whose children have all come back
// and been read answers in this same call; then the exchanges it served, then
// the group turn it was.
func (s *Service) Settled(ctx context.Context, seat SeatRef, st Settlement) error {
	defer s.changed()
	var errs []error
	for _, id := range st.Consumed {
		if err := s.store.MarkReturned(ctx, id); err != nil && !errors.Is(err, teamroster.ErrUnknownExchange) {
			errs = append(errs, err)
		}
	}
	if len(st.Serving) > 0 {
		errs = append(errs, s.settleServing(ctx, seat, st))
	}
	if len(st.Steps) > 0 {
		errs = append(errs, s.settleSteps(ctx, seat, st))
	}
	return errors.Join(errs...)
}

func (s *Service) settleServing(ctx context.Context, seat SeatRef, st Settlement) error {
	r, err := s.store.Load()
	if err != nil {
		return err
	}
	book, err := s.store.Exchanges()
	if err != nil {
		return err
	}
	var errs []error
	for _, id := range st.Serving {
		x, ok := find(book, id)
		if !ok || x.To != seat.Agent || x.State.Settled() {
			continue
		}
		if err := s.settle(ctx, r, book, x, st); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func (s *Service) settle(ctx context.Context, r teamroster.Roster, book []teamroster.Exchange, x teamroster.Exchange, st Settlement) error {
	if st.Outcome == OutcomeCompleted && outstanding(book, x.ID) {
		_, err := s.store.SetExchangeState(ctx, x.ID, teamroster.ExchangeWaiting, s.now())
		return err
	}
	settled, err := s.store.SetExchangeState(ctx, x.ID, replyStatus(st.Outcome), s.now())
	if err != nil {
		return err
	}
	return s.deliverReply(ctx, r, book, settled, st)
}

// outstanding reports whether any work exchange id handed on has not yet come
// back and been read.
func outstanding(book []teamroster.Exchange, id string) bool {
	for _, x := range book {
		if x.Parent == id && (!x.State.Settled() || !x.Returned) {
			return true
		}
	}
	return false
}

func (s *Service) deliverReply(ctx context.Context, r teamroster.Roster, book []teamroster.Exchange, x teamroster.Exchange, st Settlement) error {
	if x.ReplyTo == teamroster.ReplyToUser || x.From == teamroster.ByUser {
		return s.store.MarkReturned(ctx, x.ID)
	}
	replier := teamroster.Agent{ID: x.To, Name: x.To}
	if a, ok := r.Agent(x.To); ok {
		replier = *a
	}
	var serving []string
	var ceiling string
	if x.Parent != "" {
		serving = []string{x.Parent}
		if p, ok := find(book, x.Parent); ok {
			ceiling = p.Ceiling
		}
	}
	pane, err := s.panes.Ensure(ctx, SeatRef{Agent: x.From, Group: x.Group})
	if err == nil {
		display, text := renderReply(replier, x, st)
		err = pane.Deliver(Delivery{
			Key: "team-reply-" + x.ID, Kind: DeliverReply, From: x.To, Exchange: x.ID,
			Display: display, Text: text, Serving: serving, Ceiling: ceiling,
		})
	}
	if err != nil {
		return errors.Join(fmt.Errorf("%w: %w", ErrUnavailable, err), s.store.MarkReturned(ctx, x.ID))
	}
	return nil
}

func find(book []teamroster.Exchange, id string) (teamroster.Exchange, bool) {
	for _, x := range book {
		if x.ID == id {
			return x, true
		}
	}
	return teamroster.Exchange{}, false
}

// Stop cancels the turn the agent is running in its own chat, if its session
// is open. The exchanges that turn served settle as stopped through the host's
// settlement, so their senders are told.
func (s *Service) Stop(agent string) bool {
	pane, ok := s.panes.Live(SeatRef{Agent: agent})
	if ok {
		pane.Cancel()
	}
	return ok
}

// Recover interrupts unfinished exchanges and rounds before the host resumes
// any seat. Durable inbox consumers must discard work for settled exchanges.
func (s *Service) Recover(ctx context.Context) error {
	defer s.changed()
	r, err := s.store.Load()
	if err != nil {
		return err
	}
	book, err := s.store.Exchanges()
	if err != nil {
		return err
	}
	errs := []error{s.recoverRounds(ctx, r)}
	var interrupted []teamroster.Exchange
	for _, x := range book {
		if x.State.Settled() {
			continue
		}
		settled, err := s.store.SetExchangeState(ctx, x.ID, teamroster.ExchangeInterrupted, s.now())
		if err != nil {
			errs = append(errs, err)
		} else {
			interrupted = append(interrupted, settled)
		}
	}
	for _, x := range interrupted {
		errs = append(errs, s.deliverReply(ctx, r, book, x, Settlement{Outcome: OutcomeInterrupted, Code: "team.interrupted"}))
	}
	return errors.Join(errs...)
}
