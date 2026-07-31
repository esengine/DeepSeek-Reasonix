/**
 * Register a baseline DDLT test suite for a `.mmd`+`.sizes.json` fixture pair.
 *
 * One call per fixture, e.g. `baselineDdltSpec('domus1')`. The spec file is
 * literally that one line plus the import — no boilerplate, no per-fixture
 * setup. Asserts the universal invariants every layout should hold:
 *
 *   1. Layout produces a non-empty node list and edge list.
 *   2. Every edge has at least 2 routed points.
 *   3. No edge segment passes through a non-endpoint, non-group node's
 *      interior (the libavoid per-edge-veto regression — the
 *      "Customer→USCompany under USCompany" class of bug).
 *   4. Every edge terminates at a point on its endpoint nodes' boundaries
 *      (not strictly interior).
 *   5. validateLayout produces a finite breakdown.
 *
 * Per-fixture quality and known-failing cases stay in dedicated specs.
 */
export declare function baselineDdltSpec(name: string): void;
