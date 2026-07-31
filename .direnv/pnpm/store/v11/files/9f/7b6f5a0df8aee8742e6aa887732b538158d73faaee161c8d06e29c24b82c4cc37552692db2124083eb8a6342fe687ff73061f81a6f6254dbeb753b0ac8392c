/**
 * Temporarily replace `Math.random` with a mulberry32-seeded PRNG for the
 * duration of `fn`, then restore the original. Used to make the
 * cytoscape-fcose layout deterministic — fcose calls `Math.random` internally
 * in its constraint solver and exposes no `seed` option of its own, so the
 * only reliable way to get repeatable layouts is to swap the global at the
 * narrowest possible scope.
 *
 * A seed of `0` is treated as "opt out": the global is left alone and `fn`
 * runs against the real `Math.random`. This preserves the pre-fix behavior
 * for any caller that explicitly wants layout variety per render.
 */
export declare function withSeededRandom<T>(seed: number, fn: () => T): T;
