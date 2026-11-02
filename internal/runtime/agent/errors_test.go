package agent

import (
	"errors"
	"fmt"
	"testing"
)

func TestPauseClassNamesEachGuard(t *testing.T) {
	for _, tc := range []struct {
		err  error
		want string
	}{
		{&maxStepsPause{steps: 40, key: "max_steps"}, "max_steps"},
		{fmt.Errorf("wrapped: %w", &maxStepsPause{steps: 5}), "max_steps"},
		{errors.New("provider 500"), ""},
		{nil, ""},
	} {
		if got := PauseClass(tc.err); got != tc.want {
			t.Errorf("PauseClass(%v) = %q, want %q", tc.err, got, tc.want)
		}
	}
}
