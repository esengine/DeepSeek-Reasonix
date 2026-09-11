package agent

import (
	"context"
	"fmt"
	"strconv"
	"strings"
)

// fleetPlan is the validated dependency graph for one fleet call. Dependencies
// live here, on the graph, and never on a task spec: what a task is does not
// depend on what ran before it. Keeping them apart is what stops fleet from
// growing into a workflow language.
type fleetPlan struct {
	ids        []string
	deps       [][]int
	dependents [][]int
	// reachable[i] holds every index that transitively depends on i, so the
	// preflight can tell ordered items from genuinely concurrent ones.
	reachable []map[int]bool
	failFast  bool
}

// newFleetPlan validates ids and edges before anything runs. An unknown id, a
// duplicate, a self-edge, or a cycle fails the whole call: a fleet that starts
// and then discovers it cannot finish has already spent tokens.
func newFleetPlan(items []fleetTaskItem, failFast bool) (fleetPlan, error) {
	n := len(items)
	plan := fleetPlan{
		ids:        make([]string, n),
		deps:       make([][]int, n),
		dependents: make([][]int, n),
		reachable:  make([]map[int]bool, n),
		failFast:   failFast,
	}
	index := make(map[string]int, n)
	for i, item := range items {
		id := strings.TrimSpace(item.ID)
		if id == "" {
			id = strconv.Itoa(i + 1)
		}
		if prior, dup := index[id]; dup {
			return fleetPlan{}, fmt.Errorf("task %d: id %q is already used by task %d", i+1, id, prior+1)
		}
		index[id] = i
		plan.ids[i] = id
	}
	for i, item := range items {
		for _, raw := range item.DependsOn {
			dep := strings.TrimSpace(raw)
			target, ok := index[dep]
			if !ok {
				return fleetPlan{}, fmt.Errorf("task %d (%q): depends_on %q matches no task id", i+1, plan.ids[i], dep)
			}
			if target == i {
				return fleetPlan{}, fmt.Errorf("task %d (%q): depends_on itself", i+1, plan.ids[i])
			}
			plan.deps[i] = append(plan.deps[i], target)
			plan.dependents[target] = append(plan.dependents[target], i)
		}
	}
	if err := plan.rejectCycles(); err != nil {
		return fleetPlan{}, err
	}
	plan.computeReachability()
	return plan, nil
}

// rejectCycles runs Kahn's algorithm; anything left unvisited is in a cycle.
func (p fleetPlan) rejectCycles() error {
	pending := p.pendingCounts()
	queue := p.roots()
	visited := 0
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		visited++
		for _, next := range p.dependents[current] {
			pending[next]--
			if pending[next] == 0 {
				queue = append(queue, next)
			}
		}
	}
	if visited == len(p.ids) {
		return nil
	}
	var stuck []string
	for i, count := range pending {
		if count > 0 {
			stuck = append(stuck, p.ids[i])
		}
	}
	return fmt.Errorf("depends_on forms a cycle through: %s", strings.Join(stuck, ", "))
}

func (p fleetPlan) pendingCounts() []int {
	out := make([]int, len(p.ids))
	for i := range p.ids {
		out[i] = len(p.deps[i])
	}
	return out
}

func (p fleetPlan) roots() []int {
	var out []int
	for i := range p.ids {
		if len(p.deps[i]) == 0 {
			out = append(out, i)
		}
	}
	return out
}

func (p *fleetPlan) computeReachability() {
	for i := range p.ids {
		seen := map[int]bool{}
		var walk func(int)
		walk = func(from int) {
			for _, next := range p.dependents[from] {
				if seen[next] {
					continue
				}
				seen[next] = true
				walk(next)
			}
		}
		walk(i)
		p.reachable[i] = seen
	}
}

