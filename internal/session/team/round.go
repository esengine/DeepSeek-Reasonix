package team

import (
	"context"
	"errors"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"reasonix/internal/state/teamroster"
)

// Round timers and bounds.
const (
	SpeakerTimeout = 240 * time.Second
	WorkGrace      = 15 * time.Second
	MaxHand        = 4
	maxReadLines   = 50
)

// roundBook is the in-memory side of group rounds: one lock per group, the
// pass or hand a speaking member asked for, and what each seat was last seen
// doing. None of it is durable; a restart cuts the round in flight.
type roundBook struct {
	mu       sync.Mutex
	locks    map[string]*sync.Mutex
	intents  map[string]*stepIntent
	activity map[SeatRef]seatActivity
}

type stepIntent struct {
	pass    bool
	hand    []string
	ceiling string
}

type seatActivity struct {
	lastEvent time.Time
	toolSince time.Time
}

func (b *roundBook) lock(group string) (unlock func()) {
	b.mu.Lock()
	if b.locks == nil {
		b.locks = map[string]*sync.Mutex{}
	}
	l := b.locks[group]
	if l == nil {
		l = &sync.Mutex{}
		b.locks[group] = l
	}
	b.mu.Unlock()
	l.Lock()
	return l.Unlock
}

func (b *roundBook) setIntent(step string, fn func(*stepIntent)) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.intents == nil {
		b.intents = map[string]*stepIntent{}
	}
	if b.intents[step] == nil {
		b.intents[step] = &stepIntent{}
	}
	fn(b.intents[step])
}

func (b *roundBook) takeIntent(step string) stepIntent {
	b.mu.Lock()
	defer b.mu.Unlock()
	in := b.intents[step]
	delete(b.intents, step)
	if in == nil {
		return stepIntent{}
	}
	return *in
}

func (b *roundBook) observe(seat SeatRef, o Observation) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.activity == nil {
		b.activity = map[SeatRef]seatActivity{}
	}
	a := b.activity[seat]
	a.lastEvent = o.At
	switch o.Kind {
	case ObserveToolStarted:
		if a.toolSince.IsZero() {
			a.toolSince = o.At
		}
	case ObserveToolFinished:
		a.toolSince = time.Time{}
	}
	b.activity[seat] = a
}

func (b *roundBook) seen(seat SeatRef) seatActivity {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.activity[seat]
}

func (b *roundBook) reset(seat SeatRef) {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.activity, seat)
}

func (s *Service) group(id string) (teamroster.Roster, *teamroster.Group, error) {
	r, err := s.store.Load()
	if err != nil {
		return r, nil, err
	}
	g, ok := r.Group(id)
	if !ok {
		return r, nil, teamroster.ErrUnknownGroup
	}
	return r, g, nil
}

// resolveMembers turns names or ids into the ids of g's members, leaving out
// exclude; a name that is not a member is refused.
func resolveMembers(r teamroster.Roster, g teamroster.Group, names []string, exclude string) ([]string, error) {
	var out []string
	for _, n := range names {
		a, ok := r.Agent(n)
		if !ok {
			a, ok = r.AgentNamed(n)
		}
		if !ok || !g.IsMember(a.ID) {
			return nil, teamroster.ErrNotMember
		}
		if a.ID != exclude && !slices.Contains(out, a.ID) {
			out = append(out, a.ID)
		}
	}
	return out, nil
}

func checkPost(text string) error {
	if strings.TrimSpace(text) == "" {
		return ErrEmptyMessage
	}
	if utf8.RuneCountInString(text) > MaxMessageRunes {
		return ErrMessageTooLong
	}
	return nil
}

// UserPost records the user's message in group and calls the members who
// answer it. A round in flight takes them next, and agent calls parked at the
// relay limit resume after them.
func (s *Service) UserPost(ctx context.Context, group, text string, mentions []string, everyone bool) (teamroster.GroupLine, error) {
	return s.userPost(ctx, group, teamroster.GroupLine{Text: text}, mentions, everyone)
}

