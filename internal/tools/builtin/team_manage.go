package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"reasonix/internal/contract/tool"
)

type teamAgentArgs struct {
	Name         *string `json:"name"`
	Role         *string `json:"role"`
	Instructions *string `json:"instructions"`
	Model        *string `json:"model"`
	Folder       *string `json:"folder"`
	Glyph        *string `json:"glyph"`
	Color        *string `json:"color"`
}

func (a teamAgentArgs) spec() tool.TeamAgentSpec {
	return tool.TeamAgentSpec{Name: a.Name, Role: a.Role, Instructions: a.Instructions, Model: a.Model, Folder: a.Folder, Glyph: a.Glyph, Color: a.Color}
}

const teamAgentProps = `"name":{"type":"string","maxLength":40},"role":{"type":"string","maxLength":24,"description":"A short label, such as Writer"},"instructions":{"type":"string","description":"Standing instructions the agent works by"},"model":{"type":"string","description":"Model reference; only an agent that has not started yet takes it"},"folder":{"type":"string","description":"Absolute path of the folder the agent works in; the user approves it. An empty string returns it to its own folder."},"glyph":{"type":"string","description":"Avatar shape token"},"color":{"type":"string","description":"Avatar colour token"}`

func agentLine(v tool.TeamAgentView) string {
	line := fmt.Sprintf("%s (%s)", v.Name, v.ID)
	if v.Role != "" {
		line += " · " + v.Role
	}
	if v.Main {
		line += " · main"
	}
	return line
}

func teamManagementTools(link *tool.TeamLink) []tool.Tool {
	return []tool.Tool{
		&teamTool{link: link, name: "create_agent",
			description: "Main agents only: add a long-lived agent to the user's team. It starts in its own conversation and can be messaged at once.",
			schema:      `{"type":"object","properties":{` + teamAgentProps + `},"required":["name"],"additionalProperties":false}`,
			run: func(ctx context.Context, dir tool.TeamDirectory, args json.RawMessage) (string, error) {
				var in teamAgentArgs
				if err := decodeTeamArgs("create_agent", args, &in); err != nil {
					return "", err
				}
				v, err := dir.CreateAgent(ctx, in.spec())
				if err != nil {
					return "", err
				}
				return "Created " + agentLine(v) + ".", nil
			}},
		&teamTool{link: link, name: "update_agent",
			description: "Change an agent's name, role, instructions, model or avatar. Any agent may change itself; changing another takes a main agent. Permissions are the user's to set and are not changed here.",
			schema:      `{"type":"object","properties":{"agent":{"type":"string","description":"The agent's id or exact name; absent is yourself"},` + teamAgentProps + `},"additionalProperties":false}`,
			run: func(ctx context.Context, dir tool.TeamDirectory, args json.RawMessage) (string, error) {
				var in struct {
					Agent string `json:"agent"`
					teamAgentArgs
				}
				if err := decodeTeamArgs("update_agent", args, &in); err != nil {
					return "", err
				}
				v, err := dir.UpdateAgent(ctx, in.Agent, in.spec())
				if err != nil {
					return "", err
				}
				return "Updated " + agentLine(v) + ". A changed name, role or instructions reaches it from its next turn.", nil
			}},
		&teamTool{link: link, name: "stop_agent",
			description: "Main agents only: stop what another agent is running in its own conversation.",
			schema:      `{"type":"object","properties":{"agent":{"type":"string","description":"The agent's id or exact name"}},"required":["agent"],"additionalProperties":false}`,
			run: func(ctx context.Context, dir tool.TeamDirectory, args json.RawMessage) (string, error) {
				var in struct {
					Agent string `json:"agent"`
				}
				if err := decodeTeamArgs("stop_agent", args, &in); err != nil {
					return "", err
				}
				stopped, err := dir.StopAgent(ctx, in.Agent)
				if err != nil {
					return "", err
				}
				if !stopped {
					return "That agent was not running anything.", nil
				}
				return "Stopped. Whoever it was answering is told it was stopped.", nil
			}},
		&teamTool{link: link, name: "create_group",
			description: "Main agents only: open a group chat. You join it and are its admin unless you name another member.",
			schema:      `{"type":"object","properties":{"members":{"type":"array","items":{"type":"string"},"minItems":1,"maxItems":11,"description":"Other members, by id or exact name"},"name":{"type":"string","maxLength":60},"notice":{"type":"string","description":"What the group is for, shown to every member"},"admin":{"type":"string","description":"The admin, if not you"},"mode":{"type":"string","enum":["admin","everyone"],"description":"Who answers the user by default; default admin"}},"required":["members"],"additionalProperties":false}`,
			run: func(ctx context.Context, dir tool.TeamDirectory, args json.RawMessage) (string, error) {
				var in struct {
					Members []string `json:"members"`
					Name    string   `json:"name"`
					Notice  string   `json:"notice"`
					Admin   string   `json:"admin"`
					Mode    string   `json:"mode"`
				}
				if err := decodeTeamArgs("create_group", args, &in); err != nil {
					return "", err
				}
				id, err := dir.CreateGroup(ctx, tool.TeamGroupSpec{Name: in.Name, Notice: in.Notice, Members: in.Members, Admin: in.Admin, Mode: in.Mode})
				if err != nil {
					return "", err
				}
				return "Created the group " + id + ".", nil
			}},
		&teamTool{link: link, name: "update_group",
			description: "Change a group: its name, notice, members, admin or who answers. A main agent may change any group; an admin only its own, and only a main agent may leave a group without an admin.",
			schema:      `{"type":"object","properties":{"group":{"type":"string","description":"The group's id or exact name"},"name":{"type":"string","maxLength":60},"notice":{"type":"string"},"add_members":{"type":"array","items":{"type":"string"}},"remove_members":{"type":"array","items":{"type":"string"}},"admin":{"type":"string","description":"The new admin; an empty string removes it"},"mode":{"type":"string","enum":["admin","everyone"]}},"required":["group"],"additionalProperties":false}`,
			run: func(ctx context.Context, dir tool.TeamDirectory, args json.RawMessage) (string, error) {
				var in struct {
					Group  string   `json:"group"`
					Name   *string  `json:"name"`
					Notice *string  `json:"notice"`
					Add    []string `json:"add_members"`
					Remove []string `json:"remove_members"`
					Admin  *string  `json:"admin"`
					Mode   *string  `json:"mode"`
				}
				if err := decodeTeamArgs("update_group", args, &in); err != nil {
					return "", err
				}
				changed, err := dir.UpdateGroup(ctx, in.Group, tool.TeamGroupChange{Name: in.Name, Notice: in.Notice, Add: in.Add, Remove: in.Remove, Admin: in.Admin, Mode: in.Mode})
				if err != nil {
					return "", err
				}
				if len(changed) == 0 {
					return "Nothing changed.", nil
				}
				return "Changed: " + strings.Join(changed, "; ") + ".", nil
			}},
		&teamTool{link: link, name: "delete_group",
			description: "Main agents only: archive a group. Its history stays and it takes no new posts; removing it for good is the user's call.",
			schema:      `{"type":"object","properties":{"group":{"type":"string","description":"The group's id or exact name"}},"required":["group"],"additionalProperties":false}`,
			run: func(ctx context.Context, dir tool.TeamDirectory, args json.RawMessage) (string, error) {
				var in struct {
					Group string `json:"group"`
				}
				if err := decodeTeamArgs("delete_group", args, &in); err != nil {
					return "", err
				}
				if err := dir.DeleteGroup(ctx, in.Group); err != nil {
					return "", err
				}
				return "Archived.", nil
			}},
	}
}
