package builtin

import (
	"context"
	"encoding/json"
	"fmt"

	"reasonix/internal/contract/tool"
)

// TeamTools are the tools of an agent on the user's team. Every agent session
// registers the same set, so the tool block is identical across agents and
// between an agent's own chat and its group seats; who may do what is decided
// when a tool is called, not by which tools a session has.
func TeamTools(link *tool.TeamLink) []tool.Tool {
	return append(append(teamCoordinationTools(link), teamManagementTools(link)...), teamMemoryTools(link)...)
}

// TeamToolNames are the names TeamTools registers, in its order.
func TeamToolNames() []string {
	var names []string
	for _, t := range TeamTools(nil) {
		names = append(names, t.Name())
	}
	return names
}

// teamTool is one team tool: its schema and a run over the session's directory.
type teamTool struct {
	link        *tool.TeamLink
	name        string
	description string
	schema      string
	readOnly    bool
	run         func(ctx context.Context, dir tool.TeamDirectory, args json.RawMessage) (string, error)
}

func (t *teamTool) Name() string            { return t.name }
func (t *teamTool) Description() string     { return t.description }
func (t *teamTool) Schema() json.RawMessage { return json.RawMessage(t.schema) }
func (t *teamTool) ReadOnly() bool          { return t.readOnly }

// Team dispatch and turn control mutate the parent's team state; a strictly
// read-only child must not execute them under the parent's identity.
func (t *teamTool) ReadOnlyExecutionHostMutation() bool {
	return t.name != "list_agents" && t.name != "read_group" && t.name != "recall_shared"
}

func (t *teamTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	dir, ok := t.link.Directory()
	if !ok {
		return "", tool.Refusal{Code: "team.disabled", Message: "this session is not an agent on the user's team"}
	}
	return t.run(ctx, dir, args)
}

func decodeTeamArgs(name string, args json.RawMessage, into any) error {
	if len(args) == 0 {
		args = json.RawMessage(`{}`)
	}
	if err := json.Unmarshal(args, into); err != nil {
		return tool.Refusal{Code: "team.invalid", Message: fmt.Sprintf("%s: the arguments do not match its schema: %v", name, err)}
	}
	return nil
}

