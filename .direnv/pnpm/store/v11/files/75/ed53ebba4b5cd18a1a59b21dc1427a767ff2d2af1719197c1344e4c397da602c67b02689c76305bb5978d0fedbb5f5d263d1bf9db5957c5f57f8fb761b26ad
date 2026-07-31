/**
 * This function finds the shortest path from each node to every other
 * reachable node in the graph. It is similar to
 * {@link dijkstra}, but instead of returning a single-source
 * array, it returns a mapping of of `source -> alg.dijksta(g, source,
 * weightFn, edgeFn)`.
 *
 * @remarks This function takes `O(|V| * (|E| + |V|) * log |V|)` time.
 *
 * @example
 *
 * ![](https://github.com/dagrejs/graphlib/wiki/images/dijkstra-source.png)
 *
 * ```js
 * function weight(e) { return g.edge(e); }
 *
 * graphlib.alg.dijkstraAll(g, function(e) { return g.edge(e); });
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
 * @param {Graph} g - Input graph.
 * @param {(e: EdgeObj) => number} [weightFunc] - Optional function that returns
 * the weight for edge `e`. If no `weightFn` is supplied then each edge is
 * assumed to have a weight of 1.
 * @param {(v: NodeID) => EdgeObj[]} [edgeFunc] - Optional function that returns
 * the ids of all edges incident to the node `v` for the purposes of shortest
 * path traversal. By default this function uses the {@link Graph.outEdges}.
 * @returns {Record<NodeID, ReturnType<typeof dijkstra>>} a mapping of of
 * `source -> alg.dijksta(g, source, weightFn, edgeFn)`.
 * @throws {Error} If any of the traversed edges has a negative edge weight.
 */
export function dijkstraAll(g: Graph, weightFunc?: (e: EdgeObj) => number, edgeFunc?: (v: NodeID) => EdgeObj[]): Record<NodeID, ReturnType<typeof dijkstra>>;
import type { Graph } from '../graph.js';
import type { EdgeObj } from '../graph.js';
import type { NodeID } from '../graph.js';
import { dijkstra } from './dijkstra.js';
