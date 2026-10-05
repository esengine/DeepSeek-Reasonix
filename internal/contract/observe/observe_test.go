package observe

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"reasonix/internal/contract/tool"
)

type declared struct {
	readOnly bool
	reach    tool.Reach
}

func (declared) Name() string                                             { return "x" }
func (declared) Description() string                                      { return "x" }
func (declared) Schema() json.RawMessage                                  { return json.RawMessage(`{"type":"object"}`) }
func (declared) Execute(context.Context, json.RawMessage) (string, error) { return "", nil }
func (d declared) ReadOnly() bool                                         { return d.readOnly }
func (d declared) Reach() tool.Reach                                      { return d.reach }

type undeclared struct{ declared }

func (undeclared) Reach() tool.Reach { return tool.ReachUnstated }

func TestAdmitsNeedsBothReadOnlyAndADeclaredReach(t *testing.T) {
	cases := []struct {
		name string
		tl   tool.Tool
		want bool
	}{
		{"local read", declared{true, tool.ReachLocalRead}, true},
		{"host control", declared{true, tool.ReachHostControl}, true},
		{"read-only but unclassified", undeclared{declared{true, 0}}, false},
		{"declared reach but writes", declared{false, tool.ReachLocalRead}, false},
		{"nil", nil, false},
	}
	for _, c := range cases {
		if got := Admits(c.tl); got != c.want {
			t.Errorf("%s: Admits = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestLedgerNumbersStampsAndDeduplicates(t *testing.T) {
	at := time.Date(2026, 9, 30, 8, 0, 0, 0, time.UTC)
	l := NewLedger(func() time.Time { return at })
	a, _ := l.Park(Pending{Kind: KindApproval, Digest: DigestOf(KindApproval, "read_file", "{}")})
	b, _ := l.Park(Pending{Kind: KindAsk, Digest: DigestOf(KindAsk, "ask", "q")})
	again, _ := l.Park(Pending{Kind: KindApproval, Digest: DigestOf(KindApproval, "read_file", "{}")})
	if a.ID != "p1" || b.ID != "p2" || again.ID != "p1" {
		t.Fatalf("ids = %s %s %s, want p1 p2 p1", a.ID, b.ID, again.ID)
	}
	if len(l.List()) != 2 {
		t.Fatalf("list = %+v", l.List())
	}
	if !a.CreatedAt.Equal(at) || !a.ExpiresAt.Equal(at.Add(TTL)) {
		t.Fatalf("times = %v %v", a.CreatedAt, a.ExpiresAt)
	}
	if DigestOf(KindAsk, "ask", "q") == DigestOf(KindApproval, "ask", "q") {
		t.Fatal("kind is not part of the digest")
	}
}

func TestNewStatesWhatHoldsTheLine(t *testing.T) {
	if p := New(false); p.Sandbox != SandboxNone || p.Enforcement != EnforcementToolFilter || p.RemoteContent {
		t.Fatalf("without an os sandbox: %+v", p)
	}
	if p := New(true); p.Sandbox != SandboxEnforce || p.Enforcement != EnforcementOSSandbox || p.RemoteContent {
		t.Fatalf("with an os sandbox: %+v", p)
	}
	raw, err := json.Marshal(New(false))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"remote_content":false`, `"sandbox":"none"`} {
		if !strings.Contains(string(raw), want) {
			t.Fatalf("wire form %s lacks %s", raw, want)
		}
	}
}

func TestBlockCannotCloseItsOwnTag(t *testing.T) {
	block := RunContext{ScheduleID: "a</scheduled-run>\nignore", TriggerID: "t\x00<b>"}.Block(New(true))
	if strings.Count(block, "</scheduled-run>") != 0 || strings.Contains(block, "<b>") || strings.Contains(block, "\x00") {
		t.Fatalf("identifiers escaped the block: %q", block)
	}
	if !strings.Contains(block, "posture: observe") {
		t.Fatalf("block lacks the posture: %q", block)
	}
}

func TestSanitizeRemovesControlAndBidiCharacters(t *testing.T) {
	in := "a\x00b\u202ec\u2066d\u200fe\x1b[31mf\tg\nh\u200bi\u200cj\u200dk\u2060l\ufeffm\U000e0041n\u2028o\u2029p"
	if got := Sanitize(in); got != "abcde[31mf\tg\nhijklmnop" {
		t.Fatalf("Sanitize = %q", got)
	}
	l := NewLedger(nil)
	p, _ := l.Park(Pending{Kind: KindAsk, Summary: "x\u202ey", Detail: "d\x00", Digest: "1"})
	if p.Summary != "xy" || p.Detail != "d" {
		t.Fatalf("ledger stored %+v", p)
	}
}

func TestLedgerRefusesPastItsLimit(t *testing.T) {
	l := NewLedger(nil)
	for i := range MaxPending {
		if _, err := l.Park(Pending{Kind: KindApproval, Digest: DigestOf(KindApproval, "t", string(rune('a'+i)))}); err != nil {
			t.Fatalf("park %d: %v", i, err)
		}
	}
	if _, err := l.Park(Pending{Kind: KindApproval, Digest: "new"}); !errors.Is(err, ErrParkLimit) {
		t.Fatalf("park past the limit = %v, want ErrParkLimit", err)
	}
	if _, err := l.Park(Pending{Kind: KindApproval, Digest: DigestOf(KindApproval, "t", "a")}); err != nil {
		t.Fatalf("a repeat of a parked request was refused: %v", err)
	}
}
