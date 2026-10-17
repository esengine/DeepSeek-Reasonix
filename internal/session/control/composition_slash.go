package control

import (
	"context"
	"fmt"
	"strings"
)

func (c *Controller) compositionOrModelNotice(fields []string) {
	if fields[0] == "/model" {
		c.notice(c.modelListText())
		return
	}
	c.compositionNotice(fields)
}

func (c *Controller) compositionNotice(fields []string) {
	if len(fields) > 1 {
		if err := c.SelectAgentComposition(context.Background(), fields[1]); err != nil {
			c.notice(err.Error())
		} else {
			c.notice("Agent composition selected.")
		}
		return
	}
	if b := c.CompositionBinding(); b != nil && b.Started {
		c.notice(fmt.Sprintf("%s — %s (%s). This conversation is locked; start a new session to choose another composition.", b.Version.Definition.ID, b.Version.Definition.Name, b.Version.Revision[:min(12, len(b.Version.Revision))]))
		return
	}
	catalog, err := c.AgentPresets()
	if err != nil {
		c.notice(err.Error())
		return
	}
	var lines []string
	for _, v := range catalog.Presets {
		lines = append(lines, fmt.Sprintf("%s — %s", v.Definition.ID, v.Definition.Name))
	}
	c.notice(strings.Join(lines, "\n"))
}

func (c *Controller) compositionArgItems() []SlashItem {
	if b := c.CompositionBinding(); b != nil && b.Started {
		return []SlashItem{{Label: b.Version.Definition.ID, Insert: b.Version.Definition.ID, Hint: b.Version.Definition.Name}}
	}
	catalog, err := c.PresetStore().List()
	if err != nil {
		return nil
	}
	items := make([]SlashItem, 0, len(catalog.Presets))
	for _, v := range catalog.Presets {
		items = append(items, SlashItem{Label: v.Definition.ID, Insert: v.Definition.ID, Hint: v.Definition.Name})
	}
	return items
}
