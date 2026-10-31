package control

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"reasonix/internal/base/i18n"
	"reasonix/internal/contract/config"
	"reasonix/internal/state/teamroster"
)

// teamNotice answers /team and /msg. Both work from any session, the
// terminal's included: /team reads the roster, and /msg leaves a message in
// the team's outbox, which the window holding the team delivers.
func (c *Controller) teamNotice(fields []string, trimmed string) bool {
	switch fields[0] {
	case "/team":
		c.notice(c.teamListText())
	case "/msg":
		c.notice(c.queueUserMessage(strings.TrimSpace(strings.TrimPrefix(trimmed, fields[0]))))
	default:
		return false
	}
	return true
}

func (c *Controller) teamStore() teamroster.Store {
	return teamroster.Store{Dir: config.RootsForHome(c.PresetStore().Home).TeamDir()}
}

func (c *Controller) teamListText() string {
	r, err := c.teamStore().Load()
	if err != nil {
		return fmt.Sprintf(i18n.M.TeamReadErrorFmt, err.Error())
	}
	if len(r.Agents) == 0 {
		return i18n.M.TeamEmpty
	}
	var b strings.Builder
	b.WriteString(i18n.M.TeamAgentsHeading + "\n")
	for _, a := range r.Agents {
		line := "- " + a.Name
		if a.Role != "" {
			line += " · " + a.Role
		}
		if a.Main {
			line += " · " + i18n.M.TeamMain
		}
		b.WriteString(line + "\n")
	}
	if groups := slices.DeleteFunc(slices.Clone(r.Groups), func(g teamroster.Group) bool { return g.Archived }); len(groups) > 0 {
		b.WriteString(i18n.M.TeamGroupsHeading + "\n")
		for _, g := range groups {
			var names []string
			for _, m := range g.Members {
				if a, ok := r.Agent(m); ok {
					names = append(names, a.Name)
				}
			}
			title := g.Name
			if title == "" {
				title = strings.Join(names, ", ")
			}
			b.WriteString("- " + title + ": " + strings.Join(names, ", ") + "\n")
		}
	}
	b.WriteString(i18n.M.TeamMsgHint)
	return b.String()
}

// queueUserMessage reads "<name> <message>", where the name may have spaces:
// the longest agent name the arguments start with wins.
func (c *Controller) queueUserMessage(args string) string {
	if args == "" {
		return i18n.M.MsgUsage
	}
	store := c.teamStore()
	r, err := store.Load()
	if err != nil {
		return fmt.Sprintf(i18n.M.TeamReadErrorFmt, err.Error())
	}
	var to *teamroster.Agent
	text := ""
	if id, rest, ok := strings.Cut(args, " "); ok {
		if agent, found := r.Agent(id); found {
			to, text = agent, strings.TrimSpace(rest)
		}
	}
	for i := range r.Agents {
		if to != nil && strings.HasPrefix(args, to.ID+" ") {
			break
		}
		a := &r.Agents[i]
		prefix := strings.ToLower(a.Name) + " "
		if strings.HasPrefix(strings.ToLower(args), prefix) && (to == nil || len(a.Name) > len(to.Name)) {
			to, text = a, strings.TrimSpace(args[len(prefix):])
		}
	}
	if to == nil {
		name, _, _ := strings.Cut(args, " ")
		return fmt.Sprintf(i18n.M.MsgUnknownAgentFmt, name)
	}
	if text == "" {
		return i18n.M.MsgUsage
	}
	if _, err := store.QueueUserMessage(context.Background(), to.ID, text); err != nil {
		return fmt.Sprintf(i18n.M.TeamReadErrorFmt, err.Error())
	}
	return fmt.Sprintf(i18n.M.MsgQueuedFmt, to.Name)
}
