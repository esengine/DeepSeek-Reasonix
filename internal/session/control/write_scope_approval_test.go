package control

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/event"
	"reasonix/internal/contract/tool"
	"reasonix/internal/safety/permission"
)

type scopeHarness struct {
	c        *Controller
	requests chan event.Approval
	primary  string
	folder   func() string
}

func newScopeHarness(t *testing.T) *scopeHarness {
	t.Helper()
	t.Setenv("REASONIX_HOME", testenv.TempDir(t))
	h := &scopeHarness{requests: make(chan event.Approval, 4), primary: testenv.TempDir(t), folder: tempAside(t)}
	h.c = New(Options{WorkspaceRoot: h.primary, Sink: event.FuncSink(func(e event.Event) {
		if e.Kind == event.ApprovalRequest {
			h.requests <- e.Approval
		}
	})})
	t.Cleanup(h.c.Close)
	return h
}

func (h *scopeHarness) approver() fileWriteApprover { return fileWriteApprover{h.c} }

type scopeResult struct {
	allow  bool
	reason string
	err    error
}

// ask puts the write-scope question for a file in dir and answers it the way a
// frontend would; answer == nil leaves the card unanswered and returns it.
func (h *scopeHarness) ask(t *testing.T, dir, name string, answer func(id string)) (event.Approval, scopeResult) {
	t.Helper()
	done := make(chan scopeResult, 1)
	go func() {
		allow, reason, err := h.approver().ApproveWriteScope(context.Background(), tool.WriteScopeRequest{Path: filepath.Join(dir, name), Dir: dir})
		done <- scopeResult{allow, reason, err}
	}()
	var card event.Approval
	select {
	case card = <-h.requests:
	case <-time.After(30 * time.Second):
		t.Fatal("the write-scope question was not asked")
	}
	if answer == nil {
		return card, scopeResult{}
	}
	answer(card.ID)
	select {
	case got := <-done:
		return card, got
	case <-time.After(30 * time.Second):
		t.Fatal("the answer did not release the write")
	}
	return card, scopeResult{}
}

func TestWriteScopeApprovalIsAFreshHumanDecisionThatNeverGrantsToolWide(t *testing.T) {
	if !RequiresFreshHumanApprovalTool(WriteScopeApprovalTool) {
		t.Fatal("write_scope must require a fresh human decision")
	}
	if session, persist := ApprovalGrants(WriteScopeApprovalTool, true); !session || !persist {
		t.Fatalf("grants = %v %v, want a folder session grant and a remembered one", session, persist)
	}
	a := newApprovalManager(permission.Policy{}, ToolApprovalYolo, time.Minute, true)
	if a.preApprovedForDecision(WriteScopeApprovalTool, "/etc/wsl.conf", nil, true) {
		t.Fatal("YOLO must not answer a write outside the scope")
	}
	// A generic session grant is tool-wide; one written for this tool by mistake
	// must still answer nothing, or a single click would open every folder.
	a.grantSession(WriteScopeApprovalTool, "/etc")
	if a.preApprovedForDecision(WriteScopeApprovalTool, "/anywhere/else", nil, true) {
		t.Fatal("a generic grant must not cover writes outside the scope")
	}
	gate := NewHeadlessPermissionGate(permission.Policy{Mode: permission.Ask})
	if allow, _, err := gate.Check(t.Context(), WriteScopeApprovalTool, nil, false); err != nil || allow {
		t.Fatalf("the headless gate answered the question: allow=%v err=%v", allow, err)
	}
}

func TestWriteScopeCardNamesTheFileAndTheFolderItWouldCover(t *testing.T) {
	h := newScopeHarness(t)
	dir := h.folder()
	card, _ := h.ask(t, dir, "wsl.conf", func(id string) { h.c.Approve(id, false, false, false) })
	if card.Tool != WriteScopeApprovalTool || card.Subject != filepath.Join(dir, "wsl.conf") || !card.Fresh {
		t.Fatalf("card = %+v", card)
	}
	if !strings.Contains(card.Reason, dir) {
		t.Fatalf("reason %q should name the folder a wider answer covers", card.Reason)
	}
	var in map[string]string
	if err := json.Unmarshal(card.RawInput, &in); err != nil || in["dir"] != dir || in["path"] != filepath.Join(dir, "wsl.conf") {
		t.Fatalf("raw input = %s", card.RawInput)
	}
	if !card.AllowsSession || !card.AllowsPersist {
		t.Fatalf("an ordinary folder should offer both wider answers: %+v", card)
	}
}

