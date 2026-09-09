package agent

// A host-owned emergency or task budget stops the planner before it produced a
// complete plan, so every route fails closed: no executor runs, nothing to
// approve, nothing to return.
const plannerSafetyBoundaryError = "planner could not finalize before a safety boundary; no execution was started"
