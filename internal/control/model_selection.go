package control

import "reasonix/internal/agent"

// modelSelection is the connection this controller runs as one value: the ref
// and the identity of the connection it was resolved against always change
// together, and a restart must restore both or neither.
type modelSelection struct {
	ref      string
	identity string
}

// ValidateSessionModel checks saved identity before any lease or transcript
// transition. Explicit model changes deliberately build a new runtime instead.
func (c *Controller) ValidateSessionModel(path string) error {
	if c.resolveSessionModel == nil {
		return nil
	}
	model, identity, ok := agent.LoadSessionModelSelection(path)
	if !ok {
		return nil
	}
	_, err := c.resolveSessionModel(model, identity)
	return err
}
