// projection_debt.go — what the model has of one block that is delivered once.
package control

// projectionDebt answers "is this block owed" from the model-visible history,
// never from what a turn was handed: a refusal, a fold, a rewind or a session
// rotation all change the answer by themselves. newest reports the most recent
// block of a tag the model still reads (agent.NewestStandingBlock); nil means
// no model is attached, so every block is owed.
type projectionDebt struct {
	newest func(string) (string, bool)
}

// owed returns the projection unless the model's newest block of this tag is
// already those exact bytes. An older copy does not count: a later block of the
// same tag replaced it, and the caller may be owing those bytes again.
func (d *projectionDebt) owed(tag, projection string) string {
	if projection == "" || d.held(tag, wrapTurnBlock(tag, projection)) {
		return ""
	}
	return projection
}

// owedBlock is owed for a renderer that writes its own opening tag: a variant
// block carries attributes, which wrapTurnBlock cannot express, so the caller
// passes the whole rendered block and the newest one must match it verbatim.
func (d *projectionDebt) owedBlock(tag, block string) string {
	if block == "" || d.held(tag, block) {
		return ""
	}
	return block
}

func (d *projectionDebt) held(tag, block string) bool {
	if d.newest == nil {
		return false
	}
	got, ok := d.newest(tag)
	return ok && got == block
}

// bindProjectionSeer points every once-delivered block at the same model-visible
// history. New calls it once, before a turn can read a debt; a controller with
// no executor leaves every seer nil and owes every block, because no model has
// been told anything.
func (c *Controller) bindProjectionSeer(newest func(string) (string, bool), notes func(string) bool) {
	if newest == nil {
		return
	}
	c.date.debt.newest = newest
	c.scratch.debt.newest = newest
	c.skills.catalog.newest = newest
	c.memory.instructions.newest = newest
	c.rules.always.newest = newest
	c.rules.scoped.newest = newest
	c.goalBlock.newest = newest
	c.team.identity.newest = newest
	c.team.roster.newest = newest
	c.team.group.newest = newest
	c.deliveryNotes(notes)
}

// deliveredNewest is the seer a controller uses for its own model: it resolves
// the executor held at call time, so an adopted or rotated session is answered
// from the session this runtime has now, and it answers "nothing is visible"
// until a turn is handed to the runner. A switched model inherits another
// model's delivery, so the replacement restates the blocks once (see Restate).
func (c *Controller) deliveredNewest(tag string) (string, bool) {
	if c == nil || c.restateProjection.Load() || c.executor == nil {
		return "", false
	}
	return c.executor.NewestStandingBlock(tag)
}

// deliveredSees is deliveredNewest's question for the one-time notes.
func (c *Controller) deliveredSees(text string) bool {
	if c == nil || c.restateProjection.Load() || c.executor == nil {
		return false
	}
	return c.executor.SeesProjection(text)
}

// RestateStandingProjection marks this generation as having delivered nothing to
// the model it is about to address: a rebuild that switched models adopted a
// conversation composed for the model being replaced, not for this one.
func (c *Controller) RestateStandingProjection() {
	if c == nil {
		return
	}
	c.restateProjection.Store(true)
}

// settleProjection records that a composed turn is being handed to the runner.
// From here on the visible history is the record of what this runtime delivered.
func (c *Controller) settleProjection() {
	c.restateProjection.Store(false)
}

// deliveryNotes binds the one-time facts a turn also carries. They are not
// standing blocks: a note is delivered when the model can read it anywhere.
func (c *Controller) deliveryNotes(notes func(string) bool) {
	if notes == nil {
		return
	}
	c.mcp.promptFailures.sees = notes
	c.refused.sees = notes
}

// visible reports whether the model still reads any block with this tag. It
// answers "is anything of this kind in view", where owed asks about the exact
// bytes this turn would render: a caller replacing a block needs the first
// question, and can then owe the replacement.
func (d *projectionDebt) visible(tag string) bool {
	if d.newest == nil || tag == "" {
		return false
	}
	_, ok := d.newest(tag)
	return ok
}
