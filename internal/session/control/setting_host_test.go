package control

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/config"
	"reasonix/internal/contract/event"
	"reasonix/internal/contract/tool"
)

type proposal struct {
	receipt tool.SettingReceipt
	err     error
}

func settingController(t *testing.T) (*Controller, chan event.Approval, chan event.Event) {
	t.Helper()
	t.Setenv("REASONIX_HOME", testenv.TempDir(t))
	requests := make(chan event.Approval, 8)
	notices := make(chan event.Event, 8)
	c := New(Options{Sink: event.FuncSink(func(e event.Event) {
		switch e.Kind {
		case event.ApprovalRequest:
			requests <- e.Approval
		case event.Notice:
			notices <- e
		}
	})})
	return c, requests, notices
}

func propose(c *Controller, id, value, reason string) chan proposal {
	done := make(chan proposal, 1)
	go func() {
		r, err := c.ProposeSetting(context.Background(), id, json.RawMessage(value), reason)
		done <- proposal{r, err}
	}()
	return done
}

func userFile() *config.Config { return config.LoadForEdit(config.UserConfigPath()) }

func asRefusal(err error) (tool.Refusal, bool) {
	var r tool.Refusal
	ok := errors.As(err, &r)
	return r, ok
}

func refusalCode(err error) string {
	r, _ := asRefusal(err)
	return r.Code
}

func idle(t *testing.T, done chan proposal, what string) {
	t.Helper()
	select {
	case got := <-done:
		t.Fatalf("%s: answered without a person: %+v", what, got)
	case <-time.After(50 * time.Millisecond):
	}
}

func TestAPreferenceChangeWaitsForAPersonEvenInYoloAndWritesOnTheClick(t *testing.T) {
	c, requests, notices := settingController(t)
	c.SetAutoApproveTools(true)
	was := userFile().UI.ShowReceipt
	done := propose(c, "ui.show_receipt", jsonBool(!was), "the receipt card is in the way")
	a := nextApproval(t, requests)
	if a.Tool != SettingApprovalTool || a.Subject != "ui.show_receipt" || !a.Fresh || !a.AllowsSession || a.AllowsPersist {
		t.Fatalf("approval = %+v", a)
	}
	for _, want := range []string{"ui.show_receipt", "in the way"} {
		if !strings.Contains(a.Reason, want) {
			t.Errorf("the card does not say %q: %q", want, a.Reason)
		}
	}
	idle(t, done, "yolo")
	if userFile().UI.ShowReceipt != was {
		t.Fatal("the file changed before the click")
	}
	c.Approve(a.ID, true, false, false)
	got := <-done
	if got.err != nil || got.receipt.Via != "click" || got.receipt.From != was || got.receipt.To != !was {
		t.Fatalf("receipt = %+v err=%v", got.receipt, got.err)
	}
	if userFile().UI.ShowReceipt == was {
		t.Fatal("the click did not change the file")
	}
	select {
	case n := <-notices:
		if n.Code != event.NoticeCodeSettingChanged || n.Detail != "ui.show_receipt" || !strings.Contains(n.Text, "ui.show_receipt") {
			t.Fatalf("notice = %+v", n)
		}
	case <-time.After(time.Second):
		t.Fatal("no receipt notice")
	}
}

