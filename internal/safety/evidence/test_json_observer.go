package evidence

import (
	"bytes"
	"encoding/json"
	"sync"
)

// TestJSONObserver consumes the complete process stream independently of display
// truncation. Only a criterion's own terminal event decides its result.
type TestJSONObserver struct {
	mu       sync.Mutex
	criteria map[string]string
	pending  []byte
	broken   bool
}

func NewTestJSONObserver(criteria []string) *TestJSONObserver {
	o := &TestJSONObserver{criteria: make(map[string]string, len(criteria))}
	for _, name := range criteria {
		o.criteria[name] = ""
	}
	return o
}

func (o *TestJSONObserver) Write(body []byte) (int, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	count := len(body)
	for len(body) != 0 && !o.broken {
		line, remaining, complete := bytes.Cut(body, []byte{'\n'})
		if len(o.pending)+len(line) > 1<<20 {
			o.broken, o.pending = true, nil
			break
		}
		o.pending = append(o.pending, line...)
		if !complete {
			break
		}
		o.observe(o.pending)
		o.pending = o.pending[:0]
		body = remaining
	}
	return count, nil
}

func (o *TestJSONObserver) observe(line []byte) {
	var event struct {
		Action string `json:"Action"`
		Test   string `json:"Test"`
	}
	if json.Unmarshal(line, &event) != nil {
		return
	}
	previous, captured := o.criteria[event.Test]
	if !captured || previous == "fail" {
		return
	}
	switch event.Action {
	case "pass", "fail", "skip":
		o.criteria[event.Test] = event.Action
	}
}

func (o *TestJSONObserver) Outcome() CriterionResult {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.broken || len(o.criteria) == 0 || len(o.pending) != 0 {
		return CriterionUnavailable
	}
	// An unterminated final record is not evidence of a complete event.
	for _, action := range o.criteria {
		if action == "fail" {
			return CriterionFail
		}
	}
	for _, action := range o.criteria {
		if action != "pass" {
			return CriterionUnavailable
		}
	}
	return CriterionPass
}
