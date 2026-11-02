package teamroster

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func post(from, text string) GroupLine { return GroupLine{From: from, Kind: LinePost, Text: text} }

func seqs(lines []GroupLine) []int64 {
	out := make([]int64, 0, len(lines))
	for _, l := range lines {
		out = append(out, l.Seq)
	}
	return out
}

func sameSeqs(got []GroupLine, want ...int64) bool {
	s := seqs(got)
	if len(s) != len(want) {
		return false
	}
	for i := range want {
		if s[i] != want[i] {
			return false
		}
	}
	return true
}

func TestTheGroupLogNumbersLinesInOrderAndPages(t *testing.T) {
	s, g := Store{Dir: t.TempDir()}, newGroupID()
	ctx := context.Background()
	member := newAgentID()
	for i, l := range []GroupLine{
		post(ByUser, "plan the launch"),
		{From: member, Kind: LinePost, Text: "on it", Mentions: []string{newAgentID()}},
		{From: member, Kind: LineEvent, Event: EventWorking, Code: "team.unavailable"},
		{From: member, Kind: LinePost, Text: "draft is up", Late: true},
		{From: ByUser, Kind: LineEvent, Event: EventPaused, Count: 6},
	} {
		got, err := s.AppendLine(ctx, g, l)
		if err != nil || got.Seq != int64(i+1) || got.At.IsZero() {
			t.Fatalf("AppendLine #%d = %+v, %v", i+1, got, err)
		}
	}
	if got, err := s.Lines(g, 2, 0); err != nil || !sameSeqs(got, 3, 4, 5) {
		t.Fatalf("Lines(after 2) = %v, %v", seqs(got), err)
	}
	if got, _ := s.Lines(g, 0, 2); !sameSeqs(got, 1, 2) {
		t.Fatalf("Lines(limit 2) = %v", seqs(got))
	}
	if got, _ := s.LinesBefore(g, 5, 2); !sameSeqs(got, 3, 4) {
		t.Fatalf("LinesBefore(5, 2) = %v", seqs(got))
	}
	if got, _ := s.LinesBefore(g, 0, 3); !sameSeqs(got, 3, 4, 5) {
		t.Fatalf("LinesBefore(end, 3) = %v", seqs(got))
	}
	all, _ := s.Lines(g, 0, 0)
	if all[1].Mentions[0] == "" || !all[3].Late || all[4].Count != 6 || all[2].Event != EventWorking || all[2].Code != "team.unavailable" {
		t.Fatalf("fields lost on the way through: %+v", all)
	}
	if got, err := s.Lines(newGroupID(), 0, 0); err != nil || got == nil || len(got) != 0 {
		t.Fatalf("a group with no log = %v, %v", got, err)
	}
}

func TestALongPostIsCutAndSaysHowLongItWas(t *testing.T) {
	s, g := Store{Dir: t.TempDir()}, newGroupID()
	text := strings.Repeat("长", MaxLineRunes+7)
	got, err := s.AppendLine(context.Background(), g, post(newAgentID(), text))
	if err != nil || got.Length != MaxLineRunes+7 || []rune(got.Text)[MaxLineRunes-1] != '长' || len([]rune(got.Text)) != MaxLineRunes {
		t.Fatalf("cut post: length %d, runes %d, %v", got.Length, len([]rune(got.Text)), err)
	}
	short, _ := s.AppendLine(context.Background(), g, post(ByUser, "ok"))
	if short.Length != 0 {
		t.Fatalf("a whole post reports Length %d", short.Length)
	}
}

func TestAppendLineRefusesWhatItCannotStore(t *testing.T) {
	s := Store{Dir: t.TempDir()}
	ctx := context.Background()
	cases := map[string]struct {
		group string
		line  GroupLine
	}{
		"group.id":   {"../../etc", post(ByUser, "x")},
		"line.from":  {newGroupID(), post("Chief", "x")},
		"mentions":   {newGroupID(), GroupLine{From: ByUser, Kind: LinePost, Mentions: []string{"Chief"}}},
		"line.kind":  {newGroupID(), GroupLine{From: ByUser, Kind: "note"}},
		"line.event": {newGroupID(), GroupLine{From: ByUser, Kind: LineEvent, Event: "exploded"}},
		"line.code":  {newGroupID(), GroupLine{From: ByUser, Kind: LineEvent, Event: EventFailed, Code: "Not A Code!"}},
	}
	for field, tc := range cases {
		_, err := s.AppendLine(ctx, tc.group, tc.line)
		var fe *FieldError
		if !errors.As(err, &fe) || fe.Field != field {
			t.Errorf("%s: err = %v", field, err)
		}
	}
	if _, err := os.Stat(filepath.Join(s.Dir, groupsDir)); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("a refused line created group files")
	}
}

func TestTheLogSurvivesATornLastLine(t *testing.T) {
	s, g := Store{Dir: t.TempDir()}, newGroupID()
	ctx := context.Background()
	if _, err := s.AppendLine(ctx, g, post(ByUser, "one")); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(s.Dir, groupsDir, g, GroupLogFile)
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(`{"seq":2,"from":"user","kind":"po`); err != nil {
		t.Fatal(err)
	}
	f.Close()
	if got, err := s.Lines(g, 0, 0); err != nil || !sameSeqs(got, 1) {
		t.Fatalf("Lines with a torn tail = %v, %v", seqs(got), err)
	}
	next, err := s.AppendLine(ctx, g, post(ByUser, "two"))
	if err != nil || next.Seq != 2 {
		t.Fatalf("append after a torn tail = %+v, %v", next, err)
	}
	if got, err := s.Lines(g, 0, 0); err != nil || !sameSeqs(got, 1, 2) || got[1].Text != "two" {
		t.Fatalf("log after recovery = %+v, %v", got, err)
	}
}

func TestConcurrentAppendsGetDistinctSequenceNumbers(t *testing.T) {
	s, g := Store{Dir: t.TempDir()}, newGroupID()
	const writers = 10
	var wg sync.WaitGroup
	for range writers {
		wg.Go(func() {
			if _, err := s.AppendLine(context.Background(), g, post(ByUser, "x")); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	got, err := s.Lines(g, 0, 0)
	if err != nil || len(got) != writers {
		t.Fatalf("%d lines, %v", len(got), err)
	}
	for i, l := range got {
		if l.Seq != int64(i+1) {
			t.Fatalf("sequence numbers = %v", seqs(got))
		}
	}
}
