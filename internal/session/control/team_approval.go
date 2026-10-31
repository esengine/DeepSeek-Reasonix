package control

import (
	"context"
	"encoding/json"
	"fmt"

	"reasonix/internal/contract/tool"
)

// TeamChangeApprovalTool is the card a model-proposed team change waits on when
// it widens what an agent can reach. Only a person answers it: no posture,
// grant, hook or remembered rule stands in.
const TeamChangeApprovalTool = "team_change"

// confirmTeamChange asks the person before an agent's working folder moves to
// one a model chose. Repository trust requires a separate host decision.
func (c *Controller) confirmTeamChange(ctx context.Context, agent, folder string) error {
	args, _ := json.Marshal(map[string]string{"agent": agent, "field": "folder", "to": folder})
	text := fmt.Sprintf("Let the agent %s read and write files in %s?", agent, folder)
	return c.confirmTeamEdit(ctx, agent, args, text)
}

func (c *Controller) confirmTeamConfiguration(ctx context.Context, id, name string, change tool.TeamAgentSpec) error {
	args, _ := json.Marshal(struct {
		Agent   string             `json:"agent"`
		Changes tool.TeamAgentSpec `json:"changes"`
	}{id, change})
	return c.confirmTeamEdit(ctx, name, args, "Apply these persistent instructions, role, model or folder changes to "+name+"?")
}

func (c *Controller) confirmTeamEdit(ctx context.Context, agent string, args json.RawMessage, text string) error {
	reply, err := c.requestApprovalDecision(ctx, approvalRequest{tool: TeamChangeApprovalTool, subject: agent, args: args, reason: text, fresh: true})
	switch {
	case err != nil && ctx.Err() != nil:
		return ctx.Err()
	case err != nil:
		return tool.Refusal{Code: "team.no_approver", Message: "Nobody approved the persistent change to " + agent + "."}
	case !reply.allow:
		return tool.Refusal{Code: "team.declined", Message: "The user declined the persistent change to " + agent + "."}
	}
	return nil
}
