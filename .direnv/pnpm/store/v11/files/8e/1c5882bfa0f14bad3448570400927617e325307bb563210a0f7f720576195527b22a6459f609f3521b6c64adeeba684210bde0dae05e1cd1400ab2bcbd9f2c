/**
 * Given a Graph, `g`, this function returns all nodes that
 * are part of a cycle. As there may be more than one cycle in a graph this
 * function return an array of these cycles, where each cycle is itself
 * represented by an array of ids for each node involved in that cycle.
 *
 * @remarks
 *
 * {@link isAcyclic} is more efficient if you only need to
 * determine whether a graph has a cycle or not.
 *
 * @example
 *
 * ```js
 * var g = new graphlib.Graph();
 * g.setNode(1);
 * g.setNode(2);
 * g.setNode(3);
 * g.setEdge(1, 2);
 * g.setEdge(2, 3);
 *
 * graphlib.alg.findCycles(g);
 * // => []
 *
 * g.setEdge(3, 1);
 * graphlib.alg.findCycles(g);
 * // => [ [ '3', '2', '1' ] ]
 *
 * g.setNode(4);
 * g.setNode(5);
 * g.setEdge(4, 5);
 * g.setEdge(5, 4);
 * graphlib.alg.findCycles(g);
 * // => [ [ '3', '2', '1' ], [ '5', '4' ] ]
 * ```
 *
 * @param {Graph} g - The graph to analyze.
 * @returns {NodeID[][]} An array of cycles. Each cycle is itself an array
 * that contains the ids of all nodes in the cycle.
 */
export function findCycles(g: Graph): NodeID[][];
import type { Graph } from '../graph.js';
import type { NodeID } from '../graph.js';
