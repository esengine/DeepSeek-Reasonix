import * as _ from 'lodash-es';
import { PriorityQueue } from '../data/priority-queue.js';

/**
 * @import { EdgeObj, Graph, NodeID } from '../graph.js';
 */

export { dijkstra };

var DEFAULT_WEIGHT_FUNC = _.constant(1);

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
function dijkstra(g, source, weightFn, edgeFn) {
  return runDijkstra(
    g,
    String(source),
    weightFn || DEFAULT_WEIGHT_FUNC,
    edgeFn ||
      function (v) {
        return g.outEdges(v);
      },
  );
}

/**
 * @param {Graph} g - Input graph.
 * @param {NodeID} source - The source node id.
 * @param {(e: EdgeObj) => number} weightFn - Required weight function.
 * @param {(v: NodeID) => EdgeObj[]} edgeFn - Required edge function.
 */
function runDijkstra(g, source, weightFn, edgeFn) {
  /** @type {Record<NodeID, PathEntry>} */
  var results = {};
  var pq = new PriorityQueue();
  /** @type {NodeID} */
  var v;
  /** @type {PathEntry} */
  var vEntry;

  /** @param {EdgeObj} edge */
  var updateNeighbors = function (edge) {
    var w = edge.v !== v ? edge.v : edge.w;
    var wEntry = results[w];
    var weight = weightFn(edge);
    var distance = vEntry.distance + weight;

    if (weight < 0) {
      throw new Error(
        'dijkstra does not allow negative edge weights. ' +
          'Bad edge: ' +
          edge +
          ' Weight: ' +
          weight,
      );
    }

    if (distance < wEntry.distance) {
      wEntry.distance = distance;
      wEntry.predecessor = v;
      pq.decrease(w, distance);
    }
  };

  g.nodes().forEach(function (v) {
    var distance = v === source ? 0 : Number.POSITIVE_INFINITY;
    results[v] = { distance: distance };
    pq.add(v, distance);
  });

  while (pq.size() > 0) {
    v = pq.removeMin();
    vEntry = results[v];
    if (vEntry.distance === Number.POSITIVE_INFINITY) {
      break;
    }

    edgeFn(v).forEach(updateNeighbors);
  }

  return results;
}
