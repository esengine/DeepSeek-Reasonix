package teamroster

import (
	"context"
	"encoding/json"
	"path/filepath"
	"regexp"
	"time"
	"unicode/utf8"
)

// A group's log and round state live in groups/<id>/ under the store.
const (
	groupsDir    = "groups"
	GroupLogFile = "log.jsonl"
	RoundFile    = "round.json"
)

var lineCode = regexp.MustCompile(`^[a-z0-9_.]{0,64}$`)

// MaxLineRunes bounds the text one group line keeps; the speaker's own session
// holds the whole reply.
const MaxLineRunes = 20000

// LineKind tells a message from something the host observed.
type LineKind string

const (
	LinePost   LineKind = "post"
	LineEvent  LineKind = "event"
	BySchedule          = "scheduler"
)

// EventKind is what the host observed about a member or the round.
type EventKind string

const (
	EventPassed   EventKind = "passed"
	EventMissed   EventKind = "missed"
	EventWorking  EventKind = "working"
	EventFinished EventKind = "finished"
	EventFailed   EventKind = "failed"
	EventPaused   EventKind = "paused"
	EventResumed  EventKind = "resumed"
	EventStopped  EventKind = "stopped"
	EventCut      EventKind = "cut"
	EventMemory   EventKind = "memory"
)

func (k EventKind) valid() bool {
	switch k {
	case EventPassed, EventMissed, EventWorking, EventFinished, EventFailed,
		EventPaused, EventResumed, EventStopped, EventCut, EventMemory:
		return true
	}
	return false
}

// GroupLine is one entry of a group's log. A post is a member's or the user's
// message; an event names the member it is about in From, or the user for the
// round itself, and Code says why when the host knows. Length is the rune count
// of a post's text before it was cut to MaxLineRunes, 0 when whole.
type GroupLine struct {
	Seq      int64     `json:"seq"`
	At       time.Time `json:"at"`
	From     string    `json:"from"`
	Kind     LineKind  `json:"kind"`
	Text     string    `json:"text,omitempty"`
	Length   int       `json:"length,omitempty"`
	Mentions []string  `json:"mentions,omitempty"`
	Event    EventKind `json:"event,omitempty"`
	Count    int       `json:"count,omitempty"`
	Code     string    `json:"code,omitempty"`
	Late     bool      `json:"late,omitempty"`
	Schedule string    `json:"schedule,omitempty"`
}

func (l *GroupLine) check() error {
	if l.From != ByUser && l.From != BySchedule && !agentID.MatchString(l.From) {
		return fieldErr("line.from", ErrInvalid)
	}
	if l.Schedule != "" && (l.From != BySchedule || !scheduleID.MatchString(l.Schedule)) {
		return fieldErr("line.schedule", ErrInvalid)
	}
	for _, m := range l.Mentions {
		if !agentID.MatchString(m) {
			return fieldErr("mentions", ErrInvalid)
		}
	}
	if !lineCode.MatchString(l.Code) {
		return fieldErr("line.code", ErrInvalid)
	}
	switch l.Kind {
	case LinePost:
		if l.Event != "" || l.Code != "" {
			return fieldErr("line.event", ErrInvalid)
		}
	case LineEvent:
		if !l.Event.valid() {
			return fieldErr("line.event", ErrInvalid)
		}
	default:
		return fieldErr("line.kind", ErrInvalid)
	}
	return nil
}

func (s Store) groupDir(group string) (string, error) {
	if s.Dir == "" {
		return "", errNoDir
	}
	if !groupID.MatchString(group) {
		return "", fieldErr("group.id", ErrInvalid)
	}
	return filepath.Join(s.Dir, groupsDir, group), nil
}

// AppendLine adds l to the group's log under the next sequence number and
// returns it as stored, stamped now when At is zero.
func (s Store) AppendLine(ctx context.Context, group string, l GroupLine) (GroupLine, error) {
	if err := l.check(); err != nil {
		return GroupLine{}, err
	}
	dir, err := s.groupDir(group)
	if err != nil {
		return GroupLine{}, err
	}
	if n := utf8.RuneCountInString(l.Text); n > MaxLineRunes {
		l.Text, l.Length = string([]rune(l.Text)[:MaxLineRunes]), n
	}
	err = s.withLock(ctx, dir, GroupLogFile, func() error {
		path := filepath.Join(dir, GroupLogFile)
		var last int64
		if err := readLines(path, func(old GroupLine) { last = old.Seq }); err != nil {
			return err
		}
		l.Seq = last + 1
		if l.At.IsZero() {
			l.At = time.Now().UTC()
		}
		return appendJSONL(path, l)
	})
	if err != nil {
		return GroupLine{}, err
	}
	return l, nil
}

// Lines returns up to limit lines after sequence number after, oldest first;
// limit 0 or less is every one of them.
func (s Store) Lines(group string, after int64, limit int) ([]GroupLine, error) {
	dir, err := s.groupDir(group)
	if err != nil {
		return nil, err
	}
	out := []GroupLine{}
	err = readLines(filepath.Join(dir, GroupLogFile), func(l GroupLine) {
		if l.Seq > after && (limit <= 0 || len(out) < limit) {
			out = append(out, l)
		}
	})
	return out, err
}

// LinesBefore returns the last limit lines before sequence number before (0 is
// the end of the log), oldest first, for paging back through it.
func (s Store) LinesBefore(group string, before int64, limit int) ([]GroupLine, error) {
	dir, err := s.groupDir(group)
	if err != nil {
		return nil, err
	}
	out := []GroupLine{}
	err = readLines(filepath.Join(dir, GroupLogFile), func(l GroupLine) {
		if before <= 0 || l.Seq < before {
			out = append(out, l)
		}
	})
	if limit > 0 && len(out) > limit {
		out = out[len(out)-limit:]
	}
	return out, err
}

func readLines(path string, fn func(GroupLine)) error {
	return readJSONL(path, func(line []byte) error {
		var l GroupLine
		if err := json.Unmarshal(line, &l); err != nil {
			return err
		}
		if l.Schedule != "" {
			l.From = BySchedule
		}
		fn(l)
		return nil
	})
}