func TestWriteScopeOnceAllowsOnlyThisWrite(t *testing.T) {
	h := newScopeHarness(t)
	dir := h.folder()
	_, got := h.ask(t, dir, "a.txt", func(id string) { h.c.Approve(id, true, false, false) })
	if got.err != nil || !got.allow {
		t.Fatalf("once = %+v", got)
	}
	if h.approver().WriteScopeSessionAllowed(context.Background(), tool.WriteScopeRequest{Path: filepath.Join(dir, "b.txt"), Dir: dir}) {
		t.Fatal("a one-time answer was remembered")
	}
	if roots, _ := h.c.WorkspaceRoots(); len(roots.Extra) != 0 {
		t.Fatalf("a one-time answer was written down: %+v", roots)
	}
}

func TestWriteScopeSessionAllowsThatFolderAndBelowButNotAbove(t *testing.T) {
	h := newScopeHarness(t)
	dir := h.folder()
	_, got := h.ask(t, dir, "a.txt", func(id string) { h.c.Approve(id, true, true, false) })
	if got.err != nil || !got.allow {
		t.Fatalf("session = %+v", got)
	}
	allowed := func(d string) bool {
		return h.approver().WriteScopeSessionAllowed(context.Background(), tool.WriteScopeRequest{Path: filepath.Join(d, "x"), Dir: d})
	}
	for _, d := range []string{dir, filepath.Join(dir, "sub"), filepath.Join(dir, "sub", "deeper")} {
		if !allowed(d) {
			t.Errorf("%s should ride the folder's grant", d)
		}
	}
	for _, d := range []string{filepath.Dir(dir), dir + "-sibling", h.folder()} {
		if allowed(d) {
			t.Errorf("%s must not ride a grant for %s", d, dir)
		}
	}
	if roots, _ := h.c.WorkspaceRoots(); len(roots.Extra) != 0 {
		t.Fatalf("a session answer was written down: %+v", roots)
	}
}

func TestWriteScopeAlwaysRecordsTheFolderForThisProjectAndAllowsItNow(t *testing.T) {
	h := newScopeHarness(t)
	dir := h.folder()
	_, got := h.ask(t, dir, "a.txt", func(id string) { h.c.Approve(id, true, false, true) })
	if got.err != nil || !got.allow {
		t.Fatalf("always = %+v", got)
	}
	roots, err := h.c.WorkspaceRoots()
	if err != nil || !slices.Contains(roots.Extra, dir) {
		t.Fatalf("project roots = %+v, %v; want %s recorded", roots, err, dir)
	}
	if !h.approver().WriteScopeSessionAllowed(context.Background(), tool.WriteScopeRequest{Path: filepath.Join(dir, "b"), Dir: dir}) {
		t.Fatal("the running session should honour a folder it just recorded, before any rebuild")
	}
}

func TestWriteScopeDeclineGrantsNothingAndTellsTheModelNotToRetry(t *testing.T) {
	h := newScopeHarness(t)
	dir := h.folder()
	_, got := h.ask(t, dir, "a.txt", func(id string) { h.c.Approve(id, false, true, true) })
	if got.err != nil || got.allow || strings.TrimSpace(got.reason) == "" {
		t.Fatalf("decline = %+v", got)
	}
	if h.approver().WriteScopeSessionAllowed(context.Background(), tool.WriteScopeRequest{Path: filepath.Join(dir, "b"), Dir: dir}) {
		t.Fatal("a refusal that carried session/persist flags still granted")
	}
	if roots, _ := h.c.WorkspaceRoots(); len(roots.Extra) != 0 {
		t.Fatalf("a refusal was written down: %+v", roots)
	}
}

