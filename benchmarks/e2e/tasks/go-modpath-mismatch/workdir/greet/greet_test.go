package greet

import "testing"

func TestHello(t *testing.T) {
	if got := Hello("x"); got != "hello, x" {
		t.Fatalf("got %q", got)
	}
}
