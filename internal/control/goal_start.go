package control

// goalLaunchState is the one-shot "user just started this Goal" flag.
// It is host-only and never persisted.
type goalLaunchState struct {
	explicit bool
}

func (g *goalMachine) markExplicitStart() {
	g.mu.Lock()
	g.launch.explicit = true
	g.mu.Unlock()
}

func (g *goalMachine) consumeExplicitStart() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	ok := g.launch.explicit
	g.launch.explicit = false
	return ok
}

func (c *Controller) consumeExplicitGoalStart() bool {
	if c == nil {
		return false
	}
	return c.goals.consumeExplicitStart()
}
