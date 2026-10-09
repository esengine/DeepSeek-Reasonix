package control

import (
	"strconv"
	"strings"
	"testing"
	"time"

	"reasonix/internal/base/i18n"
)

const rigItem = `{"receipt":"FB-7K3M-9QX2","category":"bug","titleSnippet":"x","status":"fixed","resolvedVersion":"v2.25.0","createdAt":"2026-09-30T08:00:00Z","updatedAt":"2026-10-02T08:00:00Z"}`

func profileJSON(level, adopted, threshold int, next, trust string, limits string) string {
	return `{"profile":{"level":` + itoa(level) + `,"adoptedCount":` + itoa(adopted) + `,"currentThreshold":` + itoa(threshold) + next +
		`,"trustState":"` + trust + `","trustExpiresAt":"2026-11-01T00:00:00Z","observedAt":"2026-10-07T08:00:00Z","effectiveLimits":` + limits + `},"items":[` + rigItem + `]}`
}

func itoa(n int) string { return strconv.Itoa(n) }

const midNext = `,"nextLevel":3,"nextThreshold":6,"remaining":2`
const midLimits = `{"reportsPerHour":6,"reportsPerDay":20,"repliesPerHour":5}`

func useLanguage(t *testing.T, m i18n.Messages) {
	t.Helper()
	was := i18n.M
	i18n.M = m
	t.Cleanup(func() { i18n.M = was })
}

