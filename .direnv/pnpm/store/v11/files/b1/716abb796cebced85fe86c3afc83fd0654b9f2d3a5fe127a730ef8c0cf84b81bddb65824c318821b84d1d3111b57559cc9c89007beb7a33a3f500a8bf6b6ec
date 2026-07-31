/**
 * [Prim's algorithm][] takes a connected undirected graph and generates a
 * [minimum spanning tree][]. This function returns the minimum spanning
 * tree as an undirected graph. This algorithm is derived from the description
 * in "Introduction to Algorithms", Third Edition, Cormen, et al., Pg 634.
 *
 * [Prim's algorithm]: https://en.wikipedia.org/wiki/Prim's_algorithm
 * [minimum spanning tree]: https://en.wikipedia.org/wiki/Minimum_spanning_tree
 *
 * @remarks This function takes `O(|E| log |V|)` time.
 *
 * @example
 *
 * ![](https://github.com/dagrejs/graphlib/wiki/images/prim-input.png)
 * <!-- SOURCE:
 * digraph {
 * node [shape=circle, style="fill:white;stroke:#333;stroke-width:1.5px"]
 * edge [labeloffset=2 labelpos=r arrowhead="none"]
 * rankdir=lr
 * A -> B [label=3]
 * A -> D [label=12]
 * B -> C [label=6]
 * B -> D [label=1]
 * C -> D [label=1]
 * D -> E [label=2]
 * C -> E [label=9]
 * }
 * -->
 *
 * ```js
 * function weight(e) { return g(e); }
 * graphlib.alg.prim(g, weight);
 * ```
 *
 * Returns a tree (represented as a Graph) of the following form:
 *
 * ![](https://github.com/dagrejs/graphlib/wiki/images/prim-output.png)
 * <!-- SOURCE:
 * digraph {
 * node [shape=circle, style="fill:white;stroke:#333;stroke-width:1.5px"]
 * edge [labeloffset=2 labelpos=r arrowhead="none"]
 * rankdir=lr
 * A -> B
 * B -> D
 * C -> D
 * D -> E
 * }
 * -->
 *
 * @param {Graph} g - The input undirected connected graph.
 * @param {(e: EdgeObj) => number} weightFunc - Function that returns
 * the weight for edge `e`.
 * @returns {Graph<undefined, undefined, undefined>} The minimum spanning tree
 * as an undirected graph.
 * @throws {Error} If the input graph is not connected.
 */
export function prim(g: Graph, weightFunc: (e: EdgeObj) => number): Graph<undefined, undefined, undefined>;
import { Graph } from '../graph.js';
import type { EdgeObj } from '../graph.js';
