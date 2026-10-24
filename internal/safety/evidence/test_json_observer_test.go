package evidence

import (
	"strings"
	"testing"
)

func TestCapturedJSONRequiresParentTerminalEvent(t *testing.T) {
	for _, stream := range []string{
		"{\"Action\":\"pass\",\"Test\":\"TestEvict/child\"}\n",
		"{\"Action\":\"pass\",\"Test\":\"TestEvict\"}",
	} {
		observer := NewTestJSONObserver([]string{"TestEvict"})
		_, _ = observer.Write([]byte(stream))
		if got := observer.Outcome(); got != CriterionUnavailable {
			t.Fatalf("incomplete parent verdict: %s", got)
		}
	}
}

func TestCapturedJSONRetainsFailureBeyondDisplayBudget(t *testing.T) {
	o := NewTestJSONObserver([]string{"TestEvict"})
	write := func(value string) {
		t.Helper()
		if _, err := o.Write([]byte(value)); err != nil {
			t.Fatal(err)
		}
	}
	write("{\"Action\":\"pass\",\"Test\":\"TestEvict/child\"}\n")
	noise := "{\"Action\":\"output\",\"Test\":\"Other\",\"Output\":\"" + strings.Repeat("x", 8192) + "\"}\n"
	for range 1500 {
		write(noise)
	}
	write("{\"Action\":\"fail\",\"Test\":\"TestEvict\"}\n")
	for range 1500 {
		write(noise)
	}
	write("{\"Action\":\"pass\",\"Test\":\"Other\"}\n")
	if got := o.Outcome(); got != CriterionFail {
		t.Fatalf("lost middle failure: %s", got)
	}
	if cap(o.pending) > 1<<20 {
		t.Fatal("unbounded output retention")
	}
}

func TestCapturedJSONOversizedAndPartialRecordsCannotPass(t *testing.T) {
	for _, tail := range []string{strings.Repeat("x", (1<<20)+1), "{"} {
		o := NewTestJSONObserver([]string{"TestEvict"})
		for _, body := range []string{"{\"Action\":\"pass\",\"Test\":\"TestEvict\"}\n", tail} {
			for start := 0; start < len(body); start += 7 {
				_, _ = o.Write([]byte(body[start:min(start+7, len(body))]))
			}
		}
		if got := o.Outcome(); got != CriterionUnavailable {
			t.Fatalf("broken stream passed: %s", got)
		}
	}
}