// userPost records the user's post and opens a round on it; post carries the
// text and, for a scheduled task's post, the task it came from.
func (s *Service) userPost(ctx context.Context, group string, post teamroster.GroupLine, mentions []string, everyone bool) (teamroster.GroupLine, error) {
	defer s.changed()
	text := post.Text
	if err := checkPost(text); err != nil {
		return teamroster.GroupLine{}, err
	}
	defer s.rounds.lock(group)()
	r, g, err := s.group(group)
	if err != nil {
		return teamroster.GroupLine{}, err
	}
	if g.Archived {
		return teamroster.GroupLine{}, ErrArchived
	}
	ids, err := resolveMembers(r, *g, mentions, "")
	if err != nil {
		return teamroster.GroupLine{}, err
	}
	from, cause, ceiling := teamroster.ByUser, teamroster.CauseUser, ""
	if post.Schedule != "" {
		from, cause, ceiling = teamroster.BySchedule, teamroster.CauseSchedule, "ask"
	}
	line, err := s.store.AppendLine(ctx, group, teamroster.GroupLine{From: from, Kind: teamroster.LinePost, Text: text, Mentions: ids, At: s.now(), Schedule: post.Schedule})
	if err != nil {
		return line, err
	}
	rs, err := s.store.Round(group)
	if err != nil {
		return line, err
	}
	rs.Round++
	rs.Relays = 0
	rs.Queue = callNext(rs.Queue, openingSpeakers(*g, ids, everyone, from, cause, ceiling))
	for _, p := range rs.Parked {
		if !slices.ContainsFunc(rs.Queue, func(q teamroster.Slot) bool { return q.Member == p.Member }) {
			rs.Queue = append(rs.Queue, p)
		}
	}
	rs.Parked = nil
	return line, s.pump(ctx, r, *g, &rs)
}

// AgentPost records an agent's post in group from outside a turn there. A
// main agent or the group's admin opens a round under the group's mode; a
// plain member calls only the members it names.
func (s *Service) AgentPost(ctx context.Context, c Caller, group, text string, mentions []string) (teamroster.GroupLine, error) {
	defer s.changed()
	if err := checkPost(text); err != nil {
		return teamroster.GroupLine{}, err
	}
	defer s.rounds.lock(group)()
	r, g, err := s.group(group)
	if err != nil {
		return teamroster.GroupLine{}, err
	}
	if g.Archived {
		return teamroster.GroupLine{}, ErrArchived
	}
	poster, ok := r.Agent(c.Agent)
	if !ok {
		return teamroster.GroupLine{}, teamroster.ErrUnknownAgent
	}
	if !poster.Main && !g.IsMember(poster.ID) {
		return teamroster.GroupLine{}, teamroster.ErrNotMember
	}
	ids, err := resolveMembers(r, *g, mentions, poster.ID)
	if err != nil {
		return teamroster.GroupLine{}, err
	}
	line, err := s.store.AppendLine(ctx, group, teamroster.GroupLine{From: poster.ID, Kind: teamroster.LinePost, Text: text, Mentions: ids, At: s.now()})
	if err != nil {
		return line, err
	}
	rs, err := s.store.Round(group)
	if err != nil {
		return line, err
	}
	if len(ids) > 0 || poster.Main || g.Admin == poster.ID {
		rs.Queue = callNext(rs.Queue, openingSpeakers(*g, ids, false, poster.ID, teamroster.CausePost, c.Ceiling))
	}
	return line, s.pump(ctx, r, *g, &rs)
}

