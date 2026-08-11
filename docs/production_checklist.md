# Reasonix v5 Stable Production Checklist

Reasonix v5.9.9 is frozen as a stable release candidate. This checklist is the
release gate for PR #5217 and future v5-stable maintenance work.

## Release Gate

All checks below must pass before the v5 stable release candidate is merged.

## 1. Runtime Safety

- Sandbox isolation is verified.
- Resource budgets use ledger-based two-phase reservation and commit.
- No shared execution context can leak across sandboxed executions.

## 2. Control System Stability

- Distributed control plane is active and deterministic.
- Global equilibrium layer is deterministic.
- No single meta-controller is reintroduced.

## 3. Memory System Safety

- Causal compression is stable.
- Long-tail predictive and causal signal retention is validated.
- Truth-lock decay changes influence weight only, not correctness.

## 4. Predictive System Isolation

- Shadow observer remains read-only.
- Prediction-action bridge is advisory-only.
- Predictive warnings do not feed back into execution automatically.

## 5. Temporal System

- Dual logical and physical time reporting is visible.
- Lag and damping windows remain separated.
- Physical latency variance is not hidden by logical time normalization.

## 6. Architecture Freeze

- `system.StableSystemContract()` validates the v5.9.9 release boundary.
- `system.ArchitectureLocked` is enabled.
- v6-pre diagnostics remain observation-only.

## 7. Observability

- Trace and diagnostic systems remain non-invasive.
- Layer-collapse diagnostics do not affect runtime, prompts, provider requests,
  or tool schemas.

## Final Release Flow

1. Validate the stable system contract.
2. Confirm the architecture lock is enabled.
3. Confirm v6-pre diagnostics are isolated.
4. Pass the production checklist.
5. Pass `system.ReleaseGuard()`.
6. Merge as the v5.9.9 stable release candidate.
