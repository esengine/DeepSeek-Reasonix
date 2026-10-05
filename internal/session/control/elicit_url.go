package control

import (
	"context"

	"reasonix/internal/contract/event"
	"reasonix/internal/contract/tool"
)

const elicitURLQuestion = "mcp.url"

func (c *Controller) elicitURL(ctx context.Context, req tool.ElicitRequest) (tool.ElicitReply, error) {
	origin := &event.AskOrigin{Kind: event.AskOriginMCP, Source: req.Source, Message: req.Message, URL: req.URL}
	questions := []event.AskQuestion{{ID: elicitURLQuestion, Header: "External interaction", Prompt: "Complete an external interaction", Reason: event.AskReasonUserDecision}}
	answers, err := c.ask(ctx, questions, origin)
	if err != nil {
		return tool.ElicitReply{}, err
	}
	return tool.ElicitReply{Action: elicitURLAction(answers)}, nil
}

func elicitURLAction(answers []event.AskAnswer) string {
	if len(answers) == 1 && answers[0].QuestionID == elicitURLQuestion && len(answers[0].Selected) == 1 {
		switch action := answers[0].Selected[0]; action {
		case "accept", "decline", "cancel":
			return action
		}
	}
	return "decline"
}
