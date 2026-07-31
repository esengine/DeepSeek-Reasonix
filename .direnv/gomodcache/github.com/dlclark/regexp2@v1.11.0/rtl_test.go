package regexp2

import "testing"

func TestRightToLeft_Basic(t *testing.T) {
	re := MustCompile(`foo\d+`, RightToLeft)
	s := "0123456789foo4567890foo1foo  0987"

	m, err := re.FindStringMatch(s)
	if err != nil {
		t.Fatalf("Unexpected err: %v", err)
	}
	if want, got := "foo1", m.String(); want != got {
		t.Fatalf("Match 0 failed, wanted %v, got %v", want, got)
	}

	m, err = re.FindNextMatch(m)
	if err != nil {
		t.Fatalf("Unexpected err: %v", err)
	}
	if want, got := "foo4567890", m.String(); want != got {
		t.Fatalf("Match 1 failed, wanted %v, got %v", want, got)
	}
}

func TestRightToLeft_StartAt(t *testing.T) {
	re := MustCompile(`\d`, RightToLeft)

	m, err := re.FindStringMatchStartingAt("0123", -1)
	if err != nil {
		t.Fatalf("Unexpected err: %v", err)
	}
	if m == nil {
		t.Fatalf("Expected match")
	}
	if want, got := "3", m.String(); want != got {
		t.Fatalf("Find failed, wanted '%v', got '%v'", want, got)
	}

}

func TestRightToLeft_Replace(t *testing.T) {
	re := MustCompile(`\d`, RightToLeft)
	s := "0123456789foo4567890foo         "
	str, err := re.Replace(s, "#", -1, 7)
	if err != nil {
		t.Fatalf("Unexpected err: %v", err)
	}
	if want, got := "0123456789foo#######foo         ", str; want != got {
		t.Fatalf("Replace failed, wanted '%v', got '%v'", want, got)
	}
}
