/**
 * This function is an implementation of [Tarjan's algorithm][] which finds
 * all [strongly connected components][] in the directed graph `g`. Each
 * strongly connected component is composed of nodes that can reach all other
 * nodes in the component via directed edges. A strongly connected component
 * can consist of a single node if that node cannot both reach and be reached
 * by any other specific node in the graph. Components of more than one node
 * are guaranteed to have at least one cycle.
 *
 * [Tarjan's algorithm]: http://en.wikipedia.org/wiki/Tarjan's_strongly_connected_components_algorithm
 * [strongly connected components]: http://en.wikipedia.org/wiki/Strongly_connected_component
 *
 * @example
 *
 * ![](https://github.com/dagrejs/graphlib/wiki/images/tarjan.png)
 * <!-- SOURCE:
 * digraph {
 * node [shape=circle, style="fill:white;stroke:#333;stroke-width:1.5px"]
 * edge [lineInterpolate=bundle]
 * rankdir=lr
 *
 * A -> B -> C -> D -> H -> G -> F
 * F -> G
 * D -> C
 * H -> D
 * B -> E -> G
 * E -> A
 * }
 * -->
 *
 * ```js
 * graphlib.alg.tarjan(g);
 * // => [ [ 'F', 'G' ],
 * //      [ 'H', 'D', 'C' ],
 * //      [ 'E', 'B', 'A' ] ]
 * ```
 *
 * @param {Graph} g - The directed graph to analyze.
 * @returns {NodeID[][]} an array of components. Each component is itself an
 * array that contains the ids of all nodes in the component.
 */
export function tarjan(g: Graph): NodeID[][];
import type { Graph } from '../graph.js';
import type { NodeID } from '../graph.js';
