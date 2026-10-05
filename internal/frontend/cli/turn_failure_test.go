package cli

import (
	"context"
	"errors"
	"fmt"
	"testing"
)

func TestTurnFailureWorthShowing(t *testing.T) {
	for name, tc := range map[string]struct {
		err  error
		want bool
	}{
		"nothing went wrong":     {nil, false},
		"an empty message":       {errors.New(""), false},
		"the user stopped it":    {context.Canceled, false},
		"stopped, wrapped twice": {fmt.Errorf("run: %w", fmt.Errorf("turn: %w", context.Canceled)), false},
		"a real failure":         {errors.New("disk went away"), true},
		"a deadline, not a stop": {context.DeadlineExceeded, true},
	} {
		if got := turnFailureWorthShowing(tc.err); got != tc.want {
			t.Errorf("%s: = %v, want %v", name, got, tc.want)
		}
	}
}
