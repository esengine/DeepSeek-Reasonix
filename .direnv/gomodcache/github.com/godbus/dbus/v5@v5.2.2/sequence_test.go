package dbus

import (
	"testing"
)

func TestSequential(t *testing.T) {
	gen := newSequenceGenerator()

	for i := 1; i < 10; i++ {
		sequence := gen.next()
		if sequence != Sequence(i) {
			t.Errorf("Got %v expected %v", sequence, i)
		}
	}
}
