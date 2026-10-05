package event

import "encoding/json"

// ContextBudgetFigures is the Detail of a NoticeCodeContextBudget notice: the
// numbers a frontend places into its own sentence.
type ContextBudgetFigures struct {
	Percent   int `json:"percent"`
	Remaining int `json:"remaining"`
}

func (f ContextBudgetFigures) Encode() string {
	b, _ := json.Marshal(f)
	return string(b)
}

// DetailIsPayload reports that a coded notice's Detail is the typed payload its
// sentence is worded from, so a sink that prints Text must not append it again.
func DetailIsPayload(code string) bool {
	return code == NoticeCodeContextBudget || code == NoticeCodeUnappliedSteer
}

// DecodeContextBudgetFigures reads Detail back; ok is false when it is not figures.
func DecodeContextBudgetFigures(detail string) (f ContextBudgetFigures, ok bool) {
	return f, json.Unmarshal([]byte(detail), &f) == nil
}