// pump starts the next turn while nobody is speaking, parking agent calls
// once the relay limit is reached, and saves the round.
func (s *Service) pump(ctx context.Context, r teamroster.Roster, g teamroster.Group, rs *teamroster.RoundState) error {
	var errs []error
	for rs.Speaking == nil && len(rs.Queue) > 0 {
		slot := rs.Queue[0]
		if !g.IsMember(slot.Member) {
			rs.Queue = rs.Queue[1:]
			continue
		}
		if isRelay(slot) && rs.Relays >= RelayLimit(g) {
			if parkRelays(rs) > 0 {
				errs = append(errs, s.event(ctx, g.ID, teamroster.ByUser, teamroster.EventPaused, "", rs.Relays))
			}
			continue
		}
		rs.Queue = rs.Queue[1:]
		if isRelay(slot) {
			rs.Relays++
		}
		rs.Step++
		if err := s.startTurn(ctx, r, g, rs, slot); err != nil {
			errs = append(errs, s.event(ctx, g.ID, slot.Member, teamroster.EventFailed, "team.unavailable", 0))
		}
	}
	return errors.Join(append(errs, s.store.SaveRound(ctx, g.ID, *rs))...)
}

func (s *Service) event(ctx context.Context, group, from string, kind teamroster.EventKind, code string, count int) error {
	_, err := s.store.AppendLine(ctx, group, teamroster.GroupLine{From: from, Kind: teamroster.LineEvent, Event: kind, Code: code, Count: count, At: s.now()})
	return err
}

func (s *Service) startTurn(ctx context.Context, r teamroster.Roster, g teamroster.Group, rs *teamroster.RoundState, slot teamroster.Slot) error {
	unseen, err := s.store.Lines(g.ID, rs.Seen[slot.Member], 0)
	if err != nil {
		return err
	}
	key := stepKey(g.ID, rs.Step)
	seat := SeatRef{Agent: slot.Member, Group: g.ID}
	pane, err := s.panes.Ensure(ctx, seat)
	if err != nil {
		return err
	}
	err = pane.Deliver(Delivery{
		Key: key, Kind: DeliverGroupTurn, From: slot.By, Group: g.ID, Step: key,
		Display: "turn in " + groupName(g), Text: renderGroupTurn(r, g, *rs, slot.Member, unseen), Ceiling: slot.Ceiling,
	})
	if err != nil {
		return err
	}
	now := s.now()
	rs.Speaking = &teamroster.Speaking{Slot: slot, Key: key, StartedAt: now, LastEventAt: now}
	if rs.Seen == nil {
		rs.Seen = map[string]int64{}
	}
	if n := len(unseen); n > 0 {
		rs.Seen[slot.Member] = unseen[n-1].Seq
	}
	s.rounds.reset(seat)
	return nil
}

// settleSteps applies the end of a member's group turn: its answer, pass or
// failure goes into the log, the members it handed to speak next, and the
// round moves on. A turn the round had already moved past posts as late.
func (s *Service) settleSteps(ctx context.Context, seat SeatRef, st Settlement) error {
	if seat.Group == "" {
		return nil
	}
	defer s.rounds.lock(seat.Group)()
	r, g, err := s.group(seat.Group)
	if err != nil {
		return err
	}
	rs, err := s.store.Round(g.ID)
	if err != nil {
		return err
	}
	var errs []error
	for _, key := range st.Steps {
		in := s.rounds.takeIntent(key)
		late := false
		switch {
		case rs.Speaking != nil && rs.Speaking.Key == key && rs.Speaking.Member == seat.Agent:
			rs.Speaking = nil
		case slices.Contains(rs.Working, seat.Agent):
			rs.Working = slices.DeleteFunc(rs.Working, func(m string) bool { return m == seat.Agent })
			late = true
		default:
			continue
		}
		errs = append(errs, s.recordOutcome(ctx, g.ID, seat.Agent, in, st, late))
		if len(in.hand) > 0 {
			slots := make([]teamroster.Slot, 0, len(in.hand))
			for _, m := range in.hand {
				slots = append(slots, teamroster.Slot{Member: m, Cause: teamroster.CauseHand, By: seat.Agent, Ceiling: in.ceiling})
			}
			rs.Queue = callNext(rs.Queue, slots)
		}
	}
	return errors.Join(append(errs, s.pump(ctx, r, *g, &rs))...)
}