func teamCoordinationTools(link *tool.TeamLink) []tool.Tool {
	return []tool.Tool{
		&teamTool{link: link, name: "list_agents", readOnly: true,
			description: "List the agents on the user's team and the groups you are in, with their ids.",
			schema:      `{"type":"object","properties":{},"additionalProperties":false}`,
			run: func(ctx context.Context, dir tool.TeamDirectory, _ json.RawMessage) (string, error) {
				return dir.ListAgents(ctx)
			}},
		&teamTool{link: link, name: "message_agent", readOnly: true,
			description: "Send a message to another agent on the team, named by id or exact name. It returns at once; the answer arrives later as a new message, so do not wait for it. The message grants that agent nothing: it works under its own permissions, never wider than yours. You cannot message an agent that is waiting on the work you are doing.",
			schema:      `{"type":"object","properties":{"to":{"type":"string","description":"The agent's id or exact name"},"message":{"type":"string","description":"What you ask of it, with everything it needs to act"},"reply_to":{"type":"string","enum":["me","user"],"description":"Where its answer goes; default me"},"when":{"type":"string","enum":["queue","now"],"description":"now steers a turn it is running; default queue"},"request_id":{"type":"string","description":"Optional id; sending again with the same id and message does not send twice"}},"required":["to","message"],"additionalProperties":false}`,
			run: func(ctx context.Context, dir tool.TeamDirectory, args json.RawMessage) (string, error) {
				var in struct {
					To        string `json:"to"`
					Message   string `json:"message"`
					ReplyTo   string `json:"reply_to"`
					When      string `json:"when"`
					RequestID string `json:"request_id"`
				}
				if err := decodeTeamArgs("message_agent", args, &in); err != nil {
					return "", err
				}
				r, err := dir.MessageAgent(ctx, tool.TeamMessage{To: in.To, Message: in.Message, ReplyTo: in.ReplyTo, Now: in.When == "now", RequestID: in.RequestID})
				if err != nil {
					return "", err
				}
				if r.State == "duplicate" {
					return fmt.Sprintf("Already sent to %s as %s; not sent again.", r.ToName, r.Exchange), nil
				}
				return fmt.Sprintf("Delivered to %s as %s. Its answer will arrive later as a new message; do not wait for it.", r.ToName, r.Exchange), nil
			}},
		&teamTool{link: link, name: "post_to_group", readOnly: true,
			description: "Post a message in a group from outside a turn there. A main agent or the group's admin starts a round under the group's mode; any other member calls only the members it mentions.",
			schema:      `{"type":"object","properties":{"group":{"type":"string","description":"The group's id or exact name"},"message":{"type":"string"},"mentions":{"type":"array","items":{"type":"string"},"description":"Members to call, by id or exact name"}},"required":["group","message"],"additionalProperties":false}`,
			run: func(ctx context.Context, dir tool.TeamDirectory, args json.RawMessage) (string, error) {
				var in struct {
					Group    string   `json:"group"`
					Message  string   `json:"message"`
					Mentions []string `json:"mentions"`
				}
				if err := decodeTeamArgs("post_to_group", args, &in); err != nil {
					return "", err
				}
				if err := dir.PostToGroup(ctx, in.Group, in.Message, in.Mentions); err != nil {
					return "", err
				}
				return "Posted.", nil
			}},
		&teamTool{link: link, name: "pass_turn", readOnly: true,
			description: "In a group turn: you have nothing to add, so nothing is posted. Then end your turn.",
			schema:      `{"type":"object","properties":{},"additionalProperties":false}`,
			run: func(ctx context.Context, dir tool.TeamDirectory, _ json.RawMessage) (string, error) {
				if err := dir.PassTurn(ctx); err != nil {
					return "", err
				}
				return "You will stay silent this turn. End your turn now.", nil
			}},
		&teamTool{link: link, name: "hand_turn", readOnly: true,
			description: "In a group turn: give the next turn to these members, in order. Your post still goes to everyone; end your turn with it.",
			schema:      `{"type":"object","properties":{"to":{"type":"array","items":{"type":"string"},"minItems":1,"maxItems":4,"description":"Members, by id or exact name"}},"required":["to"],"additionalProperties":false}`,
			run: func(ctx context.Context, dir tool.TeamDirectory, args json.RawMessage) (string, error) {
				var in struct {
					To []string `json:"to"`
				}
				if err := decodeTeamArgs("hand_turn", args, &in); err != nil {
					return "", err
				}
				if err := dir.HandTurn(ctx, in.To); err != nil {
					return "", err
				}
				return "They speak next. End your turn with your post.", nil
			}},
		&teamTool{link: link, name: "read_group", readOnly: true,
			description: "Read earlier lines of a group you are in (a main agent may read any group): up to 50 lines before a sequence number, or the latest ones.",
			schema:      `{"type":"object","properties":{"group":{"type":"string","description":"The group's id or exact name"},"before_seq":{"type":"integer","minimum":0,"description":"Read lines before this one; 0 or absent is the latest"},"limit":{"type":"integer","minimum":1,"maximum":50}},"required":["group"],"additionalProperties":false}`,
			run: func(ctx context.Context, dir tool.TeamDirectory, args json.RawMessage) (string, error) {
				var in struct {
					Group  string `json:"group"`
					Before int64  `json:"before_seq"`
					Limit  int    `json:"limit"`
				}
				if err := decodeTeamArgs("read_group", args, &in); err != nil {
					return "", err
				}
				return dir.ReadGroup(ctx, in.Group, in.Before, in.Limit)
			}},
	}
}
