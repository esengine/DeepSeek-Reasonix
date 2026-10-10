package feedback

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"
)

const profileMine = `{"profile":{"level":2,"adoptedCount":4,"currentThreshold":3,"nextLevel":3,"nextThreshold":6,"remaining":2,"trustState":"active","trustExpiresAt":"2026-11-01T00:00:00.000Z","observedAt":"2026-10-07T08:00:00.000Z","effectiveLimits":{"reportsPerHour":6,"reportsPerDay":20,"repliesPerHour":5}},"items":[]}`

func submitted(t *testing.T, st *stub) *Service {
	t.Helper()
	svc, _ := setup(t, st)
	if _, err := svc.Submit(context.Background(), draft()); err != nil {
		t.Fatal(err)
	}
	return svc
}

func TestListMineCarriesTheServicesProfile(t *testing.T) {
	svc := submitted(t, &stub{mineBody: profileMine})
	got, err := svc.ListMine(context.Background())
	if err != nil || got.Profile == nil {
		t.Fatalf("mine = %+v %v", got, err)
	}
	p := got.Profile
	if p.Level != 2 || p.AdoptedCount != 4 || p.CurrentThreshold != 3 || p.NextLevel == nil || *p.NextLevel != 3 ||
		p.NextThreshold == nil || *p.NextThreshold != 6 || p.Remaining == nil || *p.Remaining != 2 || p.TrustState != TrustActive {
		t.Fatalf("profile = %+v", p)
	}
	if p.EffectiveLimits != (EffectiveLimits{ReportsPerHour: 6, ReportsPerDay: 20, RepliesPerHour: 5}) {
		t.Fatalf("limits = %+v", p.EffectiveLimits)
	}
	if p.TrustExpiresAt == nil || !p.TrustExpiresAt.Equal(time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)) || !p.ObservedAt.Equal(time.Date(2026, 10, 7, 8, 0, 0, 0, time.UTC)) {
		t.Fatalf("times = %v %v", p.TrustExpiresAt, p.ObservedAt)
	}
	raw, _ := json.Marshal(got)
	if !strings.Contains(string(raw), `"profile":{"level":2`) {
		t.Fatalf("json = %s", raw)
	}
}

func TestAnAbsentOrMalformedProfileIsUnavailableNeverHigherPrivilege(t *testing.T) {
	bad := map[string]string{
		"absent":            `{"items":[]}`,
		"null":              `{"profile":null,"items":[]}`,
		"wrong type":        `{"profile":{"level":"two"},"items":[]}`,
		"negative level":    strings.Replace(profileMine, `"level":2`, `"level":-1`, 1),
		"half a next":       strings.Replace(profileMine, `"nextThreshold":6`, `"nextThreshold":null`, 1),
		"next not above":    strings.Replace(profileMine, `"nextThreshold":6`, `"nextThreshold":3`, 1),
		"zero limit":        strings.Replace(profileMine, `"reportsPerHour":6`, `"reportsPerHour":0`, 1),
		"current over mine": strings.Replace(profileMine, `"currentThreshold":3`, `"currentThreshold":9`, 1),
		"no observation":    strings.Replace(profileMine, `"observedAt":"2026-10-07T08:00:00.000Z",`, ``, 1),
	}
	for name, body := range bad {
		st := &stub{mineBody: profileMine}
		svc := submitted(t, st)
		if got, _ := svc.ListMine(context.Background()); got.Profile == nil {
			t.Fatal("setup: no profile")
		}
		st.mineBody = body
		got, err := svc.ListMine(context.Background())
		if err != nil || got.Offline || got.Profile != nil {
			t.Errorf("%s: mine = %+v %v", name, got, err)
		}
		if again, _ := svc.ListMine(context.Background()); again.Profile != nil {
			t.Errorf("%s: an unreadable profile left the old one standing", name)
		}
	}
}

func TestAMalformedProfileDoesNotBlankTheList(t *testing.T) {
	body := `{"profile":{"level":"two"},"items":[{"receipt":"FB-7K3M-9QX2","category":"bug","titleSnippet":"x","status":"received","createdAt":"2026-09-30T08:00:00Z","updatedAt":"2026-09-30T08:00:00Z"}]}`
	svc := submitted(t, &stub{mineBody: body})
	got, err := svc.ListMine(context.Background())
	if err != nil || len(got.Items) != 1 || got.Profile != nil {
		t.Fatalf("mine = %+v %v", got, err)
	}
}

func TestOfflineKeepsTheLastConfirmedProfile(t *testing.T) {
	st := &stub{mineBody: profileMine}
	svc := submitted(t, st)
	if _, err := svc.ListMine(context.Background()); err != nil {
		t.Fatal(err)
	}
	st.mineCode = http.StatusBadGateway
	off, err := svc.ListMine(context.Background())
	if err != nil || !off.Offline || off.Profile == nil || off.Profile.Level != 2 {
		t.Fatalf("offline = %+v %v", off, err)
	}
}

func TestARetiredIdentityTakesItsProfileWithIt(t *testing.T) {
	st := &stub{mineBody: profileMine}
	svc := submitted(t, st)
	if _, err := svc.ListMine(context.Background()); err != nil {
		t.Fatal(err)
	}
	st.mineCode, st.mineBody = 401, `{"error":{"code":"feedback.bad_token"}}`
	got, err := svc.ListMine(context.Background())
	if err != nil || got.Profile != nil {
		t.Fatalf("mine = %+v %v", got, err)
	}
}

func TestAnUnknownTrustStatePassesThrough(t *testing.T) {
	svc := submitted(t, &stub{mineBody: strings.Replace(profileMine, `"active"`, `"suspended"`, 1)})
	got, err := svc.ListMine(context.Background())
	if err != nil || got.Profile == nil || got.Profile.TrustState != TrustState("suspended") {
		t.Fatalf("mine = %+v %v", got, err)
	}
}

func TestProfileOfTheMaxLevelHasNoNext(t *testing.T) {
	body := `{"profile":{"level":6,"adoptedCount":50,"currentThreshold":48,"nextLevel":null,"nextThreshold":null,"remaining":null,"trustState":"lapsed","trustExpiresAt":null,"observedAt":"2026-10-07T08:00:00Z","effectiveLimits":{"reportsPerHour":3,"reportsPerDay":10,"repliesPerHour":3}},"items":[]}`
	svc := submitted(t, &stub{mineBody: body})
	got, err := svc.ListMine(context.Background())
	if err != nil || got.Profile == nil || got.Profile.NextLevel != nil || got.Profile.Remaining != nil || got.Profile.TrustExpiresAt != nil {
		t.Fatalf("mine = %+v %v", got, err)
	}
}
