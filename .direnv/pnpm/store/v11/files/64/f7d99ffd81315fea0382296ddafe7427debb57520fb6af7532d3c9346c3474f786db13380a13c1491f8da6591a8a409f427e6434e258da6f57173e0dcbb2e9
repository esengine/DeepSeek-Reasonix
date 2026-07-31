export type GraphJSON<GraphLabel = any, NodeLabel = any, EdgeLabel = any> = {
    /**
     * - The options used to create the graph.
     */
    options: Required<GraphOptions>;
    /**
     * - The nodes in the graph.
     */
    nodes: Array<{
        v: NodeID;
        value?: NodeLabel;
        parent?: NodeID;
    }>;
    /**
     * - The edges in the graph.
     */
    edges: Array<EdgeObj & {
        value?: EdgeLabel;
    }>;
    /**
     * - The graph's value, if any.
     */
    value?: GraphLabel;
};
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
export function write<GraphLabel = any, NodeLabel = any, EdgeLabel = any>(g: Graph<GraphLabel, NodeLabel, EdgeLabel>): GraphJSON<GraphLabel, NodeLabel, EdgeLabel>;
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
export function read<GraphLabel = any, NodeLabel = any, EdgeLabel = any>(json: GraphJSON<GraphLabel, NodeLabel, EdgeLabel>): Graph<GraphLabel, NodeLabel, EdgeLabel>;
import type { GraphOptions } from './graph.js';
import type { NodeID } from './graph.js';
import type { EdgeObj } from './graph.js';
import { Graph } from './graph.js';
