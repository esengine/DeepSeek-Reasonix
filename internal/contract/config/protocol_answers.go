package config

import (
	"fmt"
	"strings"
)

// Answering reports whether a kind produces want. Every list that offers a
// model for a job, and every place that accepts one, asks this and nothing else.
func Answering(kind string, want Answers) bool {
	return AnswersFor(kind) == want
}

// AnswersMismatchError says a model was put to a job its wire does not do. It
// carries both sides so a frontend words the refusal without reading the text.
type AnswersMismatchError struct {
	Ref  string
	Has  Answers
	Want Answers
}

func (e *AnswersMismatchError) Error() string {
	return fmt.Sprintf("model %q answers %s, not %s", e.Ref, e.Has, e.Want)
}

// RequireAnswers refuses a resolvable ref whose wire does not produce want. A
// ref that resolves to nothing passes: that is a different refusal, owned by
// whoever resolves it.
func (c *Config) RequireAnswers(ref string, want Answers) error {
	entry, ok := c.ResolveModel(strings.TrimSpace(ref))
	if !ok || Answering(entry.Kind, want) {
		return nil
	}
	return &AnswersMismatchError{Ref: entry.Name + "/" + entry.Model, Has: AnswersFor(entry.Kind), Want: want}
}
