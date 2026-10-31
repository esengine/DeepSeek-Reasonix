package builtin

import (
	"context"
	"encoding/json"

	"reasonix/internal/contract/tool"
)

// sharedArgs is what picks a shared store; the rest of a call is the memory
// tool's own arguments, handed over untouched.
type sharedArgs struct {
	Tier  string `json:"tier"`
	Group string `json:"group"`
}

const sharedTierProps = `"tier":{"type":"string","enum":["team","group"],"description":"team: what every agent on the team shares; group: what one group's members share"},` +
	`"group":{"type":"string","description":"For tier group: the group's id or exact name; default the group this turn is in"}`

// teamMemoryTools reach the memories agents share. Each picks a store by tier
// and hands the call to the memory tool bound to it, so a shared memory keeps
// the same revisions, subject keys and search as an agent's own.
func teamMemoryTools(link *tool.TeamLink) []tool.Tool {
	return []tool.Tool{
		&teamTool{link: link, name: "request_secret", readOnly: true,
			description: "Ask the user for a secret you need, such as an API key or a password, from your own conversation (not a group). End your turn after calling it: the user fills in a card, and you get a message when they save or decline. You never see the value; once saved, your shell commands read it as $REASONIX_SECRET_<NAME>. Never ask the user to paste a secret into the chat.",
			schema:      `{"type":"object","properties":{"name":{"type":"string","description":"A short name such as SHOP_API_KEY; it becomes the variable's suffix"},"purpose":{"type":"string","description":"What you need it for, shown to the user on the card"}},"required":["name","purpose"],"additionalProperties":false}`,
			run: func(ctx context.Context, dir tool.TeamDirectory, args json.RawMessage) (string, error) {
				var in struct {
					Name    string `json:"name"`
					Purpose string `json:"purpose"`
				}
				if err := decodeTeamArgs("request_secret", args, &in); err != nil {
					return "", err
				}
				return dir.RequestSecret(ctx, in.Name, in.Purpose)
			}},
		&teamTool{link: link, name: "remember_shared", readOnly: false,
			description: "Save or update a memory shared with other agents: tier team for what the whole team should know, tier group for what one group's members should know. Your own memory stays with remember. Only a main agent or a group's admin writes the team's memory; a group's members write its memory. Search with recall_shared first and reuse a name to update rather than duplicate.",
			schema: `{"type":"object","properties":{` + sharedTierProps + `,` +
				`"id":{"type":"string","description":"Stable memory id for an update, from recall_shared"},` +
				`"expected_revision":{"type":"integer","minimum":1,"description":"Revision from recall_shared; the update fails instead of overwriting a newer change"},` +
				`"name":{"type":"string","description":"Reference from recall_shared to update it, or a short kebab-case slug for a new memory"},` +
				`"title":{"type":"string","description":"Short label shown in the list"},` +
				`"description":{"type":"string","description":"One-line hook shown in the list; make it specific"},` +
				`"type":{"type":"string","enum":["user","feedback","project","reference"],"description":"Category of the fact"},` +
				`"subject_key":{"type":"string","description":"Dotted key naming the question the fact answers; one active value per key"},` +
				`"expires_at":{"type":"string","description":"Hard expiry as RFC3339 or YYYY-MM-DD; never clears one"},` +
				`"keywords":{"type":"string","description":"Space-separated search aliases the body's own words would miss"},` +
				`"body":{"type":"string","description":"The fact itself (Markdown)"}` +
				`},"required":["tier","description","body"],"additionalProperties":false}`,
			run: func(ctx context.Context, dir tool.TeamDirectory, args json.RawMessage) (string, error) {
				return runShared(ctx, dir, "remember_shared", args, true)
			}},
		&teamTool{link: link, name: "recall_shared", readOnly: true,
			description: "Search, list or read the memories agents share: tier team for the whole team's, tier group for one group's. Your own memories stay with the memory tool.",
			schema: `{"type":"object","properties":{` + sharedTierProps + `,` +
				`"operation":{"type":"string","enum":["search","read","list"],"description":"search ranks memories; read returns one by id or name; list returns them all"},` +
				`"query":{"type":"string","description":"Search query for operation=search"},` +
				`"name":{"type":"string","description":"Memory id or name for operation=read"},` +
				`"type":{"type":"string","enum":["user","feedback","project","reference"],"description":"Optional type filter"},` +
				`"limit":{"type":"integer","description":"Maximum results, default 8, max 20"}` +
				`},"required":["tier","operation"],"additionalProperties":false}`,
			run: func(ctx context.Context, dir tool.TeamDirectory, args json.RawMessage) (string, error) {
				return runShared(ctx, dir, "recall_shared", args, false)
			}},
	}
}

func runShared(ctx context.Context, dir tool.TeamDirectory, name string, args json.RawMessage, write bool) (string, error) {
	var pick sharedArgs
	if err := decodeTeamArgs(name, args, &pick); err != nil {
		return "", err
	}
	var rest map[string]json.RawMessage
	if err := decodeTeamArgs(name, args, &rest); err != nil {
		return "", err
	}
	for _, k := range []string{"tier", "group", "scope", "activation"} {
		delete(rest, k)
	}
	bound, err := dir.SharedMemory(ctx, pick.Tier, pick.Group, write)
	if err != nil {
		return "", err
	}
	forward, err := json.Marshal(rest)
	if err != nil {
		return "", err
	}
	return bound.Execute(ctx, forward)
}