func TestWriteScopeAutoApprovalCannotAnswer(t *testing.T) {
	h := newScopeHarness(t)
	h.c.SetAutoApproveTools(true)
	dir := h.folder()
	done := make(chan scopeResult, 1)
	go func() {
		allow, reason, err := h.approver().ApproveWriteScope(context.Background(), tool.WriteScopeRequest{Path: filepath.Join(dir, "a"), Dir: dir})
		done <- scopeResult{allow, reason, err}
	}()
	var card event.Approval
	select {
	case card = <-h.requests:
	case <-time.After(30 * time.Second):
		t.Fatal("full approval skipped the question")
	}
	select {
	case got := <-done:
		t.Fatalf("auto approval answered a write outside the scope: %+v", got)
	case <-time.After(50 * time.Millisecond):
	}
	h.c.Approve(card.ID, false, false, false)
	<-done
}

func TestWriteScopeDoesNotOfferOrHonourAWiderAnswerForAFolderNobodyShouldGrant(t *testing.T) {
	h := newScopeHarness(t)
	userHome := testenv.TempDir(t)
	t.Setenv("HOME", userHome)
	t.Setenv("USERPROFILE", userHome)
	card, got := h.ask(t, userHome, ".bashrc", func(id string) { h.c.Approve(id, true, true, true) })
	if card.AllowsSession || card.AllowsPersist {
		t.Fatalf("a home directory was offered a wider answer: %+v", card)
	}
	if got.err != nil || !got.allow {
		t.Fatalf("the one write should still be allowed: %+v", got)
	}
	if h.approver().WriteScopeSessionAllowed(context.Background(), tool.WriteScopeRequest{Path: filepath.Join(userHome, "other"), Dir: userHome}) {
		t.Fatal("a frontend that sent session=true for a home directory got a grant the card never offered")
	}
	if roots, _ := h.c.WorkspaceRoots(); len(roots.Extra) != 0 {
		t.Fatalf("a home directory was recorded: %+v", roots)
	}
}

func TestWriteScopeFolderGrantsFollowTheSessionAcrossARebuildAndCanBeRevoked(t *testing.T) {
	h := newScopeHarness(t)
	dir := h.folder()
	h.ask(t, dir, "a.txt", func(id string) { h.c.Approve(id, true, true, false) })
	carried := h.c.SessionAuthorizations()
	if !slices.Contains(carried.WriteDirs, dir) {
		t.Fatalf("carried = %+v", carried)
	}
	fresh := newApprovalManager(permission.Policy{}, ToolApprovalAsk, time.Minute, true)
	fresh.restoreSessionAuthorizations(carried)
	if !fresh.writeDirAllows(filepath.Join(dir, "sub")) {
		t.Fatal("a rebuilt session lost the folder it had been given")
	}
	if n := fresh.revokeSessionGrant(""); n == 0 || fresh.writeDirAllows(dir) {
		t.Fatalf("revoking every session grant left the folder open (revoked %d)", n)
	}
	_ = os.Remove(dir)
}

func TestWriteScopeQuestionQueuedBehindAFolderGrantRidesIt(t *testing.T) {
	h := newScopeHarness(t)
	dir := h.folder()
	type outcome struct {
		allow bool
		err   error
	}
	first, second := make(chan outcome, 1), make(chan outcome, 1)
	go func() {
		allow, _, err := h.approver().ApproveWriteScope(context.Background(), tool.WriteScopeRequest{Path: filepath.Join(dir, "a.txt"), Dir: dir})
		first <- outcome{allow, err}
	}()
	var card event.Approval
	select {
	case card = <-h.requests:
	case <-time.After(30 * time.Second):
		t.Fatal("the first write was not asked about")
	}
	go func() {
		allow, _, err := h.approver().ApproveWriteScope(context.Background(), tool.WriteScopeRequest{Path: filepath.Join(dir, "b.txt"), Dir: dir})
		second <- outcome{allow, err}
	}()
	time.Sleep(100 * time.Millisecond)
	h.c.Approve(card.ID, true, true, false)
	for name, ch := range map[string]chan outcome{"first": first, "second": second} {
		select {
		case got := <-ch:
			if got.err != nil || !got.allow {
				t.Fatalf("%s write = %+v", name, got)
			}
		case <-time.After(30 * time.Second):
			t.Fatalf("%s write was never released", name)
		}
	}
	select {
	case again := <-h.requests:
		t.Fatalf("the second write in the same folder was asked about again: %+v", again)
	default:
	}
}

