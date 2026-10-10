package computer

import (
	"slices"
	"strings"

	"reasonix/internal/base/textutil"
)

// EffectClass is how far a step that reached the application is known to
// have worked. A step that did not reach it is a Failure, never a class.
type EffectClass string

const (
	EffectConfirmed     EffectClass = "confirmed"
	EffectUnverifiable  EffectClass = "unverifiable"
	EffectSuspectedNoop EffectClass = "suspected_noop"
)

// Evidence is the kind of fact the helper read that a class rests on.
type Evidence string

const (
	EvidenceValueReadback  Evidence = "value_readback"
	EvidenceValueUnchanged Evidence = "value_unchanged"
)

// classEvidence is what each class other than unverifiable must carry. An
// accepted call or a changed screen proves nothing about the target.
var classEvidence = map[EffectClass]Evidence{
	EffectConfirmed:     EvidenceValueReadback,
	EffectSuspectedNoop: EvidenceValueUnchanged,
}

// Effect is what is known of a step that went through. Class is empty for a
// step that sends the application nothing, such as a wait.
type Effect struct {
	Class    EffectClass
	Evidence []Evidence
	// BlockedBy is the modal the input went to instead of the window behind it.
	BlockedBy *Modal
}

// Modal is a window that holds an application's input until it is answered.
// Title and Blocks are names shown to the model, never read for a decision.
type Modal struct {
	Ref    string `json:"ref"`
	Title  string `json:"title"`
	Blocks string `json:"blocks"`
}

// modalNameLimit is the length the helpers clip every other label in a tree
// to; a modal's names are the application's to choose, so they get no more.
var modalNameLimit = textutil.PreviewLimit{Graphemes: 160, Lines: 1}

// shownName quotes an application-chosen name bounded, with every control and
// invisible character, and the quote itself, rendered as a visible escape.
func shownName(s string) string {
	bounded, _ := textutil.BoundLiteral(s, modalNameLimit)
	return `"` + strings.ReplaceAll(bounded, `"`, `\u{22}`) + `"`
}

func (m Modal) String() string {
	title := m.Title
	if title == "" {
		title = "(untitled)"
	}
	s := "the modal " + shownName(title)
	if m.Ref != "" {
		s += " [" + m.Ref + "]"
	}
	if m.Blocks != "" {
		s += " over " + shownName(m.Blocks)
	}
	return s
}

type wireEffect struct {
	Class     string   `json:"class"`
	Evidence  []string `json:"evidence"`
	BlockedBy *Modal   `json:"blocked_by"`
}

// settle grants the helper's class only when its evidence is there; anything
// else, an older helper's silence included, is unverifiable.
func (w *wireEffect) settle() Effect {
	e := Effect{Class: EffectUnverifiable}
	if w == nil {
		return e
	}
	e.BlockedBy = w.BlockedBy
	class := EffectClass(w.Class)
	if need, ok := classEvidence[class]; ok && slices.Contains(w.Evidence, string(need)) {
		e.Class, e.Evidence = class, []Evidence{need}
	}
	return e
}
