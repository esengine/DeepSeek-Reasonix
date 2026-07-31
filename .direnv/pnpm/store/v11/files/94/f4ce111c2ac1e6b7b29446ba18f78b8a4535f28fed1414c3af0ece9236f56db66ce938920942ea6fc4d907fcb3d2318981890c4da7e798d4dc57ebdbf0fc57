/**
 * This function is an implementation of the [Floyd-Warshall algorithm][],
 * which finds the shortest path from each node to every other reachable node
 * in the graph. It is similar to {@link dijkstraAll}, but
 * it handles negative edge weights and is more efficient for some types of
 * graphs.
 *
 * [Floyd-Warshall algorithm]: https://en.wikipedia.org/wiki/Floyd-Warshall_algorithm
 *
 * @remarks This algorithm takes `O(|V|^3)` time.
 *
 * @example
 *
 * ![](https://github.com/dagrejs/graphlib/wiki/images/dijkstra-source.png)
 *
 * ```js
 * function weight(e) { return g.edge(e); }
 *
 * graphlib.alg.floydWarshall(g, function(e) { return g.edge(e); });
 *
 * // => { A:
 * //       { A: { distance: 0 },
 * //         B: { distance: 6, predecessor: 'C' },
 * //         C: { distance: 4, predecessor: 'A' },
 * //         D: { distance: 2, predecessor: 'A' },
 * //         E: { distance: 8, predecessor: 'F' },
 * //         F: { distance: 4, predecessor: 'D' } },
 * //      B:
 * //       { A: { distance: Infinity },
 * //         B: { distance: 0 },
 * //         C: { distance: Infinity },
 * //         D: { distance: Infinity },
 * //         E: { distance: 6, predecessor: 'B' },
 * //         F: { distance: Infinity } },
 * //      C: { ... },
 * //      D: { ... },
 * //      E: { ... },
 * //      F: { ... } }
 * ```
 *
 * @param {Graph} g - The graph to analyze.
 * @param {(e: EdgeObj) => number} [weightFn] - Optional function that returns
 * the weight for edge `e`. If no `weightFn` is supplied then each edge is
 * assumed to have a weight of 1.
 * @param {(v: NodeID) => EdgeObj[]} [edgeFn] - Optional function that returns
 * the ids of all edges incident to the node `v` for the purposes of shortest
 * path traversal.
 * By default this function uses the {@link Graph.outEdges} function on the
 * supplied graph.
 * @returns {Record<NodeID, Record<NodeID, PathEntry>>} a map of
 * `source -> { target -> { distance, predecessor }`.
 */
export function floydWarshall(g: Graph, weightFn?: (e: EdgeObj) => number, edgeFn?: (v: NodeID) => EdgeObj[]): Record<NodeID, Record<NodeID, PathEntry>>;
import type { Graph } from '../graph.js';
import type { EdgeObj } from '../graph.js';
import type { NodeID } from '../graph.js';
import type { PathEntry } from './dijkstra.js';
