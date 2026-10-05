package control

import (
	"context"
	"strings"
	"testing"
	"time"

	"reasonix/internal/contract/event"
	"reasonix/internal/safety/permission"
)

// Approving a site for the session answers for loading, reading and operating
// that site, and for no other site.
func TestApprovalSessionGrantScopesTheBrowserToOneOrigin(t *testing.T) {
	c, ids, prompts := approvalIDs()
	go func() {
		c.Approve(<-ids, true, true, false) // allow https://example.com for this session
		c.Approve(<-ids, true, false, false)
	}()

	calls := []struct{ tool, origin string }{
		{"browser_open", "https://example.com"},
		{"browser_act", "https://example.com"},
		{"browser_open", "https://example.com"},
		{"browser_act", "https://other.example"},
	}
	for i, call := range calls {
		allow, _, err := gateApprover{c}.Approve(context.Background(), call.tool, call.origin, nil)
		if err != nil || !allow {
			t.Fatalf("call %d = (%v,%v), want allow", i, allow, err)
		}
	}
	if *prompts != 2 {
		t.Errorf("prompted %d times, want 2 (one per origin)", *prompts)
	}
}

// Typing a secret into a site is answered by a person in auto, and by YOLO,
// but never by the grant that let the agent onto the site.
func TestBrowserCredentialEntryNeedsAPersonInAuto(t *testing.T) {
	const site = "https://bank.example"
	credential := permission.BrowserCredentialPrefix + site
	approvals := make(chan event.Approval, 2)
	c := New(Options{Sink: event.FuncSink(func(e event.Event) {
		if e.Kind == event.ApprovalRequest {
			approvals <- e.Approval
		}
	})})
	c.SetToolApprovalMode(ToolApprovalAuto)
	c.approval.grantSession("browser_open", site)

	done := make(chan bool, 1)
	go func() {
		allow, _, _ := gateApprover{c}.Approve(context.Background(), "browser_act", credential, nil)
		done <- allow
	}()
	var approval event.Approval
	select {
	case approval = <-approvals:
	case <-time.After(30 * time.Second):
		t.Fatal("credential entry did not ask a person in auto, despite a grant for the site")
	}
	if approval.Reason != explicitApprovalReason("browser_act", credential) || approval.ReasonCode != browserCredentialApprova {
		t.Fatalf("approval reason = %q", approval.Reason)
	}
	c.Approve(approval.ID, true, false, false)
	if !<-done {
		t.Fatal("an approved credential entry was refused")
	}

	c.SetToolApprovalMode(ToolApprovalYolo)
	allow, _, err := gateApprover{c}.Approve(context.Background(), "browser_act", credential, nil)
	if err != nil || !allow {
		t.Fatalf("YOLO credential entry = (%v, %v), want allow", allow, err)
	}
	select {
	case extra := <-approvals:
		t.Fatalf("YOLO still asked: %+v", extra)
	default:
	}
}

func TestBrowserScriptCarriesItsOwnApprovalIdentity(t *testing.T) {
	subject := permission.BrowserScriptPrefix + "https://example.com"
	if got := ExplicitApprovalCode("browser_act", subject); got != browserScriptApproval {
		t.Fatalf("approval code = %q, want %q", got, browserScriptApproval)
	}
	if !strings.Contains(explicitApprovalReason("browser_act", subject), "arbitrary JavaScript") {
		t.Fatal("script approval did not explain the authority being granted")
	}
}

func TestUnattendedRefusalNamesTheSecret(t *testing.T) {
	_, _, reason, _ := denyPermissionApprover{}.ApproveWithReason(context.Background(), "browser_act", permission.BrowserCredentialPrefix+"https://bank.example", nil)
	if !strings.HasPrefix(reason, explicitApprovalReason("browser_act", permission.BrowserCredentialPrefix+"https://bank.example")) {
		t.Fatalf("unattended refusal = %q, want it to lead with why a person was needed", reason)
	}
	_, _, plain, _ := denyPermissionApprover{}.ApproveWithReason(context.Background(), "browser_act", "https://bank.example", nil)
	if strings.Contains(plain, explicitApprovalTexts[browserCredentialApprova]) {
		t.Fatalf("an ordinary refusal claimed a secret: %q", plain)
	}
}
