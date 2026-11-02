package cli

import (
	"context"
	"errors"
	"fmt"
	"testing"
)

func TestClassifyRunCompletionNamesTheFailureClass(t *testing.T) {
	for _, tc := range []struct {
		err  error
		want string
	}{
		{nil, "success"},
		{fmt.Errorf("run: %w", context.DeadlineExceeded), "timeout"},
		{context.Canceled, "cancelled"},
		{errors.New("provider 500"), "error_during_execution"},
	} {
		if got := classifyRunCompletion(tc.err).class; got != tc.want {
			t.Errorf("class for %v = %q, want %q", tc.err, got, tc.want)
		}
	}
}
