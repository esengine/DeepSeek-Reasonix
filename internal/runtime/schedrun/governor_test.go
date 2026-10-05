package schedrun

import (
	"errors"
	"io"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"reasonix/internal/contract/observe"
)

func TestGovernorStopsOnceAtTheCeiling(t *testing.T) {
	var stops atomic.Int32
	g := NewGovernor(1000, func() { stops.Add(1) })
	g.AddUsage(600, true)
	if stops.Load() != 0 || g.OverBudget() {
		t.Fatal("stopped below the ceiling")
	}
	g.AddUsage(-5000, true)
	g.AddUsage(400, true)
	g.AddUsage(400, true)
	if stops.Load() != 1 || !g.OverBudget() || g.Tokens() != 1400 || g.Usages() != 4 {
		t.Fatalf("stops=%d over=%v tokens=%d usages=%d", stops.Load(), g.OverBudget(), g.Tokens(), g.Usages())
	}
}

func TestRepeatedParksOfOneClassEndTheRun(t *testing.T) {
	var stops atomic.Int32
	g := NewGovernor(0, func() { stops.Add(1) })
	sink := g.Sink(observe.NewLedger(nil), 3)
	park := func(kind observe.Kind, source, digest string) {
		t.Helper()
		if _, err := sink.Park(observe.Pending{Kind: kind, Source: source, Digest: digest}); err != nil {
			t.Fatal(err)
		}
	}
	park(observe.KindApproval, "read_file", "a")
	park(observe.KindApproval, "grep", "b")
	park(observe.KindAsk, "ask", "c")
	park(observe.KindApproval, "read_file", "d")
	if stops.Load() != 0 || g.RepeatParked() {
		t.Fatal("different classes were counted as one")
	}
	park(observe.KindApproval, "read_file", "a")
	if stops.Load() != 1 || !g.RepeatParked() {
		t.Fatalf("the third request of one class did not end the run: stops=%d", stops.Load())
	}
}

func TestRepeatLimitBelowOneIsOff(t *testing.T) {
	g := NewGovernor(0, func() { t.Fatal("stopped") })
	sink := g.Sink(observe.NewLedger(nil), 0)
	for range 10 {
		_, _ = sink.Park(observe.Pending{Kind: observe.KindAsk, Source: "ask", Digest: "x"})
	}
}

func TestRepeatSinkPassesTheStoreRefusalOn(t *testing.T) {
	g := NewGovernor(0, nil)
	sink := g.Sink(observe.NewLedger(nil), 3)
	for i := range observe.MaxPending {
		if _, err := sink.Park(observe.Pending{Kind: observe.KindApproval, Source: "s" + string(rune('a'+i)), Digest: string(rune('a' + i))}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := sink.Park(observe.Pending{Kind: observe.KindApproval, Source: "zz", Digest: "zz"}); !errors.Is(err, observe.ErrParkLimit) {
		t.Fatalf("err = %v, want the store's own park limit", err)
	}
}

func TestAwaitGoReleasesOnTheLineAndReportsTheSupervisorLeaving(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	go func() { _, _ = io.WriteString(w, "go tok123\n") }()
	token, gone, err := AwaitGo(r, 5*time.Second)
	if err != nil || token != "tok123" {
		t.Fatalf("token %q, err %v", token, err)
	}
	select {
	case <-gone:
		t.Fatal("reported gone while the supervisor still holds the pipe")
	case <-time.After(100 * time.Millisecond):
	}
	_ = w.Close()
	select {
	case <-gone:
	case <-time.After(5 * time.Second):
		t.Fatal("the end of the stream was not reported")
	}
}

func TestAwaitGoRefusesAnythingButGoAndGivesUp(t *testing.T) {
	if _, _, err := AwaitGo(strings.NewReader("go\n"), time.Second); !errors.Is(err, ErrParentGone) {
		t.Fatalf("wrong line: %v", err)
	}
	if _, _, err := AwaitGo(strings.NewReader(""), time.Second); !errors.Is(err, ErrParentGone) {
		t.Fatalf("closed before go: %v", err)
	}
	r, w, _ := os.Pipe()
	defer r.Close()
	defer w.Close()
	if _, _, err := AwaitGo(r, 100*time.Millisecond); !errors.Is(err, ErrStartTimeout) {
		t.Fatalf("no line: %v", err)
	}
}

func TestReadLinesSkipsStrayOutputAndBoundsLines(t *testing.T) {
	var got []Line
	err := readLines(strings.NewReader("warning: hi\n{\"kind\":\"usage\",\"tokens\":7}\n{\"kind\":\"nope\"}\n"), func(l Line) { got = append(got, l) })
	if err != nil || len(got) != 1 || got[0].Tokens != 7 {
		t.Fatalf("got %+v, %v", got, err)
	}
	if err := readLines(strings.NewReader(strings.Repeat("x", MaxLineBytes+1)+"\n"), func(Line) {}); !errors.Is(err, ErrProtocol) {
		t.Fatalf("oversize: %v", err)
	}
}

func TestGovernorStopsARunWhoseRequestsReportNoUsage(t *testing.T) {
	var stops atomic.Int32
	g := NewGovernor(0, func() { stops.Add(1) })
	g.Committed()
	if stops.Load() != 0 {
		t.Fatal("stopped on the first request: its usage may still be on the way")
	}
	g.Committed()
	if stops.Load() != 1 || !g.Unmetered() {
		t.Fatalf("stops=%d unmetered=%v: two requests with no report between them must stop the run", stops.Load(), g.Unmetered())
	}
	ok := NewGovernor(0, func() { t.Fatal("stopped") })
	ok.Committed()
	ok.AddUsage(10, true)
	ok.Committed()
	ok.AddUsage(10, true)
	if ok.Unmetered() {
		t.Fatal("a metered run reads as unmetered")
	}
}

func TestExitWhenGoneForcesTheProcessOut(t *testing.T) {
	gone := make(chan struct{})
	got := make(chan int, 1)
	ExitWhenGone(gone, 50*time.Millisecond, 73, func(c int) { got <- c })
	select {
	case <-got:
		t.Fatal("exited while the supervisor was alive")
	case <-time.After(100 * time.Millisecond):
	}
	close(gone)
	select {
	case c := <-got:
		if c != 73 {
			t.Fatalf("code %d", c)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("never exited")
	}
}

func TestEstimatesAndSideCallsDoNotStandInForAMeteredRequest(t *testing.T) {
	var stops atomic.Int32
	g := NewGovernor(0, func() { stops.Add(1) })
	for range 5 {
		g.AddUsage(100, false)
	}
	g.Committed()
	g.Committed()
	if stops.Load() != 1 || !g.Unmetered() {
		t.Fatalf("stops=%d unmetered=%v: unmetered requests slipped past on the strength of other usage", stops.Load(), g.Unmetered())
	}
}
