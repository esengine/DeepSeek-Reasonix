package provider

import (
	"errors"
	"fmt"
	"testing"
)

func TestStreamFailureCausePreservesIdentity(t *testing.T) {
	for _, tc := range []struct {
		err  error
		want StreamFailureCause
	}{
		{ErrInvalidFunctionCall, StreamFailureInvalidFunctionCall},
		{ErrUnfinishedFunctionCall, StreamFailureUnfinishedFunctionCall},
		{errors.New(ErrInvalidFunctionCall.Error()), ""},
		{nil, ""},
	} {
		err := tc.err
		if err != nil {
			err = fmt.Errorf("caller: %w", err)
		}
		if got := StreamFailureCauseOf(err); got != tc.want {
			t.Errorf("cause=%q want=%q", got, tc.want)
		}
		if tc.want != "" && tc.want.Description() == "" {
			t.Error("known cause has no projection")
		}
	}
	if errors.Is(ErrInvalidFunctionCall, ErrUnfinishedFunctionCall) {
		t.Error("causes collapsed")
	}
	if StreamFailureCause("untrusted raw text").Description() != "" {
		t.Error("unknown cause projected raw text")
	}
}
