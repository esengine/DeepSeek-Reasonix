// Package navigator implements the OSWorld 2.0 "state-based navigator"
// paradigm as an independent kernel that any host (Reasonix or HERMES) can
// embed through a HostAdapter.
//
// OSWorld 2.0 (XLANG Lab) found that computer-operating agents fail 79.4% of
// real office tasks and reach 0% success beyond 163 minutes. The three
// systematic failure modes are:
//
//  1. Implicit-state amnesia — recovered file paths, inferred IDs, and
//     unexplored data sources are lost across compaction or long turns.
//  2. Dynamic-interface blindness — UI changes between actions go undetected,
//     so the agent acts on a stale screen model.
//  3. Environment-update deafness — filesystem/process/config changes made by
//     the task itself or by external processes are not perceived.
//
// The Navigator Kernel addresses all three through a continuous-state,
// closed-loop architecture:
//
//   - ContinuousStateManager maintains a state graph + trajectory and never
//     relies on compaction to preserve implicit facts.
//   - ClosedLoopEngine generates a hypothesis before each action, observes the
//     real outcome, and corrects on deviation.
//   - DynamicEnvSensor runs background sensors (filesystem, process, interface)
//     and correlates their events into the state graph.
//
// The kernel has zero dependencies on Reasonix internals; a HostAdapter bridges
// it to whatever runtime embeds it. ReasonixAdapter wires it to the existing
// agent/control/hook/event stack; HermesAdapter does the same for the HERMES
// operator shell so HERMES can optimize its kernel on top of this one.
package navigator
