import * as _ from 'lodash-es';

/**
 * @import { Graph, NodeID } from '../graph.js';
 */

export { components };

/**
 * Finds all [connected components][] in a graph and returns an array of these
 * components. Each component is itself an array that contains the ids of nodes
 * in the component.
 *
 * [connected components]: http://en.wikipedia.org/wiki/Connected_component_(graph_theory)
 *
 * @example
 *
 * ![](https://github.com/dagrejs/graphlib/wiki/images/components.png)
 *
 * ```js
 * graphlib.alg.components(g);
 * // => [ [ 'A', 'B', 'C', 'D' ],
 * //      [ 'E', 'F', 'G' ],
 * //      [ 'H', 'I' ] ]
 * ```
 *
 * @param {Graph} g - The graph to find components in.
 * @returns {NodeID[][]} An array of components, each of which is an array of node IDs.
 *
 * @remarks This function takes `O(|V|)` time.
 */
function components(g) {
  /**
   * @type {Record<NodeID, true>}
   */
  var visited = {};
  /**
   * @type {NodeID[][]}
   */
  var cmpts = [];
  /**
   * @type {NodeID[]}
   */
  var cmpt;

  /**
   * @param {NodeID} v - The node to visit.
   */
  function dfs(v) {
    if (Object.prototype.hasOwnProperty.call(visited, v)) return;
    visited[v] = true;
    cmpt.push(v);
    _.each(g.successors(v), dfs);
    _.each(g.predecessors(v), dfs);
  }

  _.each(g.nodes(), function (v) {
    cmpt = [];
    dfs(v);
    if (cmpt.length) {
      cmpts.push(cmpt);
    }
  });

  return cmpts;
}
