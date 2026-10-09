package feedback

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"unicode"

	"github.com/charmbracelet/x/ansi"

	"reasonix/internal/platform/crashreport"
)

const maxReplies = 20

var receiptPattern = regexp.MustCompile(`^[A-Za-z0-9-]{1,40}$`)

// Reply sends the reporter's answer on one of their reports. The service alone
// decides whether the report takes one; a refusal comes back as a sentinel, and
// a second reply on a report whose first is still in flight is ErrDuplicate.
// What this machine remembers is updated so the thread reads right before the
// next list; what was shown is only ever recorded by MarkSeen.
func (s *Service) Reply(ctx context.Context, receipt, body string) (ReplyReceipt, error) {
	receipt = strings.TrimSpace(receipt)
	if !receiptPattern.MatchString(receipt) {
		return ReplyReceipt{}, invalid(FieldReceipt, ReasonBadValue)
	}
	body = strings.TrimSpace(body)
	if body == "" {
		return ReplyReceipt{}, invalid(FieldBody, ReasonEmpty)
	}
	redacted := crashreport.Redact(body, 4*s.limits.ReplyBytes)
	if len(redacted) > s.limits.ReplyBytes {
		return ReplyReceipt{}, invalid(FieldBody, ReasonTooLong)
	}
	st, err := s.store.load()
	if err != nil {
		return ReplyReceipt{}, err
	}
	it, ok := st.find(receipt)
	switch {
	case !ok:
		return ReplyReceipt{}, invalid(FieldReceipt, ReasonBadValue)
	case it.StatusUnavailable || st.InstallID == "" || st.InstallToken == "":
		return ReplyReceipt{}, ErrNotReplyable
	}
	if _, busy := s.replying.LoadOrStore(it.Receipt, true); busy {
		return ReplyReceipt{}, ErrDuplicate
	}
	defer s.replying.Delete(it.Receipt)
	got, err := s.postReply(ctx, st.InstallID, st.InstallToken, it.Receipt, redacted)
	if errors.Is(err, ErrBadToken) {
		if rerr := s.store.update(func(st *state) error { st.retire(); return nil }); rerr != nil {
			return ReplyReceipt{}, rerr
		}
	}
	if err != nil {
		return ReplyReceipt{}, err
	}
	at := got.CreatedAt
	if at.IsZero() {
		at = now().UTC()
		got.CreatedAt = at
	}
	mine := it.Receipt
	err = s.store.update(func(st *state) error {
		for i := range st.Items {
			it := &st.Items[i]
			if it.Receipt != mine {
				continue
			}
			it.Replies = append(it.Replies, Reply{ID: got.ReplyID, Author: AuthorUser, Body: redacted, CreatedAt: at})
			if len(it.Replies) > maxReplies {
				it.Replies = it.Replies[len(it.Replies)-maxReplies:]
			}
			it.UpdatedAt = at
			if it.Status == StatusNeedsInfo {
				it.Status = StatusReceived
			}
			it.NeedsInput = false
		}
		return nil
	})
	return got, err
}

// MarkSeen records that the person was shown the thread of a report up to and
// including reply upTo. It never moves backwards and never claims a reply newer
// than the newest one held here.
func (s *Service) MarkSeen(receipt string, upTo ReplyID) error {
	if upTo <= 0 {
		return invalid(FieldReplyID, ReasonBadValue)
	}
	return s.store.update(func(st *state) error {
		if it, ok := st.find(strings.TrimSpace(receipt)); ok {
			var newest ReplyID
			for _, r := range it.Replies {
				newest = max(newest, r.ID)
			}
			st.markSeen(it.Receipt, min(upTo, newest))
		}
		return nil
	})
}

// Item is one remembered report with its read state, or false.
func (s *Service) Item(receipt string) (Item, bool) {
	st, err := s.store.load()
	if err != nil {
		return Item{}, false
	}
	it, ok := st.find(strings.TrimSpace(receipt))
	if !ok {
		return Item{}, false
	}
	return st.view(it), true
}

func (st *state) find(receipt string) (Item, bool) {
	for _, it := range st.Items {
		if strings.EqualFold(it.Receipt, receipt) {
			return it, true
		}
	}
	return Item{}, false
}

func (st *state) markSeen(receipt string, upTo ReplyID) {
	if st.Seen == nil {
		st.Seen = map[string]ReplyID{}
	}
	st.Seen[receipt] = max(st.Seen[receipt], upTo)
}

// view fills what is computed on read: the unread count, and a thread that is
// never nil.
func (st *state) view(it Item) Item {
	it.Replies = cleanReplies(it.Replies)
	it.UnreadReplies = 0
	for _, r := range it.Replies {
		if r.Author == AuthorMaintainer && r.ID > st.Seen[it.Receipt] {
			it.UnreadReplies++
		}
	}
	if it.StatusUnavailable {
		it.NeedsInput = false
	}
	return it
}

func (st *state) mine(offline bool) Mine {
	out := Mine{Items: make([]Item, 0, len(st.Items)), Offline: offline}
	for _, it := range st.Items {
		v := st.view(it)
		if !v.StatusUnavailable && (v.UnreadReplies > 0 || v.NeedsInput) {
			out.Unread++
		}
		out.Items = append(out.Items, v)
	}
	out.HasNew = out.Unread > 0
	if st.Profile != nil {
		p := *st.Profile
		out.Profile = &p
	}
	return out
}

// cleanReplies makes a thread from the service safe to show anywhere: escape
// sequences and control characters other than line breaks are removed, because
// the text reaches terminals as well as web pages.
func cleanReplies(in []Reply) []Reply {
	if len(in) > maxReplies {
		in = in[len(in)-maxReplies:]
	}
	out := make([]Reply, len(in))
	for i, r := range in {
		r.Body = cleanText(r.Body)
		if r.Author != AuthorMaintainer {
			r.Author = AuthorUser
		}
		out[i] = r
	}
	return out
}

func cleanText(s string) string {
	s = strings.ReplaceAll(ansi.Strip(s), "\r\n", "\n")
	var b strings.Builder
	for _, r := range s {
		switch {
		case r == '\n':
			b.WriteByte('\n')
		case r == '\t':
			b.WriteByte(' ')
		case unicode.IsControl(r), isBidiControl(r):
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

func isBidiControl(r rune) bool {
	return r == 0x061C || r == 0x200E || r == 0x200F || r == 0x2028 || r == 0x2029 ||
		r >= 0x202A && r <= 0x202E || r >= 0x2066 && r <= 0x2069 || r >= 0xE0000 && r <= 0xE007F
}