func jsonBool(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

func TestAnElevatedChangeOffersNoSessionGrantAndAsksEveryTime(t *testing.T) {
	c, requests, _ := settingController(t)
	setDispatchSwitch(t, false, false)
	done := propose(c, "peers.enabled", "true", "")
	a := nextApproval(t, requests)
	if a.Tool != SettingElevatedApprovalTool || a.AllowsSession {
		t.Fatalf("approval = %+v, want the elevated card with no session grant", a)
	}
	c.Approve(a.ID, true, true, false)
	if got := <-done; got.err != nil || got.receipt.Via != "click" {
		t.Fatalf("receipt = %+v err=%v", got.receipt, got.err)
	}
	c.settings.proposals.Store(0)
	again := propose(c, "peers.enabled", "false", "")
	b := nextApproval(t, requests)
	if b.Tool != SettingElevatedApprovalTool {
		t.Fatalf("second card = %+v", b)
	}
	c.Approve(b.ID, false, false, false)
	<-again
}

func TestASessionGrantCoversThatOnePreferenceAndNoOther(t *testing.T) {
	c, requests, _ := settingController(t)
	was := userFile().UI.ShowReceipt
	first := propose(c, "ui.show_receipt", jsonBool(!was), "")
	a := nextApproval(t, requests)
	c.Approve(a.ID, true, true, false)
	<-first
	second := <-propose(c, "ui.show_receipt", jsonBool(was), "")
	if second.err != nil || second.receipt.Via != "session_grant" {
		t.Fatalf("second = %+v err=%v, want it applied by the grant", second.receipt, second.err)
	}
	other := propose(c, "ui.show_changed_files", jsonBool(!userFile().UI.ShowChangedFiles), "")
	card := nextApproval(t, requests)
	idle(t, other, "a different setting")
	c.Approve(card.ID, false, false, false)
	<-other
}

func TestAGuardedSettingIsRefusedWithAnOpenRemedyAndNoCard(t *testing.T) {
	c, requests, _ := settingController(t)
	before := userFile().Permissions.Mode
	_, err := c.ProposeSetting(context.Background(), "permissions.mode", json.RawMessage(`"yolo"`), "faster")
	r, ok := asRefusal(err)
	if !ok || r.Code != "setting.guarded" || r.Remedy == nil || r.Remedy.Op != tool.RemedyOpen || r.Remedy.Setting != "permissions.mode" {
		t.Fatalf("err = %#v", err)
	}
	select {
	case a := <-requests:
		t.Fatalf("a card was shown for a boundary setting: %+v", a)
	default:
	}
	if userFile().Permissions.Mode != before {
		t.Fatal("a guarded setting was written")
	}
}

func TestADeclinedChangeLeavesTheFileAndSaysSo(t *testing.T) {
	c, requests, _ := settingController(t)
	was := userFile().UI.ShowReceipt
	done := propose(c, "ui.show_receipt", jsonBool(!was), "")
	a := nextApproval(t, requests)
	c.Approve(a.ID, false, false, false)
	got := <-done
	if refusalCode(got.err) != CodeSettingDeclined || userFile().UI.ShowReceipt != was {
		t.Fatalf("err = %v, show = %v", got.err, userFile().UI.ShowReceipt)
	}
}

func TestAValueTheKeyCannotHoldIsRefusedBeforeAnyCard(t *testing.T) {
	c, requests, _ := settingController(t)
	_, err := c.ProposeSetting(context.Background(), "ui.show_receipt", json.RawMessage(`"maybe"`), "")
	if refusalCode(err) != "setting.invalid_value" {
		t.Fatalf("err = %v", err)
	}
	select {
	case a := <-requests:
		t.Fatalf("a card for an unusable value: %+v", a)
	default:
	}
}

func TestAChangeToTheValueAlreadyHeldAsksNothing(t *testing.T) {
	c, requests, _ := settingController(t)
	got, err := c.ProposeSetting(context.Background(), "ui.show_receipt", json.RawMessage(jsonBool(userFile().UI.ShowReceipt)), "")
	if err != nil || got.Via != "unchanged" {
		t.Fatalf("got = %+v err=%v", got, err)
	}
	select {
	case a := <-requests:
		t.Fatalf("a card for no change: %+v", a)
	default:
	}
}

func TestOnlyThreeProposalsAreAskedPerTurn(t *testing.T) {
	c, requests, _ := settingController(t)
	for range settingProposalsPerTurn {
		d := propose(c, "notifications.sound", "true", "")
		a := nextApproval(t, requests)
		c.Approve(a.ID, false, false, false)
		<-d
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, err := c.ProposeSetting(ctx, "notifications.sound", json.RawMessage(`true`), "")
	if refusalCode(err) != CodeSettingLimit {
		t.Fatalf("fourth proposal: %v", err)
	}
}

func TestTheModelsReasonIsQuotedTidyAndBounded(t *testing.T) {
	c, requests, _ := settingController(t)
	d := propose(c, "ui.show_receipt", jsonBool(!userFile().UI.ShowReceipt), "line one\n\nline   two "+strings.Repeat("x", 400))
	a := nextApproval(t, requests)
	if strings.Contains(a.Reason, "\n\n") || strings.Contains(a.Reason, strings.Repeat("x", 200)) {
		t.Fatalf("the reason was not tidied or bounded: %q", a.Reason)
	}
	c.Approve(a.ID, false, false, false)
	<-d
}

func TestSettingsListFindsByWordsAndNeverShowsASecret(t *testing.T) {
	c, _, _ := settingController(t)
	rows := c.SettingsList("peers dispatch")
	// The switch first, then the two ceilings that share its words.
	if len(rows) != 3 || rows[0].ID != "peers.dispatch" || rows[0].Class != "elevated" || rows[1].ID != "peers.max_dispatch_depth" || rows[2].ID != "peers.max_dispatched_sessions" {
		t.Fatalf("rows = %+v", rows)
	}
	for _, r := range c.SettingsList("serve token") {
		if r.ID == "serve.token" && r.Value != nil {
			t.Fatalf("a secret's value was listed: %+v", r)
		}
	}
}

func TestOpeningASettingEmitsANoticeAndChangesNothing(t *testing.T) {
	c, _, notices := settingController(t)
	before := userFile().Permissions.Mode
	if err := c.OpenSetting("permissions.mode"); err != nil {
		t.Fatal(err)
	}
	n := <-notices
	if n.Code != event.NoticeCodeSettingOpen || n.Detail != "permissions.mode" || userFile().Permissions.Mode != before {
		t.Fatalf("notice = %+v", n)
	}
	if refusalCode(c.OpenSetting("no.such")) != "setting.unknown" {
		t.Fatal("an undeclared id was opened")
	}
}

func TestADisabledDispatchNamesTheSwitchThatWouldLiftIt(t *testing.T) {
	c, requests, _ := settingController(t)
	setDispatchSwitch(t, false, false)
	_, err := peerDirectory{c}.DispatchToProject(context.Background(), "other", "do it")
	r, ok := asRefusal(err)
	if !ok || r.Code != "dispatch.disabled" || r.Remedy == nil || r.Remedy.Setting != "peers.enabled" || r.Remedy.Op != tool.RemedyPropose {
		t.Fatalf("err = %#v, want the refusal to name peers.enabled", err)
	}
	done := propose(c, r.Remedy.Setting, string(r.Remedy.Value), "hand a task to the other project")
	c.Approve(nextApproval(t, requests).ID, true, false, false)
	if got := <-done; got.err != nil || !userFile().Peers.Enabled {
		t.Fatalf("applying the remedy: %+v err=%v enabled=%v", got.receipt, got.err, userFile().Peers.Enabled)
	}
	c.settings.proposals.Store(0)
	_, err = peerDirectory{c}.DispatchToProject(context.Background(), "other", "do it")
	if r, ok = asRefusal(err); !ok || r.Remedy == nil || r.Remedy.Setting != "peers.dispatch" {
		t.Fatalf("with messaging on, err = %#v, want the dispatch switch next", err)
	}
}

func listOf(prefix string, n int) string {
	var b strings.Builder
	b.WriteString("[")
	for i := range n {
		if i > 0 {
			b.WriteString(",")
		}
		fmt.Fprintf(&b, `"%s%03d"`, prefix, i)
	}
	b.WriteString("]")
	return b.String()
}

// What the card shows is what is written: a list whose harmless head fits the
// card and whose last element is the one that matters is refused outright.
func TestAValueTooLongForTheCardToShowIsRefusedBeforeAnyCardOrWrite(t *testing.T) {
	c, requests, _ := settingController(t)
	value := strings.TrimSuffix(listOf("skill-", 80), "]") + `,"the-one-that-matters"]`
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, err := c.ProposeSetting(ctx, "skills.disabled_skills", json.RawMessage(value), "")
	if refusalCode(err) != CodeSettingTooLong {
		t.Fatalf("err = %v, want %s", err, CodeSettingTooLong)
	}
	select {
	case a := <-requests:
		t.Fatalf("a card for a value it cannot show: %+v", a)
	default:
	}
	if len(userFile().Skills.DisabledSkills) != 0 {
		t.Fatal("the long value was written")
	}
}

func TestAValueTheCardCanShowIsShownWhole(t *testing.T) {
	c, requests, _ := settingController(t)
	done := propose(c, "skills.disabled_skills", `["a","b","the-one-that-matters"]`, "")
	a := nextApproval(t, requests)
	if !strings.Contains(a.Reason, "the-one-that-matters") {
		t.Fatalf("the card hides the tail of the value: %q", a.Reason)
	}
	c.Approve(a.ID, false, false, false)
	<-done
}

func TestARawValueBeyondTheParseCapIsRefusedUnread(t *testing.T) {
	c, _, _ := settingController(t)
	huge := `"` + strings.Repeat("x", settingRawBytes) + `"`
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, err := c.ProposeSetting(ctx, "agent.output_style", json.RawMessage(huge), "")
	if refusalCode(err) != CodeSettingTooLong {
		t.Fatalf("err = %v", err)
	}
}

func TestARawValueIsCappedBeforeItIsParsedWhateverItHolds(t *testing.T) {
	c, _, _ := settingController(t)
	padded := strings.Repeat(" ", settingRawBytes) + jsonBool(!userFile().UI.ShowReceipt)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if _, err := c.ProposeSetting(ctx, "ui.show_receipt", json.RawMessage(padded), ""); refusalCode(err) != CodeSettingTooLong {
		t.Fatalf("err = %v", err)
	}
}

// A click authorises the change, not the mode: in plan mode nothing is proposed,
// while reading and pointing stay open.
func TestPlanModeRefusesAProposalButNotListOrOpen(t *testing.T) {
	c, requests, _ := settingController(t)
	c.SetPlanMode(true)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, err := c.ProposeSetting(ctx, "ui.show_receipt", json.RawMessage(jsonBool(!userFile().UI.ShowReceipt)), "")
	if refusalCode(err) != CodeSettingPlanMode {
		t.Fatalf("err = %v, want %s", err, CodeSettingPlanMode)
	}
	select {
	case a := <-requests:
		t.Fatalf("a card in plan mode: %+v", a)
	default:
	}
	if len(c.SettingsList("receipt")) == 0 || c.OpenSetting("permissions.mode") != nil {
		t.Fatal("list and open must stay available in plan mode")
	}
}

func TestListShowsNoValueForABoundarySetting(t *testing.T) {
	c, _, _ := settingController(t)
	cfg := userFile()
	cfg.Agent.SystemPrompt = "secret instructions"
	if err := cfg.SaveTo(config.UserConfigPath()); err != nil {
		t.Fatal(err)
	}
	for _, r := range c.SettingsList("agent.system_prompt") {
		if r.ID == "agent.system_prompt" && (r.Value != nil || r.Class != "guarded") {
			t.Fatalf("row = %+v, want the row without its value", r)
		}
	}
}
