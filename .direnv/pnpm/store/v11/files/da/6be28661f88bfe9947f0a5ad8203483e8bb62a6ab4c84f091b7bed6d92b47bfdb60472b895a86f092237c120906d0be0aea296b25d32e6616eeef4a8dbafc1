import * as _ from 'lodash-es';
import { PriorityQueue } from '../data/priority-queue.js';
import { Graph } from '../graph.js';

/**
 * @import { EdgeObj, NodeID } from '../graph.js';
 */

export { prim };

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
function prim(g, weightFunc) {
  var result = new Graph();
  /** @type {Record<NodeID, NodeID>} */
  var parents = {};
  var pq = new PriorityQueue();
  /** @type {NodeID} */
  var v;

  /**
   * @param {EdgeObj} edge - Edge to examine for possible inclusion in the
   * minimum spanning tree.
   */
  function updateNeighbors(edge) {
    var w = edge.v === v ? edge.w : edge.v;
    var pri = pq.priority(w);
    if (pri !== undefined) {
      var edgeWeight = weightFunc(edge);
      if (edgeWeight < pri) {
        parents[w] = v;
        pq.decrease(w, edgeWeight);
      }
    }
  }

  if (g.nodeCount() === 0) {
    return result;
  }

  _.each(g.nodes(), function (v) {
    pq.add(v, Number.POSITIVE_INFINITY);
    result.setNode(v);
  });

  // Start from an arbitrary node
  pq.decrease(g.nodes()[0], 0);

  var init = false;
  while (pq.size() > 0) {
    v = pq.removeMin();
    if (Object.prototype.hasOwnProperty.call(parents, v)) {
      result.setEdge(v, parents[v]);
    } else if (init) {
      throw new Error('Input graph is not connected: ' + g);
    } else {
      init = true;
    }

    g.nodeEdges(v).forEach(updateNeighbors);
  }

  return result;
}
