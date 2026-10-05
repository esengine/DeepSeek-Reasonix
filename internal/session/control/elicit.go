package control

import (
	"context"
	"strings"

	"reasonix/internal/contract/event"
	"reasonix/internal/contract/tool"
)

// Elicit implements tool.Elicitor through the same prompt the ask tool uses,
// marked with who is asking. Nothing given back is a refusal for the party,
// not a skip that ends the turn.
func (c *Controller) Elicit(ctx context.Context, req tool.ElicitRequest) (tool.ElicitReply, error) {
	if req.URL != "" {
		if c.observe != nil {
			return tool.ElicitReply{Declined: true, Action: "decline"}, nil
		}
		return c.elicitURL(ctx, req)
	}
	questions := make([]event.AskQuestion, len(req.Fields))
	for i, f := range req.Fields {
		q := event.AskQuestion{ID: f.Name, Header: f.Title, Prompt: f.Description, Reason: event.AskReasonMissingValue, Multi: f.Multi, Default: f.Default}
		if q.Header == "" {
			q.Header = f.Name
		}
		if q.Prompt == "" {
			q.Prompt = q.Header
		}
		for _, choice := range f.Choices {
			q.Options = append(q.Options, event.AskOption{Label: choice})
		}
		questions[i] = q
	}
	origin := &event.AskOrigin{Kind: event.AskOriginMCP, Source: req.Source, Message: req.Message, Note: req.Note}
	answers, err := c.ask(ctx, questions, origin)
	if err != nil {
		return tool.ElicitReply{}, err
	}
	if !askAnswersHaveSelection(answers) {
		return tool.ElicitReply{Declined: true}, nil
	}
	values := make(map[string][]string, len(answers))
	for _, a := range answers {
		values[a.QuestionID] = append([]string(nil), a.Selected...)
	}
	return tool.ElicitReply{Values: values}, nil
}

// formReceipt records that a party's form was answered, and never what was in
// it: the values were given to that party, and a receipt travels to every
// attached device, the session file and the trajectory.
func formReceipt(origin *event.AskOrigin, questions []event.AskQuestion, answers []event.AskAnswer) (subject, outcome string) {
	if origin.URL != "" {
		outcome := "declined"
		switch elicitURLAction(answers) {
		case "accept":
			outcome = "accepted"
		case "cancel":
			outcome = "cancelled"
		}
		return clipUTF8("external interaction from "+origin.Source, 240), outcome
	}
	names := make([]string, len(questions))
	for i, q := range questions {
		names[i] = q.ID
	}
	outcome = "declined"
	if askAnswersHaveSelection(answers) {
		outcome = "accepted"
	}
	return clipUTF8("form from "+origin.Source+": "+strings.Join(names, ", "), 240), outcome
}
