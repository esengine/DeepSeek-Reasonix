package farewell

import "testing"

func TestBye(t *testing.T) {
	if got := Bye("x"); got != "bye, x" {
		t.Fatalf("got %q", got)
	}
}