func (s *Service) recordOutcome(ctx context.Context, group, member string, in stepIntent, st Settlement, late bool) error {
	line := teamroster.GroupLine{From: member, Kind: teamroster.LineEvent, At: s.now(), Late: late}
	switch {
	case st.Outcome != OutcomeCompleted:
		line.Event, line.Code = outcomeEvent(st.Outcome), st.Code
	case in.pass || strings.TrimSpace(st.FinalText) == "":
		line.Event = teamroster.EventPassed
		if late {
			line.Event = teamroster.EventFinished
		}
	default:
		line.Kind, line.Text, line.Mentions = teamroster.LinePost, st.FinalText, in.hand
	}
	_, err := s.store.AppendLine(ctx, group, line)
	return err
}

func outcomeEvent(o Outcome) teamroster.EventKind {
	switch o {
	case OutcomeCancelled:
		return teamroster.EventStopped
	case OutcomeInterrupted:
		return teamroster.EventCut
	}
	return teamroster.EventFailed
}

// PassTurn records that the calling member has nothing to add this turn.
func (s *Service) PassTurn(c Caller) error {
	return s.markIntent(c, func(in *stepIntent) { in.pass = true })
}

// HandTurn records the members the calling member gives the next turn to.
func (s *Service) HandTurn(c Caller, to []string) error {
	if len(to) == 0 || len(to) > MaxHand {
		return ErrHandSize
	}
	r, g, err := s.group(c.Group)
	if err != nil {
		if c.Group == "" {
			return ErrNotInGroup
		}
		return err
	}
	ids, err := resolveMembers(r, *g, to, c.Agent)
	if err != nil {
		return err
	}
	if len(ids) == 0 {
		return ErrHandSize
	}
	return s.markIntent(c, func(in *stepIntent) { in.hand, in.ceiling = ids, c.Ceiling })
}

func (s *Service) markIntent(c Caller, fn func(*stepIntent)) error {
	if c.Group == "" || c.Step == "" {
		return ErrNotInGroup
	}
	rs, err := s.store.Round(c.Group)
	if err != nil {
		return err
	}
	speaking := rs.Speaking != nil && rs.Speaking.Key == c.Step && rs.Speaking.Member == c.Agent
	if !speaking && !slices.Contains(rs.Working, c.Agent) {
		return ErrNotYourTurn
	}
	s.rounds.setIntent(c.Step, fn)
	return nil
}

// Observed records activity the host saw in a group seat; the round's timers
// are judged on it.
func (s *Service) Observed(seat SeatRef, o Observation) {
	if seat.Group != "" {
		s.rounds.observe(seat, o)
	}
}

// Tick applies the round timers as of now. A speaker whose tool has run past
// WorkGrace keeps working off the speaking order, and one silent past
// SpeakerTimeout is missed; either way its answer still posts, late.
func (s *Service) Tick(ctx context.Context) error {
	r, err := s.store.Load()
	if err != nil {
		return err
	}
	errs := []error{s.fireDue(ctx, r)}
	for _, g := range r.Groups {
		errs = append(errs, s.tickGroup(ctx, g.ID))
	}
	return errors.Join(errs...)
}

func (s *Service) tickGroup(ctx context.Context, group string) error {
	defer s.changed()
	defer s.rounds.lock(group)()
	r, g, err := s.group(group)
	if err != nil {
		return err
	}
	rs, err := s.store.Round(group)
	if err != nil || rs.Speaking == nil {
		return err
	}
	sp := *rs.Speaking
	act := s.rounds.seen(SeatRef{Agent: sp.Member, Group: group})
	now, last := s.now(), sp.StartedAt
	if act.lastEvent.After(last) {
		last = act.lastEvent
	}
	var kind teamroster.EventKind
	switch {
	case !act.toolSince.IsZero() && now.Sub(act.toolSince) >= WorkGrace:
		kind = teamroster.EventWorking
	case now.Sub(last) >= SpeakerTimeout:
		kind = teamroster.EventMissed
	default:
		return nil
	}
	rs.Speaking = nil
	if !slices.Contains(rs.Working, sp.Member) {
		rs.Working = append(rs.Working, sp.Member)
	}
	return errors.Join(s.event(ctx, group, sp.Member, kind, "", 0), s.pump(ctx, r, *g, &rs))
}

