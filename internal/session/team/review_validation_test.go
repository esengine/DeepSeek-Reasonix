package team

import (
	"errors"
	"testing"
)

func TestInvalidReviewBatchPublishesNothing(t *testing.T) {
	for name, reply := range map[string]string{
		"invalid JSON":  `{"changes":[`,
		"unknown entry": `{"changes":[{"op":"add","description":"valid","body":"valid"},{"op":"edit","entry":99,"body":"unknown"}]}`,
		"credential":    `{"changes":[{"op":"add","description":"valid","body":"valid"},{"op":"add","description":"credential","body":"api_key=sk-Abc1234567890abcdefghijklmnop"}]}`,
	} {
		t.Run(name, func(t *testing.T) {
			s := reviewStore(t)
			a := &askRecorder{reply: reply}
			_, err := ReviewMemory(t.Context(), s, []ReviewLine{{User: true, Text: "Remember this preference"}}, a.ask)
			if !errors.Is(err, ErrReviewReply) {
				t.Fatalf("invalid batch = %v", err)
			}
			facts, err := s.List()
			if err != nil || len(facts) != 0 {
				t.Fatalf("invalid batch changed memory: %+v, %v", facts, err)
			}
		})
	}
}
