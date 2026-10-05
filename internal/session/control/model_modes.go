package control

import (
	"errors"
	"fmt"
)

// ErrModelModeUnsupported is a mode the session's model does not declare.
var ErrModelModeUnsupported = errors.New("model mode not declared for this model")

// ModelModeView is one optional mode the session's model declares, as a
// frontend draws its switch.
type ModelModeView struct {
	ID       string `json:"id"`
	LabelKey string `json:"labelKey"`
	HintKey  string `json:"hintKey"`
	Costlier bool   `json:"costlier"`
	Active   bool   `json:"active"`
}

// ModelModes lists the modes the session's model declares, nil when it
// declares none — which is what keeps the switch off every other model's menu.
func (c *Controller) ModelModes() []ModelModeView {
	if len(c.modelModes) == 0 {
		return nil
	}
	active := c.ModelMode()
	out := make([]ModelModeView, 0, len(c.modelModes))
	for _, m := range c.modelModes {
		out = append(out, ModelModeView{ID: m.ID, LabelKey: m.LabelKey, HintKey: m.HintKey, Costlier: m.Costlier, Active: m.ID == active})
	}
	return out
}

// ModelMode is the mode the next request carries, "" when off.
func (c *Controller) ModelMode() string {
	if c.executor == nil {
		return ""
	}
	return c.executor.RequestMode()
}

// SetModelMode selects a declared mode for this conversation; "" turns it off.
// It rides the request, so it takes effect on the next model call and leaves
// the cached prefix alone.
func (c *Controller) SetModelMode(id string) error {
	if id != "" && !c.declaresModelMode(id) {
		return fmt.Errorf("%w: %q on %s", ErrModelModeUnsupported, id, c.modelRef)
	}
	if c.executor != nil {
		c.executor.SetRequestMode(id)
	}
	return nil
}

func (c *Controller) declaresModelMode(id string) bool {
	for _, m := range c.modelModes {
		if m.ID == id {
			return true
		}
	}
	return false
}
