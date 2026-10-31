package secrets

import (
	"bytes"
	"strings"
	"testing"
)

func TestAStreamMasksAValueSplitAcrossWrites(t *testing.T) {
	watch("STREAM_KEY", "sk-stream-0123456789")
	var out bytes.Buffer
	s := NewStream(&out)
	for _, piece := range []string{"token=sk-st", "ream-01234", "56789 done\n"} {
		if _, err := s.Write([]byte(piece)); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.Flush(); err != nil {
		t.Fatal(err)
	}
	if got := out.String(); got != "token=[secret:STREAM_KEY] done\n" {
		t.Fatalf("stream = %q", got)
	}
}

// Text that cannot begin a value passes on at once: a job waiting for a line
// must see it without the stream ending.
func TestAStreamPassesOnWhatCannotBeginAValue(t *testing.T) {
	watch("STREAM_KEY2", "sk-other-0123456789")
	var out bytes.Buffer
	s := NewStream(&out)
	_, _ = s.Write([]byte("server ready\n"))
	if out.String() != "server ready\n" {
		t.Fatalf("held back %q", out.String())
	}
	_, _ = s.Write([]byte("next sk-oth"))
	if out.String() != "server ready\nnext " {
		t.Fatalf("a possible value's start was passed on: %q", out.String())
	}
	_ = s.Flush()
	if !strings.HasSuffix(out.String(), "next sk-oth") {
		t.Fatalf("flush lost the held fragment: %q", out.String())
	}
}

func TestRunningStreamMasksNewlySavedValuesImmediately(t *testing.T) {
	var current []Labeled
	WatchValues(t.Name(), func() []Labeled { return current })
	t.Cleanup(func() { WatchValues(t.Name(), func() []Labeled { return nil }) })
	var out bytes.Buffer
	s := NewStream(&out)
	_, _ = s.Write([]byte("ready\n"))
	current = []Labeled{{Label: "STREAM_NEW", Value: "newly-saved-stream-value"}}
	_, _ = s.Write([]byte("newly-saved-stream-value\n"))
	_ = s.Flush()
	if got := out.String(); got != "ready\n[secret:STREAM_NEW]\n" {
		t.Fatalf("stream leaked a newly saved value: %q", got)
	}
}