func TestFeedbackListStatesTheLevelProgressAndLimits(t *testing.T) {
	useLanguage(t, i18n.English)
	r := sentRig(t)
	r.edit(func(r *feedbackRig) { r.mine = profileJSON(2, 4, 3, midNext, "active", midLimits) })
	r.c.Submit("/feedback list")
	got := r.last(t, "Seedling")
	for _, want := range []string{"Level 2, Seedling: 4 shipped, 2 more to reach Sapling.", "6 reports an hour, 20 a day, 5 replies an hour", "FB-7K3M-9QX2"} {
		if !strings.Contains(got, want) {
			t.Errorf("list lacks %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "lapsed") || strings.Contains(got, "stay until") {
		t.Errorf("an active install is told about trust it does not need:\n%s", got)
	}
}

func TestFeedbackListSpeaksTheSessionLanguage(t *testing.T) {
	useLanguage(t, i18n.Chinese)
	r := sentRig(t)
	r.edit(func(r *feedbackRig) { r.mine = profileJSON(2, 4, 3, midNext, "active", midLimits) })
	r.c.Submit("/feedback list")
	got := r.last(t, "幼苗")
	if !strings.Contains(got, "小树") || strings.Contains(got, "Seedling") {
		t.Errorf("list = %s", got)
	}
}

func TestFeedbackListAtTheTopLevelHasNothingLeftToReach(t *testing.T) {
	useLanguage(t, i18n.English)
	r := sentRig(t)
	top := `{"reportsPerHour":12,"reportsPerDay":60,"repliesPerHour":10}`
	r.edit(func(r *feedbackRig) {
		r.mine = profileJSON(6, 50, 48, `,"nextLevel":null,"nextThreshold":null,"remaining":null`, "active", top)
	})
	r.c.Submit("/feedback list")
	if got := r.last(t, "Grove"); !strings.Contains(got, "50 shipped, the highest level.") || strings.Contains(got, "more to reach") {
		t.Fatalf("list = %s", got)
	}
}

func TestFeedbackListNamesWhyLimitsAreNotTheLevelsOwn(t *testing.T) {
	useLanguage(t, i18n.English)
	r := sentRig(t)
	legacy := `{"reportsPerHour":12,"reportsPerDay":60,"repliesPerHour":10}`
	r.edit(func(r *feedbackRig) {
		r.mine = profileJSON(0, 0, 0, `,"nextLevel":1,"nextThreshold":1,"remaining":1`, "legacy_active", legacy)
	})
	r.c.Submit("/feedback list")
	want := "stay until " + time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC).Local().Format("2006-01-02")
	if got := r.last(t, "New seed"); !strings.Contains(got, want) || !strings.Contains(got, "12 reports an hour") {
		t.Fatalf("want %q in:\n%s", want, got)
	}
	r.edit(func(r *feedbackRig) {
		r.mine = profileJSON(3, 7, 6, `,"nextLevel":4,"nextThreshold":12,"remaining":5`, "lapsed", `{"reportsPerHour":3,"reportsPerDay":10,"repliesPerHour":3}`)
	})
	r.c.Submit("/feedback list")
	if got := r.last(t, "Sapling"); !strings.Contains(got, "have lapsed") || !strings.Contains(got, "3 reports an hour") {
		t.Fatalf("lapsed:\n%s", got)
	}
}

func TestFeedbackListWithoutAProfileSaysNothingOfLevels(t *testing.T) {
	useLanguage(t, i18n.English)
	r := sentRig(t)
	r.c.Submit("/feedback list")
	if got := r.last(t, "FB-7K3M-9QX2"); strings.Contains(got, "Level") || strings.Contains(got, "shipped") {
		t.Fatalf("list = %s", got)
	}
}

func TestFeedbackListOfflineLabelsTheStandingAsLastConfirmed(t *testing.T) {
	useLanguage(t, i18n.English)
	r := sentRig(t)
	r.edit(func(r *feedbackRig) { r.mine = profileJSON(2, 4, 3, midNext, "active", midLimits) })
	r.c.Submit("/feedback list")
	r.last(t, "Seedling")
	r.mu.Lock()
	r.texts = nil
	r.mu.Unlock()
	r.edit(func(r *feedbackRig) { r.status = 502 })
	r.c.Submit("/feedback list")
	got := r.last(t, "offline")
	if !strings.Contains(got, "Seedling") || !strings.Contains(got, "last confirmed") {
		t.Fatalf("offline list = %s", got)
	}
}

func TestFeedbackRefusalNamesTheWindowFromItsIdentity(t *testing.T) {
	resets := time.Date(2026, 10, 8, 0, 0, 0, 0, time.UTC)
	at := resets.Local().Format("2006-01-02 15:04")
	cases := []struct {
		name, lang string
		m          i18n.Messages
		line       string
		params     string
		code       string
		says       []string
		not        []string
	}{
		{"daily", "en", i18n.English, "/feedback bug --yes again", `{"limit":"install_daily","resetsAt":"2026-10-08T00:00:00Z","retryAfterSeconds":3600}`, "feedback.rate_limited",
			[]string{"the daily report limit was reached", "resets at " + at}, []string{"too many submissions"}},
		{"daily zh", "zh", i18n.Chinese, "/feedback bug --yes again", `{"limit":"install_daily","resetsAt":"2026-10-08T00:00:00Z","retryAfterSeconds":3600}`, "feedback.rate_limited",
			[]string{"每日反馈次数上限", at}, nil},
		{"reply hourly", "en", i18n.English, "/feedback reply FB-7K3M-9QX2 --yes hi", `{"limit":"reply_hourly","resetsAt":"2026-10-07T13:00:00Z","retryAfterSeconds":60}`, "feedback.rate_limited",
			[]string{"the hourly reply limit was reached"}, nil},
		{"item cap", "en", i18n.English, "/feedback reply FB-7K3M-9QX2 --yes hi", `{"limit":"reply_item","resetsAt":null,"retryAfterSeconds":null}`, "feedback.reply_limit",
			[]string{"the reply limit for this report was reached", "does not reset"}, []string{"try again later"}},
		{"global", "en", i18n.English, "/feedback bug --yes again", `{"limit":"global_daily","resetsAt":"2026-10-08T00:00:00Z","retryAfterSeconds":900}`, "feedback.busy",
			[]string{"the service's daily capacity was reached", "resets at " + at}, nil},
		{"unknown window", "en", i18n.English, "/feedback bug --yes again", `{"limit":"something_new","resetsAt":null,"retryAfterSeconds":null}`, "feedback.rate_limited",
			[]string{"a limit was reached", "Try again later"}, nil},
	}
	for _, c := range cases {
		useLanguage(t, c.m)
		r := sentRig(t)
		r.c.Submit("/feedback list")
		r.last(t, "FB-7K3M-9QX2")
		status := 429
		if c.code == "feedback.busy" {
			status = 503
		}
		r.edit(func(r *feedbackRig) {
			r.status = status
			r.respond = `{"error":{"code":"` + c.code + `","message":"limit hit: install_hourly","params":` + c.params + `}}`
		})
		r.c.Submit(c.line)
		got := r.last(t, c.says[0])
		for _, w := range c.says {
			if !strings.Contains(got, w) {
				t.Errorf("%s: %q lacks %q", c.name, got, w)
			}
		}
		for _, w := range c.not {
			if strings.Contains(got, w) {
				t.Errorf("%s: %q should not say %q", c.name, got, w)
			}
		}
	}
}

func TestFeedbackRefusalFromAnOlderServiceKeepsItsPlainWords(t *testing.T) {
	useLanguage(t, i18n.English)
	r := sentRig(t)
	r.edit(func(r *feedbackRig) {
		r.status = 429
		r.respond = `{"error":{"code":"feedback.rate_limited"}}`
	})
	r.c.Submit("/feedback bug --yes again")
	r.last(t, "too many submissions, try again later")
}
