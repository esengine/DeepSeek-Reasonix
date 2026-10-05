package control

import (
	"context"
	"testing"
	"time"

	"reasonix/internal/contract/event"
)

// Reading or operating another application is answered by a person in auto,
// once per application for the session, and by YOLO.
func TestComputerUseNeedsAPersonPerApplicationInAuto(t *testing.T) {
	approvals := make(chan event.Approval, 4)
	c := New(Options{Sink: event.FuncSink(func(e event.Event) {
		if e.Kind == event.ApprovalRequest {
			approvals <- e.Approval
		}
	})})
	c.SetToolApprovalMode(ToolApprovalAuto)
	approve := func(tool, app string) chan bool {
		done := make(chan bool, 1)
		go func() {
			allow, _, _ := gateApprover{c}.Approve(context.Background(), tool, app, nil)
			done <- allow
		}()
		return done
	}
	answer := func(session bool) event.Approval {
		t.Helper()
		select {
		case a := <-approvals:
			c.Approve(a.ID, true, session, false)
			return a
		case <-time.After(30 * time.Second):
			t.Fatal("computer use did not ask a person in auto")
		}
		return event.Approval{}
	}

	done := approve("computer_read", "com.apple.Notes")
	if a := answer(true); a.ReasonCode != computerUseApproval || a.Reason != explicitApprovalTexts[computerUseApproval] || a.Subject != "com.apple.Notes" {
		t.Fatalf("approval = %+v, want the application and why a person is needed", a)
	}
	if !<-done {
		t.Fatal("an approved read was refused")
	}
	if allow, _, err := (gateApprover{c}).Approve(context.Background(), "computer_act", "com.apple.Notes", nil); err != nil || !allow {
		t.Fatalf("operating the approved application = (%v, %v), want allow without asking", allow, err)
	}
	done = approve("computer_act", "com.apple.TextEdit")
	answer(false)
	if !<-done {
		t.Fatal("an approved call on another application was refused")
	}

	c.SetToolApprovalMode(ToolApprovalYolo)
	if allow, _, err := (gateApprover{c}).Approve(context.Background(), "computer_act", "com.apple.Mail", nil); err != nil || !allow {
		t.Fatalf("YOLO = (%v, %v), want allow", allow, err)
	}
	select {
	case extra := <-approvals:
		t.Fatalf("asked more than once per application, or under YOLO: %+v", extra)
	default:
	}
}

// What a window may offer is the host's answer, not the window's guess: an
// answer it drops would be a promise the person cannot collect.
func TestAnApprovalSaysWhichGrantsThisHostWillHonour(t *testing.T) {
	cases := []struct {
		tool             string
		fresh            bool
		session, persist bool
	}{
		{tool: "computer_act"},
		{tool: "bash"},
		{tool: memoryRememberTool},
		{tool: SandboxEscapeApprovalTool},
		{tool: "browser_act", fresh: true},
	}
	want := map[string][2]bool{
		"computer_act":            {true, true},
		"bash":                    {true, true},
		memoryRememberTool:        {false, false},
		SandboxEscapeApprovalTool: {true, false},
		"browser_act":             {false, false},
	}
	for _, tc := range cases {
		session, persist := ApprovalGrants(tc.tool, tc.fresh)
		if got := [2]bool{session, persist}; got != want[tc.tool] {
			t.Errorf("ApprovalGrants(%q, fresh=%v) = %v, want %v", tc.tool, tc.fresh, got, want[tc.tool])
		}
	}
}

func TestRequiredHumanApprovalKeepsExactSessionGrantButNotPersistentRule(t *testing.T) {
	session, persist := approvalGrantsForRequest("bash", false, true)
	if !session || persist {
		t.Fatalf("required-human bash grants = session %v persist %v, want true false", session, persist)
	}
}

// And the request carries that answer, so a frontend renders what the host
// will act on rather than a menu of its own.
func TestTheApprovalRequestCarriesItsGrants(t *testing.T) {
	approvals := make(chan event.Approval, 2)
	c := New(Options{Sink: event.FuncSink(func(e event.Event) {
		if e.Kind == event.ApprovalRequest {
			approvals <- e.Approval
		}
	})})
	go func() {
		_, _, _ = gateApprover{c}.Approve(context.Background(), "computer_act", "com.apple.Notes", nil)
	}()
	select {
	case a := <-approvals:
		if !a.AllowsSession {
			t.Error("a call a session grant covers did not say so")
		}
		if a.AllowsPersist {
			t.Error("a controller with nowhere to write a rule offered to write one")
		}
		c.Approve(a.ID, false, false, false)
	case <-time.After(30 * time.Second):
		t.Fatal("no approval arrived")
	}
}

// Where keys reach only the foreground, the card for operating an application
// says it will be brought forward; reading one takes nothing.
func TestOperatingAnApplicationThatTakesTheFrontSaysSo(t *testing.T) {
	was := computerActTakesFront
	t.Cleanup(func() { computerActTakesFront = was })

	computerActTakesFront = true
	if got := ExplicitApprovalCode("computer_act", "notepad.exe"); got != computerFrontApproval {
		t.Fatalf("act where keys need the front = %q, want %q", got, computerFrontApproval)
	}
	if got := ExplicitApprovalCode("computer_read", "notepad.exe"); got != computerUseApproval {
		t.Fatalf("read = %q, want %q", got, computerUseApproval)
	}
	if got := ExplicitApprovalCode("computer_act", "pointer:notepad.exe"); got != computerPointerApproval {
		t.Fatalf("pointer = %q, want %q", got, computerPointerApproval)
	}

	computerActTakesFront = false
	if got := ExplicitApprovalCode("computer_act", "com.apple.Notes"); got != computerUseApproval {
		t.Fatalf("act where keys reach a background application = %q, want %q", got, computerUseApproval)
	}
}
