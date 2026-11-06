package control

import "reasonix/internal/contract/config"

// MemoryProactiveSettings is what the model may do on its own with memory.
type MemoryProactiveSettings struct {
	Mode string `json:"mode"`
	Path string `json:"path"`
}

func (c *Controller) MemoryProactiveSettings() MemoryProactiveSettings {
	path := config.UserConfigPath()
	return MemoryProactiveSettings{Mode: config.LoadForEdit(path).MemoryProactive(), Path: path}
}

// SaveMemoryProactive takes effect for asking versus writing at once; whether
// the model is offered the remember tool at all is settled when the session opens.
func (c *Controller) SaveMemoryProactive(mode string) error {
	unlock := config.LockUserConfigEdits()
	defer unlock()
	path := config.UserConfigPath()
	cfg := config.LoadForEdit(path)
	if err := cfg.SetMemoryProactive(mode); err != nil {
		return err
	}
	if err := cfg.SaveTo(path); err != nil {
		return err
	}
	c.memory.book.setAsk(cfg.MemoryProactive() == "ask")
	return nil
}
