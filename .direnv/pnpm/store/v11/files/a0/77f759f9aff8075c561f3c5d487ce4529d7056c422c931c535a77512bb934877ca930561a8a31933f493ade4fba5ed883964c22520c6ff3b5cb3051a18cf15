import * as _ from 'lodash-es';

/**
 * @import { Graph, NodeID } from '../graph.js';
 */

export { dfs };

/**
 * A helper that preforms a pre- or post-order traversal on the input graph
 * and returns the nodes in the order they were visited. If the graph is
 * undirected then this algorithm will navigate using neighbors. If the graph
 * is directed then this algorithm will navigate using successors.
 *
 * @param {Graph} g - Input graph.
 * @param {NodeID[] | NodeID} vs - Starting node or array of nodes.
 * @param {'post' | 'pre'} order - The order to use. Must be one of "pre" or "post".
 * @returns {NodeID[]} The nodes in the order they were visited.
 */
function dfs(g, vs, order) {
  if (!_.isArray(vs)) {
    vs = [vs];
  }

  /** @type {Parameters<typeof doDfs>[4]} */
  var navigation = (g.isDirected() ? g.successors : g.neighbors).bind(g);
  /** @type {Parameters<typeof doDfs>[5]} */
  var acc = [];
  /** @type {Parameters<typeof doDfs>[3]} */
  var visited = {};
  _.each(vs, function (v) {
    if (!g.hasNode(v)) {
      throw new Error('Graph does not have node: ' + v);
    }

    doDfs(g, v, order === 'post', visited, navigation, acc);
  });
  return acc;
}

/**
 * @param {Graph} g - Input graph.
 * @param {NodeID} v - The node to visit.
 * @param {boolean} postorder - Whether to do postorder traversal.
 * @param {Record<NodeID, true>} visited - Visited nodes.
 * @param {(node: NodeID) => (NodeID[] | undefined)} navigation - Function to get
 * neighbors/successors.
 * @param {NodeID[]} acc - Accumulator for visited nodes.
 */
function doDfs(g, v, postorder, visited, navigation, acc) {
  if (!Object.prototype.hasOwnProperty.call(visited, v)) {
    visited[v] = true;

    if (!postorder) {
      acc.push(v);
    }
    _.each(navigation(v), function (w) {
      doDfs(g, w, postorder, visited, navigation, acc);
    });
    if (postorder) {
      acc.push(v);
    }
  }
}