// describe names an item by its caller-visible position, adding the id only
// when the caller chose one, so diagnostics stay readable either way.
func (p fleetPlan) describe(i int) string {
	position := strconv.Itoa(i + 1)
	if p.ids[i] == position {
		return "task " + position
	}
	return fmt.Sprintf("task %s (%q)", position, p.ids[i])
}

// ordered reports whether one of the two items must finish before the other
// starts, in either direction.
func (p fleetPlan) ordered(a, b int) bool {
	return p.reachable[a][b] || p.reachable[b][a]
}

// validateConcurrentWriteClaims rejects overlapping write claims only for items
// that can actually run at the same time. Two writers joined by a dependency are
// serialised by the graph, so an implement → review chain may legitimately share
// paths that two parallel writers never could.
func (p fleetPlan) validateConcurrentWriteClaims(claims []WritePathSet) error {
	for i := range claims {
		if claims[i].Empty() {
			continue
		}
		for j := i + 1; j < len(claims); j++ {
			if claims[j].Empty() || p.ordered(i, j) {
				continue
			}
			if claims[i].WholeWorkspace || claims[j].WholeWorkspace {
				continue
			}
			if ScheduleOverlaps(claims[i], claims[j]) {
				return fmt.Errorf("%s and %s can run at the same time and their write claims conflict; add a depends_on between them or give them disjoint write_paths",
					p.describe(i), p.describe(j))
			}
		}
	}
	return nil
}

// skipDependents marks everything downstream of a failed item as skipped. A
// dependent never runs on a broken input: it would burn tokens to produce a
// result the parent must discard.
func (p fleetPlan) skipDependents(results []fleetItemResult, failed int) {
	for idx := range p.reachable[failed] {
		if results[idx].status != fleetItemPending {
			continue
		}
		results[idx].status = fleetItemSkipped
		results[idx].err = fmt.Errorf("skipped: depends on %q, which did not complete", p.ids[failed])
	}
}

// errFleetBranchNotStarted marks a task whose branch was cut before it ran.
var errFleetBranchNotStarted = fmt.Errorf("skipped: the fleet stopped starting new tasks")

func firstNonNilErr(errs ...error) error {
	for _, err := range errs {
		if err != nil {
			return err
		}
	}
	return nil
}

// driveFleet starts items as their dependencies complete and collects every
// terminal result. Started items always publish one, including after
// cancellation, so partial writer work is never reported as a task that never
// ran. It returns whether the run ended without every item completing.
func driveFleet(ctx context.Context, plan fleetPlan, results []fleetItemResult, doneCh <-chan fleetItemResult, wait func(), startOne func(int)) bool {
	pending := plan.pendingCounts()
	started, completed := 0, 0
	stopStarting := false
	launch := func(idx int) {
		if stopStarting || ctx.Err() != nil || results[idx].status != fleetItemPending {
			return
		}
		startOne(idx)
		started++
	}
	for _, idx := range plan.roots() {
		launch(idx)
	}

	cancelled := false
	for completed < started && !cancelled {
		select {
		case r := <-doneCh:
			results[r.index] = r
			completed++
			if r.status != fleetItemCompleted {
				plan.skipDependents(results, r.index)
				if plan.failFast {
					stopStarting = true
				}
				continue
			}
			for _, next := range plan.dependents[r.index] {
				if pending[next]--; pending[next] == 0 {
					launch(next)
				}
			}
		case <-ctx.Done():
			cancelled = true
		}
	}
	// doneCh is buffered for every item, so workers can always publish while
	// this goroutine waits; drain the outstanding ones rather than overwriting
	// their real status with skipped.
	wait()
	for completed < started {
		r := <-doneCh
		results[r.index] = r
		completed++
	}
	for i := range results {
		if results[i].status != fleetItemPending {
			continue
		}
		results[i].status = fleetItemSkipped
		if results[i].err == nil {
			results[i].err = firstNonNilErr(ctx.Err(), errFleetBranchNotStarted)
		}
	}
	return cancelled
}
