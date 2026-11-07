package teamroster

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func ask(from, to, requestID string) Exchange {
	return Exchange{From: from, To: to, RequestID: requestID, ReplyTo: ReplyToSender}
}

func TestOpenExchangeQueuesUnderAFreshID(t *testing.T) {
	s := Store{Dir: t.TempDir()}
	from, to := newAgentID(), newAgentID()
	x, existed, err := s.OpenExchange(context.Background(), ask(from, to, ""), "draft the launch post")
	if err != nil || existed {
		t.Fatalf("OpenExchange = %+v, %v, %v", x, existed, err)
	}
	if !exchangeID.MatchString(x.ID) || x.State != ExchangeQueued || x.Digest == "" || x.OpenedAt.IsZero() || !x.At.Equal(x.OpenedAt) {
		t.Fatalf("opened exchange = %+v", x)
	}
	book, err := s.Exchanges()
	if err != nil || len(book) != 1 || book[0].ID != x.ID {
		t.Fatalf("Exchanges = %+v, %v", book, err)
	}
}

func TestARequestIDIsARetryOnlyForTheSameMessageFromTheSameSender(t *testing.T) {
	s := Store{Dir: t.TempDir()}
	from, to := newAgentID(), newAgentID()
	first, _, err := s.OpenExchange(context.Background(), ask(from, to, "launch-1"), "draft it")
	if err != nil {
		t.Fatal(err)
	}
	again, existed, err := s.OpenExchange(context.Background(), ask(from, to, "launch-1"), "draft it")
	if err != nil || !existed || again.ID != first.ID {
		t.Fatalf("retry = %+v, %v, %v", again, existed, err)
	}
	if _, _, err := s.OpenExchange(context.Background(), ask(from, to, "launch-1"), "something else"); !errors.Is(err, ErrRequestReused) {
		t.Fatalf("reused id = %v, want ErrRequestReused", err)
	}
	other, existed, err := s.OpenExchange(context.Background(), ask(newAgentID(), to, "launch-1"), "draft it")
	if err != nil || existed || other.ID == first.ID {
		t.Fatalf("another sender's id = %+v, %v, %v", other, existed, err)
	}
	if book, _ := s.Exchanges(); len(book) != 2 {
		t.Fatalf("book holds %d exchanges, want 2", len(book))
	}
}

func TestOpenExchangeRefusesMalformedFields(t *testing.T) {
	s := Store{Dir: t.TempDir()}
	good := ask(ByUser, newAgentID(), "")
	cases := map[string]func(*Exchange){
		"exchange.from":   func(x *Exchange) { x.From = "someone" },
		"exchange.to":     func(x *Exchange) { x.To = "../x" },
		"request_id":      func(x *Exchange) { x.RequestID = "has space" },
		"reply_to":        func(x *Exchange) { x.ReplyTo = "everyone" },
		"exchange.parent": func(x *Exchange) { x.Parent = "x-1" },
		"group.id":        func(x *Exchange) { x.Group = "g-../" },
		"ceiling":         func(x *Exchange) { x.Ceiling = "YOLO!" },
	}
	for field, edit := range cases {
		x := good
		edit(&x)
		_, _, err := s.OpenExchange(context.Background(), x, "hi")
		var fe *FieldError
		if !errors.As(err, &fe) || fe.Field != field || !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: err = %v", field, err)
		}
	}
	if _, err := os.Stat(filepath.Join(s.Dir, ExchangeFile)); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("a refused exchange reached the disk")
	}
}

func TestASettledExchangeTakesNoFurtherChange(t *testing.T) {
	s := Store{Dir: t.TempDir()}
	x, _, err := s.OpenExchange(context.Background(), ask(ByUser, newAgentID(), ""), "hi")
	if err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 10, 26, 9, 0, 0, 0, time.UTC)
	for _, state := range []ExchangeState{ExchangeDelivered, ExchangeWaiting, ExchangeReplied} {
		got, err := s.SetExchangeState(context.Background(), x.ID, state, at)
		if err != nil || got.State != state || !got.At.Equal(at) || !got.OpenedAt.Equal(x.OpenedAt) {
			t.Fatalf("SetExchangeState(%s) = %+v, %v", state, got, err)
		}
	}
	if _, err := s.SetExchangeState(context.Background(), x.ID, ExchangeFailed, at); !errors.Is(err, ErrExchangeSettled) {
		t.Fatalf("change after settling = %v", err)
	}
	if _, err := s.SetExchangeState(context.Background(), "x-0000000000000000", ExchangeDelivered, at); !errors.Is(err, ErrUnknownExchange) {
		t.Fatalf("unknown exchange = %v", err)
	}
	if _, err := s.SetExchangeState(context.Background(), x.ID, "lost", at); !errors.Is(err, ErrInvalid) {
		t.Fatalf("unknown state = %v", err)
	}
	if book, _ := s.Exchanges(); len(book) != 1 || book[0].State != ExchangeReplied {
		t.Fatalf("folded book = %+v", book)
	}
}

