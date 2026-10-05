package schedule

import (
	"errors"
	"strconv"
	"testing"
	"time"
)

func TestCreateValidation(t *testing.T) {
	p := DefaultPolicy()
	cases := []struct {
		name string
		edit func(*CreateRequest)
		want error
	}{
		{"not human", func(r *CreateRequest) { r.ConfirmedBy = "model" }, ErrNotHuman},
		{"interval below floor", func(r *CreateRequest) { r.Trigger.EverySec = 3599 }, ErrIntervalBelowFloor},
		{"at too soon", func(r *CreateRequest) {
			r.Trigger = Trigger{Kind: TriggerAt, At: epoch.Add(30 * time.Second)}
		}, ErrInvalid},
		{"grant above ceiling", func(r *CreateRequest) { r.Grant.Tools = []string{"bash"} }, ErrGrantExceedsCeil},
		{"read-only bash unavailable", func(r *CreateRequest) { r.Grant.ReadOnlyBash = true }, ErrGrantExceedsCeil},
		{"budget above policy", func(r *CreateRequest) {
			b := p.DefaultBudget()
			b.PerRunTokens++
			r.Budget = &b
		}, ErrInvalid},
		{"empty prompt", func(r *CreateRequest) { r.Prompt = " " }, ErrInvalid},
		{"relative workspace", func(r *CreateRequest) { r.Target.Workspace = "repo" }, ErrInvalid},
		{"unpinned model", func(r *CreateRequest) { r.Model.Model = "" }, ErrInvalid},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			st, _, _ := newTestStore(t)
			req := everyReq()
			tc.edit(&req)
			if _, err := st.Create(t.Context(), p, req); !errors.Is(err, tc.want) {
				t.Fatalf("got %v, want %v", err, tc.want)
			}
			m, _, _ := st.Snapshot(t.Context())
			if len(m.Schedules) != 0 {
				t.Fatal("refused create must not persist")
			}
		})
	}
}

func TestCreateFillsPolicyDefaultsAndExpiry(t *testing.T) {
	st, _, _ := newTestStore(t)
	p := DefaultPolicy()
	sc := mustCreate(t, st, p, everyReq())
	if sc.Status != StatusActive || !sc.ExpiresAt.Equal(epoch.Add(30*24*time.Hour)) {
		t.Fatalf("status/expiry: %+v", sc)
	}
	if sc.Budget != p.DefaultBudget() || sc.Confirmed.Digest != Digest(sc) {
		t.Fatalf("budget/digest: %+v", sc)
	}
	if len(sc.Grant.ReadRoots) != 1 || sc.Grant.ReadRoots[0] != sc.Target.Workspace {
		t.Fatalf("read roots default to the workspace: %v", sc.Grant.ReadRoots)
	}
}

func TestCreateEnforcesScheduleLimit(t *testing.T) {
	st, _, _ := newTestStore(t)
	p := DefaultPolicy()
	p.MaxSchedules = 2
	mustCreate(t, st, p, everyReq())
	mustCreate(t, st, p, everyReq())
	if _, err := st.Create(t.Context(), p, everyReq()); !errors.Is(err, ErrTooMany) {
		t.Fatalf("got %v", err)
	}
}

func TestTriggerSlots(t *testing.T) {
	tr := Trigger{Kind: TriggerEvery, EverySec: 3600, Anchor: epoch}
	if _, ok := tr.LatestSlot(epoch.Add(59 * time.Minute)); ok {
		t.Fatal("no slot before the first interval")
	}
	got, ok := tr.LatestSlot(epoch.Add(5*time.Hour + 20*time.Minute))
	if !ok || !got.Equal(epoch.Add(5*time.Hour)) {
		t.Fatalf("latest slot: %v %v", got, ok)
	}
	if !tr.IsSlot(epoch.Add(2*time.Hour)) || tr.IsSlot(epoch.Add(2*time.Hour+time.Second)) || tr.IsSlot(epoch) {
		t.Fatal("IsSlot must accept exactly anchor + k*every, k>=1")
	}
	at := Trigger{Kind: TriggerAt, At: epoch.Add(time.Hour)}
	if _, ok := at.LatestSlot(epoch); ok {
		t.Fatal("at trigger not due yet")
	}
	if id := TriggerID("sch_x", got); id != "sch_x/"+strconv.FormatInt(got.Unix(), 10) || id != TriggerID("sch_x", got.In(time.FixedZone("x", 3600))) {
		t.Fatalf("trigger id must be deterministic across zones: %q", id)
	}
}

func TestOneShotIsSpentAfterClaim(t *testing.T) {
	st, clk, _ := newTestStore(t)
	p := DefaultPolicy()
	req := everyReq()
	req.Trigger = Trigger{Kind: TriggerAt, At: epoch.Add(2 * time.Hour)}
	sc := mustCreate(t, st, p, req)
	clk.Advance(2 * time.Hour)
	if _, err := st.Claim(t.Context(), p, sc.ID, sc.Trigger.At); err != nil {
		t.Fatal(err)
	}
	m, _, _ := st.Snapshot(t.Context())
	if m.Schedules[0].Status != StatusExpired {
		t.Fatalf("status %s", m.Schedules[0].Status)
	}
	if err := st.Renew(t.Context(), p, sc.ID); !errors.Is(err, ErrNotActive) {
		t.Fatalf("spent one-shot must not renew: %v", err)
	}
}

func TestCreateAcceptsUserLoweredInterval(t *testing.T) {
	st, _, _ := newTestStore(t)
	p, _, _ := Resolve(Overrides{MinIntervalMinutes: new(int64(15))}, Overrides{})
	req := everyReq()
	req.Trigger.EverySec = 15 * 60
	mustCreate(t, st, p, req)
	req.Trigger.EverySec = 14 * 60
	if _, err := st.Create(t.Context(), p, req); !errors.Is(err, ErrIntervalBelowFloor) {
		t.Fatalf("got %v", err)
	}
}