func (h *scopeHarness) askDelete(t *testing.T, paths []string, answer func(id string)) (event.Approval, scopeResult) {
	t.Helper()
	done := make(chan scopeResult, 1)
	go func() {
		allow, reason, err := h.approver().ApproveDeleteScope(context.Background(), tool.DeleteScopeRequest{Paths: paths})
		done <- scopeResult{allow, reason, err}
	}()
	var card event.Approval
	if answer != nil {
		select {
		case card = <-h.requests:
		case <-time.After(30 * time.Second):
			t.Fatal("the delete question was not asked")
		}
		answer(card.ID)
	}
	select {
	case got := <-done:
		return card, got
	case <-time.After(30 * time.Second):
		t.Fatal("the delete question did not resolve")
	}
	return card, scopeResult{}
}

func TestDeleteScopeApprovalIsAFreshHumanDecisionForThisCommandOnly(t *testing.T) {
	if !RequiresFreshHumanApprovalTool(DeleteScopeApprovalTool) {
		t.Fatal("delete_scope must require a fresh human decision")
	}
	if session, persist := ApprovalGrants(DeleteScopeApprovalTool, true); session || persist {
		t.Fatalf("grants = %v %v: a delete answer must cover one command and nothing after it", session, persist)
	}
	a := newApprovalManager(permission.Policy{}, ToolApprovalYolo, time.Minute, true)
	if a.preApprovedForDecision(DeleteScopeApprovalTool, "/srv/data", nil, true) {
		t.Fatal("full access must not answer a recursive delete outside the scope")
	}
}

func TestDeleteScopeCardNamesEveryTargetAndOffersNothingWider(t *testing.T) {
	h := newScopeHarness(t)
	one, two := filepath.Join(h.folder(), "nas"), filepath.Join(h.folder(), "askpass.sh")
	card, got := h.askDelete(t, []string{one, two}, func(id string) { h.c.Approve(id, true, true, true) })
	if got.err != nil || !got.allow {
		t.Fatalf("allowed delete = %+v", got)
	}
	if card.Tool != DeleteScopeApprovalTool || !card.Fresh || card.AllowsSession || card.AllowsPersist {
		t.Fatalf("card = %+v", card)
	}
	if !strings.Contains(card.Subject, one) || !strings.Contains(card.Subject, two) || !strings.Contains(card.Reason, one) {
		t.Fatalf("card should name both targets: subject=%q reason=%q", card.Subject, card.Reason)
	}
	// session=true/persist=true on the reply grants nothing: the next delete asks again.
	again, _ := h.askDelete(t, []string{one}, func(id string) { h.c.Approve(id, false, false, false) })
	if again.ID == card.ID {
		t.Fatal("the second delete rode the first one's answer")
	}
}

func TestDeleteScopeDeclineCarriesAReasonForTheModel(t *testing.T) {
	h := newScopeHarness(t)
	_, got := h.askDelete(t, []string{filepath.Join(h.folder(), "x")}, func(id string) { h.c.Approve(id, false, false, false) })
	if got.err != nil || got.allow || strings.TrimSpace(got.reason) == "" {
		t.Fatalf("decline = %+v", got)
	}
}

func TestDeleteScopeNeverAsksAboutAPathNoApprovalMayOpen(t *testing.T) {
	h := newScopeHarness(t)
	userHome := testenv.TempDir(t)
	t.Setenv("HOME", userHome)
	t.Setenv("USERPROFILE", userHome)
	_, got := h.askDelete(t, []string{userHome}, nil)
	if !errors.Is(got.err, tool.ErrDeleteTargetProtected) || got.allow {
		t.Fatalf("a home directory should be refused as protected: %+v", got)
	}
	select {
	case card := <-h.requests:
		t.Fatalf("the question was put for a protected path: %+v", card)
	default:
	}
}