func TestATornLastLineIsSkippedAndCutBeforeTheNextWrite(t *testing.T) {
	s := Store{Dir: t.TempDir()}
	first, _, err := s.OpenExchange(context.Background(), ask(ByUser, newAgentID(), ""), "one")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(s.Dir, ExchangeFile)
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(`{"id":"x-00000000`); err != nil {
		t.Fatal(err)
	}
	f.Close()
	if book, err := s.Exchanges(); err != nil || len(book) != 1 || book[0].ID != first.ID {
		t.Fatalf("Exchanges with a torn tail = %+v, %v", book, err)
	}
	second, _, err := s.OpenExchange(context.Background(), ask(ByUser, newAgentID(), ""), "two")
	if err != nil {
		t.Fatal(err)
	}
	if book, err := s.Exchanges(); err != nil || len(book) != 2 || book[1].ID != second.ID {
		t.Fatalf("Exchanges after the next write = %+v, %v", book, err)
	}
}

func TestACorruptLineInsideTheBookIsReported(t *testing.T) {
	s := Store{Dir: t.TempDir()}
	path := filepath.Join(s.Dir, ExchangeFile)
	if err := os.WriteFile(path, []byte("not json\n{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Exchanges(); !errors.Is(err, ErrCorrupt) {
		t.Fatalf("Exchanges = %v, want ErrCorrupt", err)
	}
	if _, _, err := s.OpenExchange(context.Background(), ask(ByUser, newAgentID(), ""), "x"); !errors.Is(err, ErrCorrupt) {
		t.Fatalf("OpenExchange = %v, want ErrCorrupt", err)
	}
}

func TestCompactionKeepsOpenExchangesAndTheLatestSettledOnes(t *testing.T) {
	defer func(k, c int) { keepClosed, compactAt = k, c }(keepClosed, compactAt)
	keepClosed, compactAt = 3, 12
	s := Store{Dir: t.TempDir()}
	ctx := context.Background()
	var opened []Exchange
	for range 8 {
		x, _, err := s.OpenExchange(ctx, ask(ByUser, newAgentID(), ""), "m")
		if err != nil {
			t.Fatal(err)
		}
		opened = append(opened, x)
	}
	for _, x := range opened[:6] {
		if _, err := s.SetExchangeState(ctx, x.ID, ExchangeReplied, time.Time{}); err != nil {
			t.Fatal(err)
		}
	}
	book, err := s.Exchanges()
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, x := range book {
		ids = append(ids, x.ID)
	}
	// Line 13 (the fifth settle) passes compactAt: 0 and 1 go, 2..4 stay. The
	// sixth settle lands after it, so 5 is kept too.
	want := []string{opened[2].ID, opened[3].ID, opened[4].ID, opened[5].ID, opened[6].ID, opened[7].ID}
	if len(ids) != len(want) {
		t.Fatalf("compacted book = %v, want %v", ids, want)
	}
	for i := range want {
		if ids[i] != want[i] {
			t.Fatalf("compacted book = %v, want %v", ids, want)
		}
	}
	data, _ := os.ReadFile(filepath.Join(s.Dir, ExchangeFile))
	lines := 0
	for _, b := range data {
		if b == '\n' {
			lines++
		}
	}
	if lines > compactAt {
		t.Fatalf("book still holds %d lines after compaction", lines)
	}
	var x Exchange
	if err := json.Unmarshal(data[:len(data)-1][lastLineStart(data):], &x); err != nil {
		t.Fatalf("compacted book does not end in a whole line: %v", err)
	}
}

func lastLineStart(data []byte) int {
	for i := len(data) - 2; i >= 0; i-- {
		if data[i] == '\n' {
			return i + 1
		}
	}
	return 0
}

func TestReturnedIsMarkedEvenAfterTheExchangeSettled(t *testing.T) {
	s := Store{Dir: t.TempDir()}
	ctx := context.Background()
	x, _, err := s.OpenExchange(ctx, ask(newAgentID(), newAgentID(), ""), "hi")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.SetExchangeState(ctx, x.ID, ExchangeReplied, time.Time{}); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := s.MarkReturned(ctx, x.ID); err != nil {
			t.Fatal(err)
		}
	}
	book, err := s.Exchanges()
	if err != nil || len(book) != 1 || !book[0].Returned || book[0].State != ExchangeReplied {
		t.Fatalf("book = %+v, %v", book, err)
	}
	if err := s.MarkReturned(ctx, "x-0000000000000000"); !errors.Is(err, ErrUnknownExchange) {
		t.Fatalf("unknown exchange = %v", err)
	}
	data, _ := os.ReadFile(filepath.Join(s.Dir, ExchangeFile))
	if n := len(data) - len(strings.ReplaceAll(string(data), "\n", "")); n != 3 {
		t.Fatalf("marking twice wrote %d lines, want 3", n)
	}
}

func TestEveryApprovalModeIsAnAcceptedCeiling(t *testing.T) {
	s := Store{Dir: t.TempDir()}
	for _, mode := range []string{"", "readOnly", "dontAsk", "ask", "auto", "yolo"} {
		x := ask(ByUser, newAgentID(), "")
		x.Ceiling = mode
		if _, _, err := s.OpenExchange(context.Background(), x, mode); err != nil {
			t.Errorf("ceiling %q: %v", mode, err)
		}
	}
}