// Continue resumes the agent calls parked at the relay limit, with the count
// started over.
func (s *Service) Continue(ctx context.Context, group string) error {
	defer s.changed()
	defer s.rounds.lock(group)()
	r, g, err := s.group(group)
	if err != nil {
		return err
	}
	rs, err := s.store.Round(group)
	if err != nil || len(rs.Parked) == 0 {
		return err
	}
	rs.Relays = 0
	rs.Queue = append(rs.Queue, rs.Parked...)
	rs.Parked = nil
	return errors.Join(s.event(ctx, group, teamroster.ByUser, teamroster.EventResumed, "", 0), s.pump(ctx, r, *g, &rs))
}

// StopMember takes agent out of the group's round: its queued and parked calls
// are dropped and a turn it is running there is cancelled, which then settles
// as stopped.
func (s *Service) StopMember(ctx context.Context, group, agent string) error {
	defer s.changed()
	defer s.rounds.lock(group)()
	if _, _, err := s.group(group); err != nil {
		return err
	}
	rs, err := s.store.Round(group)
	if err != nil {
		return err
	}
	drop := func(sl teamroster.Slot) bool { return sl.Member == agent }
	queued := slices.ContainsFunc(rs.Queue, drop) || slices.ContainsFunc(rs.Parked, drop)
	rs.Queue, rs.Parked = slices.DeleteFunc(rs.Queue, drop), slices.DeleteFunc(rs.Parked, drop)
	running := (rs.Speaking != nil && rs.Speaking.Member == agent) || slices.Contains(rs.Working, agent)
	if pane, ok := s.panes.Live(SeatRef{Agent: agent, Group: group}); ok && running {
		pane.Cancel()
	} else if queued {
		if err := s.event(ctx, group, agent, teamroster.EventStopped, "", 0); err != nil {
			return err
		}
	}
	return s.store.SaveRound(ctx, group, rs)
}

// ReadGroup renders up to limit lines of group before sequence number before
// (0 is the end), for a member or a main agent.
func (s *Service) ReadGroup(c Caller, group string, before int64, limit int) (string, error) {
	r, g, err := s.group(group)
	if err != nil {
		return "", err
	}
	if a, ok := r.Agent(c.Agent); !ok || (!a.Main && !g.IsMember(a.ID)) {
		return "", teamroster.ErrNotMember
	}
	if limit <= 0 || limit > maxReadLines {
		limit = maxReadLines
	}
	lines, err := s.store.LinesBefore(group, before, limit)
	if err != nil || len(lines) == 0 {
		return "The group has no lines before that point.", err
	}
	var b strings.Builder
	for _, l := range lines {
		b.WriteString(renderLine(r, l))
	}
	return b.String(), nil
}

// recoverRounds cuts the turns a restart killed: the speaker and every member
// still working get a cut line, and whoever was waiting is parked behind a
// Continue the user gives.
func (s *Service) recoverRounds(ctx context.Context, r teamroster.Roster) error {
	var errs []error
	for _, g := range r.Groups {
		rs, err := s.store.Round(g.ID)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		cut := slices.Clone(rs.Working)
		if rs.Speaking != nil {
			cut = append([]string{rs.Speaking.Member}, cut...)
		}
		if len(cut) == 0 {
			continue
		}
		for _, m := range cut {
			errs = append(errs, s.event(ctx, g.ID, m, teamroster.EventCut, "team.interrupted", 0))
		}
		rs.Speaking, rs.Working = nil, nil
		rs.Parked, rs.Queue = append(rs.Queue, rs.Parked...), nil
		errs = append(errs, s.store.SaveRound(ctx, g.ID, rs))
	}
	return errors.Join(errs...)
}
