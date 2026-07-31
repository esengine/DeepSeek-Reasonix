/**
 * An implementation of [topological sorting](https://en.wikipedia.org/wiki/Topological_sorting).
 *
 * @remarks Takes `O(|V| + |E|)` time.
 *
 * @example
 *
 * ![](https://github.com/dagrejs/graphlib/wiki/images/topsort.png)
 *
 * ```js
 * graphlib.alg.topsort(g)
 * // [ '1', '2', '3', '4' ] or [ '1', '3', '2', '4' ]
 * ```
 *
 * @param {Graph} g - The graph to sort.
 * @returns {NodeID[]} an array of nodes
 * such that for each edge `u -> v`, `u` appears before `v` in the array.
 * @throws {CycleException} If the graph has a cycle so that it is impossible
 * to generate a topological sort.
 */
export function topsort(g: Graph): NodeID[];
export namespace topsort {
    export { CycleException };
}
/**
 * @class
 */
export function CycleException(): void;
export class CycleException {
}
import type { Graph } from '../graph.js';
import type { NodeID } from '../graph.js';
