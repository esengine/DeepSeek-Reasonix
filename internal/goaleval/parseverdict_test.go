package goaleval

import (
	"strings"
	"testing"
	"unicode/utf8"
)

// TestParseVerdict covers extracting the {…} JSON object from a model
// response (tolerating code fences and prose wrappers) and validating the
// outcome enum. Every error is fail-closed: the host pauses the goal.
func TestParseVerdict(t *testing.T) {
	for _, tc := range []struct {
		name    string
		text    string
		want    Verdict
		wantErr string // substring of the expected error; "" means success
	}{
		{name: "empty", text: "", wantErr: "empty goal evaluator response"},
		{name: "whitespace only", text: "  \n\t ", wantErr: "empty goal evaluator response"},
		{name: "pure json", text: `{"outcome":"complete","reason":"the goal is done"}`, want: Verdict{Outcome: OutcomeComplete, Reason: "the goal is done"}},
		{name: "fenced json", text: "```json\n{\"outcome\":\"continue\",\"reason\":\"more work\"}\n```", want: Verdict{Outcome: OutcomeContinue, Reason: "more work"}},
		{name: "prose wrapped", text: "Here is my judgment: {\"outcome\":\"blocked\",\"reason\":\"needs user input\"}", want: Verdict{Outcome: OutcomeBlocked, Reason: "needs user input"}},
		{name: "prose before and after", text: "Sure: {\"outcome\":\"uncertain\",\"reason\":\"cannot judge\"} done.", want: Verdict{Outcome: OutcomeUncertain, Reason: "cannot judge"}},
		{name: "malformed json", text: "{not json", wantErr: "invalid goal evaluator JSON"},
		{name: "no json object", text: "no json here", wantErr: "invalid goal evaluator JSON"},
		{name: "missing outcome", text: `{"reason":"x"}`, wantErr: "invalid outcome"},
		{name: "invalid outcome", text: `{"outcome":"maybe","reason":"x"}`, wantErr: "invalid outcome"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseVerdict(tc.text)
			if tc.wantErr != "" {
				if err == nil {
					t.Fatalf("parseVerdict(%q) error = nil, want error containing %q", tc.text, tc.wantErr)
				}
				if !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("parseVerdict(%q) error = %v, want containing %q", tc.text, err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseVerdict(%q) error = %v", tc.text, err)
			}
			if got != tc.want {
				t.Errorf("parseVerdict(%q) = %+v, want %+v", tc.text, got, tc.want)
			}
		})
	}
}

func TestParseVerdictClipsReason(t *testing.T) {
	long := strings.Repeat("x", MaxReasonBytes+200)
	got, err := parseVerdict(`{"outcome":"complete","reason":"` + long + `"}`)
	if err != nil {
		t.Fatalf("parseVerdict() error = %v", err)
	}
	if len(got.Reason) != MaxReasonBytes {
		t.Errorf("reason length = %d, want %d", len(got.Reason), MaxReasonBytes)
	}
	if got.Reason != strings.Repeat("x", MaxReasonBytes) {
		t.Errorf("reason = prefix mismatch, want %d x's", MaxReasonBytes)
	}
}

func TestParseVerdictClipsReasonAtRuneBoundary(t *testing.T) {
	reason := strings.Repeat("é", MaxReasonBytes) // 2 bytes per rune → 1000 bytes
	got, err := parseVerdict(`{"outcome":"continue","reason":"` + reason + `"}`)
	if err != nil {
		t.Fatalf("parseVerdict() error = %v", err)
	}
	if len(got.Reason) > MaxReasonBytes {
		t.Errorf("reason length = %d, want <= %d", len(got.Reason), MaxReasonBytes)
	}
	if !utf8.ValidString(got.Reason) {
		t.Errorf("clipped reason is not valid UTF-8: %q", got.Reason)
	}
	if got.Reason != strings.Repeat("é", MaxReasonBytes/2) {
		t.Errorf("clipped reason = %d runes, want %d", utf8.RuneCountInString(got.Reason), MaxReasonBytes/2)
	}
}

func TestClip(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   string
		max  int
		want string
	}{
		{name: "empty", in: "", max: 10, want: ""},
		{name: "under budget", in: "hello", max: 10, want: "hello"},
		{name: "exact fit", in: "hello", max: 5, want: "hello"},
		{name: "truncates ascii", in: "hello world", max: 5, want: "hello"},
		{name: "zero budget", in: "hello", max: 0, want: ""},
		{name: "cut lands after multi-byte rune", in: "héllo", max: 3, want: "hé"},
		{name: "cut splits multi-byte rune", in: "héllo", max: 2, want: "h"},
		{name: "all multi-byte", in: "日本語", max: 5, want: "日"},
		{name: "all multi-byte exact", in: "日本語", max: 6, want: "日本"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := clip(tc.in, tc.max); got != tc.want {
				t.Errorf("clip(%q, %d) = %q, want %q", tc.in, tc.max, got, tc.want)
			}
		})
	}
}

func TestUTF8RuneStart(t *testing.T) {
	for _, tc := range []struct {
		b    byte
		want bool
	}{
		{b: 'a', want: true},
		{b: 0xC3, want: true}, // 2-byte lead byte
		{b: 0xE2, want: true}, // 3-byte lead byte
		{b: 0xF0, want: true}, // 4-byte lead byte
		{b: 0x80, want: false},
		{b: 0xA9, want: false},
		{b: 0xBF, want: false},
	} {
		if got := utf8RuneStart(tc.b); got != tc.want {
			t.Errorf("utf8RuneStart(%#x) = %v, want %v", tc.b, got, tc.want)
		}
	}
}
