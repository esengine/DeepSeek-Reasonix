export type PathEntry = {
    /**
     * The sum of the weights from `source` to `v`
     * along the shortest path or `Number.POSITIVE_INFINITY` if there is no path
     * from `source`.
     */
    distance: number;
    /**
     * Can be used to walk the individual
     * elements of the path from `source` to `v` in reverse order.
     */
    predecessor?: NodeID;
};
/**
 * @typedef {Object} PathEntry
 * @property {number} distance The sum of the weights from `source` to `v`
 * along the shortest path or `Number.POSITIVE_INFINITY` if there is no path
 * from `source`.
 * @property {NodeID} [predecessor] Can be used to walk the individual
 * elements of the path from `source` to `v` in reverse order.
 */
/**
 * This function is an implementation of [Dijkstra's algorithm][] which finds
 * the shortest path from `source` to all other nodes in `g`. This
 * function returns
 *
 * [Dijkstra's algorithm]: http://en.wikipedia.org/wiki/Dijkstra%27s_algorithm
 *
 * @example
 *
 * ![](https://github.com/dagrejs/graphlib/wiki/images/dijkstra-source.png)
 * <!-- SOURCE:
 * http://dagrejs.github.io/project/dagre-d3/latest/demo/interactive-demo.html?graph=digraph%20%7B%0Anode%20%5Bshape%3Dcircle%2C%20style%3D%22fill%3Awhite%3Bstroke%3A%23333%3Bstroke-width%3A1.5px%22%5D%0Aedge%20%5Blabeloffset%3D2%20labelpos%3Dr%5D%0Arankdir%3Dlr%0A%20%20A%20-%3E%20B%5Blabel%3D10%5D%0A%20%20A%20-%3E%20C%5Blabel%3D4%5D%0A%20%20A%20-%3E%20D%5Blabel%3D2%5D%0A%20%20C%20-%3E%20B%5Blabel%3D2%5D%0A%20%20C%20-%3E%20D%5Blabel%3D8%5D%0A%20%20B%20-%3E%20E%5Blabel%3D6%5D%0A%20%20D%20-%3E%20F%5Blabel%3D2%5D%0A%20%20F%20-%3E%20E%5Blabel%3D4%5D%0A%7D
 * -->
 *
 * ```js
 * function weight(e) { return g.edge(e); }
 *
 * graphlib.alg.dijkstra(g, "A", weight);
 * // => { A: { distance: 0 },
 * //      B: { distance: 6, predecessor: 'C' },
 * //      C: { distance: 4, predecessor: 'A' },
 * //      D: { distance: 2, predecessor: 'A' },
 * //      E: { distance: 8, predecessor: 'F' },
 * //      F: { distance: 4, predecessor: 'D' } }
 * ```
 *
 * @remarks It takes `O((|E| + |V|) * log |V|)` time.
 *
 * @param {Graph} g - Input graph.
 * @param {NodeID | number} source - The source node id. Converted to a string.
 * @param {(e: EdgeObj) => number} [weightFn] - Optional function that returns
 * the weight for edge `e`. If no `weightFn` is supplied then each edge is
 * assumed to have a weight of 1.
 * @param {(v: NodeID) => EdgeObj[]} [edgeFn] - Optional function that returns
 * the ids of all edges incident to the node `v` for the purposes of shortest
 * path traversal.
 * By default this function uses the {@link Graph.outEdges} function on the
 * supplied graph.
 * @returns {Record<NodeID, PathEntry>} a map of `v -> { distance, predecessor }`.
 * @throws {Error} If any of the traversed edges has a negative edge weight.
 */
export function dijkstra(g: Graph, source: NodeID | number, weightFn?: (e: EdgeObj) => number, edgeFn?: (v: NodeID) => EdgeObj[]): Record<NodeID, PathEntry>;
import type { NodeID } from '../graph.js';
import type { Graph } from '../graph.js';
import type { EdgeObj } from '../graph.js';
