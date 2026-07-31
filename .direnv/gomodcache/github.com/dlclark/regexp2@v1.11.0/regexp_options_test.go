package regexp2

import "testing"

func TestIgnoreCase_Simple(t *testing.T) {
	r := MustCompile("aaamatch thisbbb", IgnoreCase)
	m, err := r.FindStringMatch("AaAMatch thisBBb")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if m == nil {
		t.Fatalf("no match when one was expected")
	}
	if want, got := "AaAMatch thisBBb", m.String(); want != got {
		t.Fatalf("group 0 wanted '%v', got '%v'", want, got)
	}
}

func TestIgnoreCase_Inline(t *testing.T) {
	r := MustCompile("aaa(?i:match this)bbb", 0)
	m, err := r.FindStringMatch("aaaMaTcH ThIsbbb")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if m == nil {
		t.Fatalf("no match when one was expected")
	}
	if want, got := "aaaMaTcH ThIsbbb", m.String(); want != got {
		t.Fatalf("group 0 wanted '%v', got '%v'", want, got)
	}
}

func TestIgnoreCase_Revert(t *testing.T) {

	r := MustCompile("aaa(?-i:match this)bbb", IgnoreCase)
	m, err := r.FindStringMatch("AaAMatch thisBBb")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if m != nil {
		t.Fatalf("had a match but expected no match")
	}
}
