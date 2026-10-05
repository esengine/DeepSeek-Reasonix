package plancontract

// Ordered returns the steps in projection order: each phase in
// dependency-respecting declared order, followed by its own sub-steps in the
// same order. Render and ProjectTodos both read it, which is what keeps the list
// a user approves and the task list the host seeds from ever disagreeing.
func (p Plan) Ordered() []Step {
	if len(p.Steps) == 0 {
		return nil
	}
	parents := phaseIDs(p.Steps)
	phases := make([]Step, 0, len(p.Steps))
	children := make(map[string][]Step)
	for i, s := range p.Steps {
		if parents[i] == "" {
			phases = append(phases, s)
			continue
		}
		children[parents[i]] = append(children[parents[i]], s)
	}
	ordered, _ := sortSiblings(phases)
	out := make([]Step, 0, len(p.Steps))
	for _, phase := range ordered {
		out = append(out, phase)
		kids, _ := sortSiblings(children[phase.ID])
		out = append(out, kids...)
	}
	return out
}

// phaseIDs resolves each step to the id of the top-level phase it belongs to,
// or "" when the step is a phase itself. No parent, an unknown parent, and a
// parent chain that loops all mean the same thing, and nesting deeper than two
// levels flattens onto the top ancestor — the shape the task list can hold.
func phaseIDs(steps []Step) []string {
	index := make(map[string]int, len(steps))
	for i, s := range steps {
		index[s.ID] = i
	}
	out := make([]string, len(steps))
	const (
		todo = iota
		resolving
		done
	)
	state := make([]int, len(steps))
	var resolve func(int) string
	resolve = func(i int) string {
		switch state[i] {
		case done:
			return out[i]
		case resolving:
			return ""
		}
		state[i] = resolving
		if parent, ok := index[steps[i].ParentID]; ok && parent != i {
			if top := resolve(parent); top != "" {
				out[i] = top
			} else {
				out[i] = steps[parent].ID
			}
		}
		if out[i] == steps[i].ID {
			out[i] = ""
		}
		state[i] = done
		return out[i]
	}
	for i := range steps {
		resolve(i)
	}
	return out
}

// siblingGroups partitions steps by phase, phases first, so a dependency check
// only ever compares steps that can actually be reordered against each other.
func siblingGroups(steps []Step) [][]Step {
	if len(steps) == 0 {
		return nil
	}
	parents := phaseIDs(steps)
	phases := make([]Step, 0, len(steps))
	byPhase := make(map[string][]Step)
	order := make([]string, 0, len(steps))
	for i, s := range steps {
		if parents[i] == "" {
			phases = append(phases, s)
			continue
		}
		if _, ok := byPhase[parents[i]]; !ok {
			order = append(order, parents[i])
		}
		byPhase[parents[i]] = append(byPhase[parents[i]], s)
	}
	out := make([][]Step, 0, len(order)+1)
	out = append(out, phases)
	for _, phase := range order {
		out = append(out, byPhase[phase])
	}
	return out
}

// sortSiblings orders one phase's steps so a step follows the siblings it
// depends on, breaking ties by declared order. Dependencies outside the sibling
// set are ignored — they cannot order anything here — and a cycle reports
// cyclic while still emitting every step, so ordering never drops work.
func sortSiblings(steps []Step) (ordered []Step, cyclic bool) {
	if len(steps) < 2 {
		return steps, false
	}
	index := make(map[string]int, len(steps))
	for i, s := range steps {
		index[s.ID] = i
	}
	deps := make([][]int, len(steps))
	for i, s := range steps {
		for _, dep := range s.DependsOn {
			if j, ok := index[dep]; ok && j != i {
				deps[i] = append(deps[i], j)
			}
		}
	}
	done := make([]bool, len(steps))
	out := make([]Step, 0, len(steps))
	for len(out) < len(steps) {
		pick := -1
		for i := range steps {
			if done[i] || !ready(deps[i], done) {
				continue
			}
			pick = i
			break
		}
		if pick < 0 {
			for i := range steps {
				if !done[i] {
					out = append(out, steps[i])
				}
			}
			return out, true
		}
		done[pick] = true
		out = append(out, steps[pick])
	}
	return out, false
}

func ready(deps []int, done []bool) bool {
	for _, j := range deps {
		if !done[j] {
			return false
		}
	}
	return true
}
