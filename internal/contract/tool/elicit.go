package tool

import "context"

// ElicitField is one value an external party asks the person to supply. With
// Choices it is picked from them (several when Multi); without, it is typed.
type ElicitField struct {
	Name        string
	Title       string
	Description string
	Choices     []string
	Multi       bool
	Default     []string // prefilled: the party's default, or what was given last time
}

// ElicitRequest asks for an external party's form or URL interaction. A nonempty
// URL selects an action-only interaction; otherwise Fields describes the form.
type ElicitRequest struct {
	Source  string
	Message string
	Note    string
	Fields  []ElicitField
	URL     string
}

// ElicitReply carries form values or a URL interaction's accept/decline/cancel
// Action. URL replies never carry Values.
type ElicitReply struct {
	Declined bool
	Values   map[string][]string
	Action   string
}

// Elicitor puts an external party's form in front of the person. A refusal is a
// reply, never an error: it goes back to the party, and the turn goes on. A nil
// elicitor (headless runs, sub-agents with no interactive parent) means nobody
// can be asked.
type Elicitor interface {
	Elicit(ctx context.Context, req ElicitRequest) (ElicitReply, error)
}

type elicitorContextKey struct{}

// WithElicitor stamps an elicitor onto a tool execution context.
func WithElicitor(ctx context.Context, e Elicitor) context.Context {
	if e == nil {
		return ctx
	}
	return context.WithValue(ctx, elicitorContextKey{}, e)
}

// ElicitorFrom returns the elicitor carried by ctx.
func ElicitorFrom(ctx context.Context) (Elicitor, bool) {
	if ctx == nil {
		return nil, false
	}
	e, ok := ctx.Value(elicitorContextKey{}).(Elicitor)
	return e, ok && e != nil
}
