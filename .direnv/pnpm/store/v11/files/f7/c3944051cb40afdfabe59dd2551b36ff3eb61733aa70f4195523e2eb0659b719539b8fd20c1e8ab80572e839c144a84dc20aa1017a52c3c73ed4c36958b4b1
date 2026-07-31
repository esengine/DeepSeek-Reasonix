import * as _ from 'lodash-es';
import { Graph } from './graph.js';

/**
 * @import { NodeID, EdgeObj, GraphOptions } from './graph.js';
 */

export { write, read };

/**
 * @template [GraphLabel=any] - Label of the graph.
 * @template [NodeLabel=any] - Label of a node.
 * @template [EdgeLabel=any] - Label of an edge.
 *
 * @typedef {object} GraphJSON
 * @property {Required<GraphOptions>} options - The options used to create the graph.
 * @property {Array<{ v: NodeID; value?: NodeLabel; parent?: NodeID }>} nodes - The nodes in the graph.
 * @property {Array<EdgeObj & { value?: EdgeLabel }>} edges - The edges in the graph.
 * @property {GraphLabel} [value] - The graph's value, if any.
 */

/**
 * Creates a JSON representation of the graph that can be serialized to a
 * string with
 * [JSON.stringify](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/JSON/stringify).
 * The graph can later be restored using {@link read}.
 *
 * @example
 *
 * ```js
 * var g = new graphlib.Graph();
 * g.setNode("a", { label: "node a" });
 * g.setNode("b", { label: "node b" });
 * g.setEdge("a", "b", { label: "edge a->b" });
 * graphlib.json.write(g);
 * // Returns the object:
 * //
 * // {
 * //   "options": {
 * //     "directed": true,
 * //     "multigraph": false,
 * //     "compound": false
 * //   },
 * //   "nodes": [
 * //     { "v": "a", "value": { "label": "node a" } },
 * //     { "v": "b", "value": { "label": "node b" } }
 * //   ],
 * //   "edges": [
 * //     { "v": "a", "w": "b", "value": { "label": "edge a->b" } }
 * //   ]
 * // }
 * ```
 *
 * @template [GraphLabel=any] - Label of the graph.
 * @template [NodeLabel=any] - Label of a node.
 * @template [EdgeLabel=any] - Label of an edge.
 * @param {Graph<GraphLabel, NodeLabel, EdgeLabel>} g - The graph to serialize.
 * @returns {GraphJSON<GraphLabel, NodeLabel, EdgeLabel>} The JSON representation of the graph.
 */
function write(g) {
  /** @type {GraphJSON<GraphLabel, NodeLabel, EdgeLabel>} */
  var json = {
    options: {
      directed: g.isDirected(),
      multigraph: g.isMultigraph(),
      compound: g.isCompound(),
    },
    nodes: writeNodes(g),
    edges: writeEdges(g),
  };
  if (!_.isUndefined(g.graph())) {
    json.value = _.clone(g.graph());
  }
  return json;
}

/**
 * @template NodeLabel - Label of a node.
 *
 * @param {Graph<unknown, NodeLabel, unknown>} g - The graph to serialize.
 * @returns {Array<{ v: NodeID; value?: NodeLabel; parent?: NodeID }>} The nodes in the graph.
 */
function writeNodes(g) {
  return _.map(g.nodes(), function (v) {
    var nodeValue = g.node(v);
    var parent = g.parent(v);
    /** @type {{ v: NodeID; value?: NodeLabel; parent?: NodeID }} */
    var node = { v: v };
    if (!_.isUndefined(nodeValue)) {
      node.value = nodeValue;
    }
    if (!_.isUndefined(parent)) {
      node.parent = parent;
    }
    return node;
  });
}

/**
 * @template EdgeLabel - Label of a node.
 *
 * @param {Graph<unknown, unknown, EdgeLabel>} g - The graph to serialize.
 * @returns {Array<EdgeObj & { value?: EdgeLabel }>} The edges in the graph.
 */
function writeEdges(g) {
  return _.map(g.edges(), function (e) {
    var edgeValue = g.edge(e);
    /** @type {EdgeObj & { value?: EdgeLabel }} */
    var edge = { v: e.v, w: e.w };
    if (!_.isUndefined(e.name)) {
      edge.name = e.name;
    }
    if (!_.isUndefined(edgeValue)) {
      edge.value = edgeValue;
    }
    return edge;
  });
}

/**
 * Takes JSON as input and returns the graph representation.
 *
 * @example
 *
 * For example, if we have serialized the graph in {@link write}
 * to a string named `str`, we can restore it to a graph as follows:
 *
 * ```js
 * var g2 = graphlib.json.read(JSON.parse(str));
 * // or, in order to copy the graph
 * var g3 = graphlib.json.read(graphlib.json.write(g))
 *
 * g2.nodes();
 * // ['a', 'b']
 * g2.edges()
 * // [ { v: 'a', w: 'b' } ]
 * ```
 *
 * @template [GraphLabel=any] - Label of the graph.
 * @template [NodeLabel=any] - Label of a node.
 * @template [EdgeLabel=any] - Label of an edge.
 * @param {GraphJSON<GraphLabel, NodeLabel, EdgeLabel>} json - The JSON representation of the graph.
 * @returns {Graph<GraphLabel, NodeLabel, EdgeLabel>} The restored graph.
 */
function read(json) {
  var g = new Graph(json.options).setGraph(json.value);
  _.each(json.nodes, function (entry) {
    g.setNode(entry.v, entry.value);
    if (entry.parent) {
      g.setParent(entry.v, entry.parent);
    }
  });
  _.each(json.edges, function (entry) {
    g.setEdge({ v: entry.v, w: entry.w, name: entry.name }, entry.value);
  });
  return g;
}
