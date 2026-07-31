import * as _ from 'lodash-es';

/**
 * @import { Graph, EdgeObj, NodeID } from '../graph.js';
 * @import { PathEntry } from './dijkstra.js';
 */

export { floydWarshall };

var DEFAULT_WEIGHT_FUNC = _.constant(1);

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
function floydWarshall(g, weightFn, edgeFn) {
  return runFloydWarshall(
    g,
    weightFn || DEFAULT_WEIGHT_FUNC,
    edgeFn ||
      function (v) {
        return g.outEdges(v);
      },
  );
}

/**
 * @param {Graph} g - Input graph.
 * @param {(e: EdgeObj) => number} weightFn - Required weight function.
 * @param {(v: NodeID) => EdgeObj[]} edgeFn - Required edge function.
 */
function runFloydWarshall(g, weightFn, edgeFn) {
  /** @type {Record<NodeID, Record<NodeID, PathEntry>>} */
  var results = {};
  var nodes = g.nodes();

  nodes.forEach(function (v) {
    results[v] = {};
    results[v][v] = { distance: 0 };
    nodes.forEach(function (w) {
      if (v !== w) {
        results[v][w] = { distance: Number.POSITIVE_INFINITY };
      }
    });
    edgeFn(v).forEach(function (edge) {
      var w = edge.v === v ? edge.w : edge.v;
      var d = weightFn(edge);
      results[v][w] = { distance: d, predecessor: v };
    });
  });

  nodes.forEach(function (k) {
    var rowK = results[k];
    nodes.forEach(function (i) {
      var rowI = results[i];
      nodes.forEach(function (j) {
        var ik = rowI[k];
        var kj = rowK[j];
        var ij = rowI[j];
        var altDistance = ik.distance + kj.distance;
        if (altDistance < ij.distance) {
          ij.distance = altDistance;
          ij.predecessor = kj.predecessor;
        }
      });
    });
  });

  return results;
}
