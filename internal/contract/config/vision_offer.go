package config

import "strings"

// FirstVisionModelRef is a configured model that reads images, or "" if none
// does. It answers what an attachment raises when the main model cannot read it
// and no vision role is set: could anything here have, or does the picture go
// nowhere. Configured order, so the answer is stable between runs.
func (c *Config) FirstVisionModelRef() string {
	if c == nil {
		return ""
	}
	for i := range c.Providers {
		p := &c.Providers[i]
		for _, model := range p.ChatModelList() {
			ref := strings.TrimSpace(p.Name) + "/" + strings.TrimSpace(model)
			if entry, ok := c.ResolveModel(ref); ok && EffectiveVision(entry) {
				return ref
			}
		}
	}
	return ""
}
