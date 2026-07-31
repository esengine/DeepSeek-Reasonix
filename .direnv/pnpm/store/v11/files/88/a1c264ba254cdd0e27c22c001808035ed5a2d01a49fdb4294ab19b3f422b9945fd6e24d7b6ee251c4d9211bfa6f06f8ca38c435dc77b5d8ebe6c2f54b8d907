import * as _ from 'lodash-es';

/**
 * @import { Graph, NodeID } from '../graph.js';
 */

export { topsort, CycleException };

topsort.CycleException = CycleException;

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
function topsort(g) {
  /** @type {Record<NodeID, true>} */
  var visited = {};
  /** @type {Record<NodeID, true>} */
  var stack = {};
  /** @type {NodeID[]} */
  var results = [];

  /**
   * @param {NodeID} node - Node to recursively visit.
   */
  function visit(node) {
    if (Object.prototype.hasOwnProperty.call(stack, node)) {
      throw new CycleException();
    }

    if (!Object.prototype.hasOwnProperty.call(visited, node)) {
      stack[node] = true;
      visited[node] = true;
      _.each(g.predecessors(node), visit);
      delete stack[node];
      results.push(node);
    }
  }

  _.each(g.sinks(), visit);

  if (_.size(visited) !== g.nodeCount()) {
    throw new CycleException();
  }

  return results;
}

/**
 * @class
 */
function CycleException() {}
CycleException.prototype = new Error(); // must be an instance of Error to pass testing
