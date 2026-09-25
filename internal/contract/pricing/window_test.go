package pricing

import (
	"testing"
	"time"
)

// beijingAt builds an instant from the wall clock the vendor publishes its rule
// in, so a case reads as the hour a user in Beijing would see.
func beijingAt(year int, month time.Month, day, hour int) time.Time {
	return time.Date(year, month, day, hour, 0, 0, 0, time.FixedZone("", beijing))
}

func TestPeakWindowReadsTheVendorsClock(t *testing.T) {
	cases := []struct {
		name string
		at   time.Time
		peak bool
	}{
		// 2026-08-19 is a Wednesday; 08-22 a Saturday, 08-23 a Sunday.
		{"weekday morning inside 9-12", beijingAt(2026, 8, 19, 10), true},
		{"weekday lunch gap between the two ranges", beijingAt(2026, 8, 19, 13), false},
		{"weekday afternoon inside 14-18", beijingAt(2026, 8, 19, 15), true},
		{"weekday 18:00 is the exclusive end", beijingAt(2026, 8, 19, 18), false},
		{"weekday night", beijingAt(2026, 8, 19, 23), false},
		// The weekend rule starts 2026-08-23, so the Saturday before it still peaks.
		{"saturday before the weekend rule", beijingAt(2026, 8, 22, 10), true},
		{"sunday the weekend rule starts", beijingAt(2026, 8, 23, 10), false},
		{"saturday after the weekend rule", beijingAt(2026, 8, 29, 10), false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := deepseekPeak.IsPeak(c.at); got != c.peak {
				t.Fatalf("IsPeak(%s) = %v, want %v", c.at.Format(time.RFC3339), got, c.peak)
			}
		})
	}
}

// Public holidays bill off-peak even on weekdays (Mid-Autumn Fri 2026-09-25,
// Spring Festival Tue 2026-02-17, National Day Thu 2026-10-01), and a make-up
// workday (Sat 2026-10-10) is still a weekend.
func TestPeakWindowReadsTheHolidayTable(t *testing.T) {
	cases := []struct {
		name string
		at   time.Time
		peak bool
	}{
		{"mid-autumn friday morning window", beijingAt(2026, 9, 25, 10), false},
		{"mid-autumn friday afternoon window", beijingAt(2026, 9, 25, 15), false},
		{"thursday before mid-autumn", beijingAt(2026, 9, 24, 10), true},
		{"monday after mid-autumn", beijingAt(2026, 9, 28, 10), true},
		{"spring festival new year's day", beijingAt(2026, 2, 17, 9), false},
		{"friday before spring festival", beijingAt(2026, 2, 13, 10), true},
		{"national day morning window", beijingAt(2026, 10, 1, 10), false},
		{"first workday after national day", beijingAt(2026, 10, 8, 10), true},
		{"make-up workday saturday", beijingAt(2026, 10, 10, 10), false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := deepseekPeak.IsPeak(c.at); got != c.peak {
				t.Fatalf("IsPeak(%s) = %v, want %v", c.at.Format(time.RFC3339), got, c.peak)
			}
		})
	}
}

// The instant is what decides, not the clock of whoever reads it: the same
// moment expressed in UTC has to land on the same side of the window.
func TestPeakWindowIsAboutTheInstantNotTheReader(t *testing.T) {
	at := beijingAt(2026, 8, 19, 10)
	if !deepseekPeak.IsPeak(at.UTC()) {
		t.Fatal("the same instant in UTC read as off-peak")
	}
	if deepseekPeak.IsPeak(at.In(time.FixedZone("", -5*3600))) != true {
		t.Fatal("the same instant in a US zone read as off-peak")
	}
}

func TestNilWindowNeverPeaks(t *testing.T) {
	var none *PeakWindow
	if none.IsPeak(beijingAt(2026, 8, 19, 10)) {
		t.Fatal("a vendor with no schedule charged a peak rate")
	}
}

// A turn inside the peak window is billed at the peak card — this is the whole
// point of the schedule, and it has to survive all the way out of BuildQuote.
func TestBuildQuoteBillsThePeakRateInsideTheWindow(t *testing.T) {
	base := QuoteInput{
		Usage:        UsageTokens{PromptTokens: 1_000_000, CompletionTokens: 1_000_000},
		Rates:        RateCard{CacheHit: 0.15, Input: 4.5, Output: 13.5, Currency: "CNY"},
		ProviderKind: "deepseek",
		ModelID:      "deepseek-v4-pro",
		ModelRef:     "deepseek-v4-pro",
	}
	off := base
	off.OccurredAt = beijingAt(2026, 8, 19, 23)
	on := base
	on.OccurredAt = beijingAt(2026, 8, 19, 10)

	offQuote, onQuote := BuildQuote(off), BuildQuote(on)
	// 1M uncached input + 1M output: 4.5+13.5 off-peak, 9+27 at peak.
	if offQuote.Original.Amount != "18" {
		t.Fatalf("off-peak original = %q, want 18", offQuote.Original.Amount)
	}
	if onQuote.Original.Amount != "36" {
		t.Fatalf("peak original = %q, want 36", onQuote.Original.Amount)
	}
}

// A price the vendor did not publish is the user's own arrangement. Projecting
// someone else's peak schedule onto it would invent a number.
func TestCustomPriceIsNeverRescheduled(t *testing.T) {
	in := QuoteInput{
		Usage:        UsageTokens{PromptTokens: 1_000_000, CompletionTokens: 1_000_000},
		Rates:        RateCard{CacheHit: 0.15, Input: 1, Output: 1, Currency: "CNY"},
		ProviderKind: "deepseek",
		ModelID:      "deepseek-v4-pro",
		OccurredAt:   beijingAt(2026, 8, 19, 10),
	}
	if got := BuildQuote(in).Original.Amount; got != "2" {
		t.Fatalf("custom price original = %q, want 2", got)
	}
}
