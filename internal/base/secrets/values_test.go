package secrets

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"
)

func watch(label, value string) {
	WatchValues("test:"+label+"\x00"+value, func() []Labeled { return []Labeled{{label, value}} })
}

func masked(s string) string { return Masker().String(s) }

func TestARegisteredValueIsMaskedInEveryFormItTravelsIn(t *testing.T) {
	watch("SHOP_KEY", `k3y"with/odd chars`)
	for _, in := range []string{
		`export SHOP_KEY=k3y"with/odd chars`,
		`{"cmd":"curl -H 'X: k3y\"with/odd chars'"}`,
		`https://shop.example/?key=k3y%22with%2Fodd+chars`,
	} {
		got := masked(in)
		if strings.Contains(got, "odd chars") || strings.Contains(got, "odd+chars") || !strings.Contains(got, "[secret:SHOP_KEY]") {
			t.Errorf("masked(%q) = %q", in, got)
		}
	}
}

func TestAShortValueIsMaskedOnlyWhereItStandsAlone(t *testing.T) {
	watch("PIN", "4821")
	if got := masked("the pin is 4821."); got != "the pin is [secret:PIN]." {
		t.Fatalf("standalone = %q", got)
	}
	if got := masked("order 948217 shipped"); got != "order 948217 shipped" {
		t.Fatalf("inside a longer run = %q", got)
	}
	for in, want := range map[string]string{
		"4821\n4821\n":  "[secret:PIN]\n[secret:PIN]\n",
		"pin=4821,4821": "pin=[secret:PIN],[secret:PIN]",
		"4821 x4821":    "[secret:PIN] x4821",
	} {
		if got := masked(in); got != want {
			t.Errorf("neighbouring copies %q = %q, want %q", in, got, want)
		}
	}
}

func TestTheLongerOfTwoValuesIsMaskedFirst(t *testing.T) {
	watch("SHORT", "abcdefgh")
	watch("LONG", "abcdefgh-ijklmnop")
	if got := masked("token abcdefgh-ijklmnop"); got != "token [secret:LONG]" {
		t.Fatalf("overlapping values = %q", got)
	}
}

// A store answers for its values on every snapshot, so a value saved past
// every API is masked at once, and one it no longer answers for stays masked:
// copies of it may already sit in any transcript.
func TestAWatchedStoreIsReadOnEverySnapshotAndNothingIsForgotten(t *testing.T) {
	var stored []Labeled
	WatchValues(t.Name(), func() []Labeled { return stored })
	if got := masked("w4tch-value-001"); got != "w4tch-value-001" {
		t.Fatalf("before it was stored = %q", got)
	}
	stored = []Labeled{{"WATCHED", "w4tch-value-001"}}
	if got := masked("w4tch-value-001"); got != "[secret:WATCHED]" {
		t.Fatalf("stored = %q", got)
	}
	stored = nil
	if got := masked("w4tch-value-001"); got != "[secret:WATCHED]" {
		t.Fatalf("after the store dropped it = %q", got)
	}
}

func TestOneValueUnderTwoNamesStaysMaskedWhenEitherGoes(t *testing.T) {
	stored := []Labeled{{"GH_TOKEN", "ghp_twonames0001"}, {"GITHUB_TOKEN", "ghp_twonames0001"}}
	WatchValues(t.Name(), func() []Labeled { return stored })
	if got := masked("token=ghp_twonames0001"); got != "token=[secret:GH_TOKEN,GITHUB_TOKEN]" {
		t.Fatalf("two names = %q", got)
	}
	if got := Masker().Marks("token=[secret:GH_TOKEN,GITHUB_TOKEN]"); !slices.Equal(got, []string{"GH_TOKEN", "GITHUB_TOKEN"}) {
		t.Fatalf("marks of a value under two names = %v", got)
	}
	stored = stored[1:]
	if got := masked("token=ghp_twonames0001"); strings.Contains(got, "ghp_") {
		t.Fatalf("after one name went = %q", got)
	}
}

func TestJSONStaysTheDocumentItWas(t *testing.T) {
	watch("QUOTED", `j"son\value-01`)
	raw := json.RawMessage(`{"type":"message","content":[{"text":"use j\"son\\value-01 and \\\\\"x"}],"n":12345678901234567890}`)
	got := Masker().JSON(raw)
	if !json.Valid(got) || strings.Contains(string(got), "value-01") || !strings.Contains(string(got), "[secret:QUOTED]") || !strings.Contains(string(got), "12345678901234567890") {
		t.Fatalf("JSON = %s", got)
	}
	plain := json.RawMessage(`{"a": 1}`)
	if got := Masker().JSON(plain); string(got) != string(plain) {
		t.Fatalf("a document holding no value was rewritten: %s", got)
	}
}

func TestMarksFindsWhatAModelWroteBackFromAMaskedCopy(t *testing.T) {
	watch("ENV_KEY", "env-key-value-01")
	m := Masker()
	if got := m.Marks("KEY=[secret:ENV_KEY]\nOTHER=[secret:NOBODY]"); !slices.Equal(got, []string{"ENV_KEY"}) {
		t.Fatalf("marks = %v", got)
	}
	if got := m.Marks("KEY=env-key-value-01"); got != nil {
		t.Fatalf("marks in plain text = %v", got)
	}
	if got := (*Mask)(nil).String("x"); got != "x" {
		t.Fatalf("nil mask = %q", got)
	}
}

func TestNothingRegisteredIsLeftAlone(t *testing.T) {
	if got := masked("nothing here"); got != "nothing here" {
		t.Fatalf("no values = %q", got)
	}
}
