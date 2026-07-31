/**
 * @typedef {string} NodeID ID of a node.
 */
/**
 * @typedef {`${string}${typeof EDGE_KEY_DELIM}${string}${typeof EDGE_KEY_DELIM}${string}`} EdgeID ID of an edge.
 * @internal - All public APIs use {@link EdgeObj} instead to refer to edges.
 */
/**
 * @typedef {object} EdgeObj
 * @property {NodeID} v the id of the source or tail node of an edge
 * @property {NodeID} w the id of the target or head node of an edge
 * @property {string | number} [name] Name of the edge. Needed to uniquely identify
 * multiple edges between the same pair of nodes in a multigraph.
 */
/**
 * @template {unknown} T
 * @typedef {T[] | Record<any, T>} Collection
 * Lodash object that can be iterated over with `_.each`.
 *
 * Beware, objects with `.length` are treated as arrays, see
 * https://lodash.com/docs/4.17.15#forEach
 */
/**
 * @typedef {object} GraphOptions
 * @property {boolean | undefined} [directed] - set to `true` to get a
 * directed graph and `false` to get an undirected graph.
 * An undirected graph does not treat the order of nodes in an edge as
 * significant.
 * In other words, `g.edge("a", "b") === g.edge("b", "a")` for
 * an undirected graph.
 * Default: `true`
 * @property {boolean | undefined} [multigraph] - set to `true` to allow a
 * graph to have multiple edges between the same pair of nodes.
 * Default: `false`.
 * @property {boolean | undefined} [compound] - set to `true` to allow a
 * graph to have compound nodes - nodes which can be the parent of other
 * nodes.
 * Default: `false`.
 */
/**
 * Graphlib has a single graph type: {@link Graph}. To create a new instance:
 *
 * ```js
 * var g = new Graph();
 * ```
 *
 * By default this will create a directed graph that does not allow multi-edges
 * or compound nodes.
 * The following options can be used when constructing a new graph:
 *
 * * {@link GraphOptions#directed}: set to `true` to get a directed graph and `false` to get an
 *   undirected graph.
 *   An undirected graph does not treat the order of nodes in an edge as
 *   significant. In other words,
 *   `g.edge("a", "b") === g.edge("b", "a")` for an undirected graph.
 *   Default: `true`.
 * * {@link GraphOptions#multigraph}: set to `true` to allow a graph to have multiple edges
 *   between the same pair of nodes. Default: `false`.
 * * {@link GraphOptions#compound}: set to `true` to allow a graph to have compound nodes -
 *   nodes which can be the parent of other nodes. Default: `false`.
 *
 * To set the options, pass in an options object to the `Graph` constructor.
 * For example, to create a directed compound multigraph:
 *
 * ```js
 * var g = new Graph({ directed: true, compound: true, multigraph: true });
 * ```
 *
 * ### Node and Edge Representation
 *
 * In graphlib, a node is represented by a user-supplied String id.
 * All node related functions use this String id as a way to uniquely identify
 * the node. Here is an example of interacting with nodes:
 *
 * ```js
 * var g = new Graph();
 * g.setNode("my-id", "my-label");
 * g.node("my-id"); // returns "my-label"
 * ```
 *
 * Edges in graphlib are identified by the nodes they connect. For example:
 *
 * ```js
 * var g = new Graph();
 * g.setEdge("source", "target", "my-label");
 * g.edge("source", "target"); // returns "my-label"
 * ```
 *
 * However, we need a way to uniquely identify an edge in a single object for
 * various edge queries (e.g. {@link Graph#outEdges}).
 * We use {@link EdgeObj}s for this purpose.
 * They consist of the following properties:
 *
 * * {@link EdgeObj#v}: the id of the source or tail node of an edge
 * * {@link EdgeObj#w}: the id of the target or head node of an edge
 * * {@link EdgeObj#name} (optional): the name that uniquely identifies a multiedge.
 *
 * Any edge function that takes an edge id will also work with an {@link EdgeObj}. For example:
 *
 * ```js
 * var g = new Graph();
 * g.setEdge("source", "target", "my-label");
 * g.edge({ v: "source", w: "target" }); // returns "my-label"
 * ```
 *
 * ### Multigraphs
 *
 * A [multigraph](https://en.wikipedia.org/wiki/Multigraph) is a graph that can
 * have more than one edge between the same pair of nodes.
 * By default graphlib graphs are not multigraphs, but a multigraph can be
 * constructed by setting the {@link GraphOptions#multigraph} property to true:
 *
 * ```js
 * var g = new Graph({ multigraph: true });
 * ```
 *
 * With multiple edges between two nodes we need some way to uniquely identify
 * each edge. We call this the {@link EdgeObj#name} property.
 * Here's an example of creating a couple of edges between the same nodes:
 *
 * ```js
 * var g = new Graph({ multigraph: true });
 * g.setEdge("a", "b", "edge1-label", "edge1");
 * g.setEdge("a", "b", "edge2-label", "edge2");
 * g.edge("a", "b", "edge1"); // returns "edge1-label"
 * g.edge("a", "b", "edge2"); // returns "edge2-label"
 * g.edges(); // returns [{ v: "a", w: "b", name: "edge1" },
 *            //          { v: "a", w: "b", name: "edge2" }]
 * ```
 *
 * A multigraph still allows an edge with no name to be created:
 *
 * ```js
 * var g = new Graph({ multigraph: true });
 * g.setEdge("a", "b", "my-label");
 * g.edge({ v: "a", w: "b" }); // returns "my-label"
 * ```
 *
 * ### Compound Graphs
 *
 * A compound graph is one where a node can be the parent of other nodes.
 * The child nodes form a "subgraph".
 * Here's an example of constructing and interacting with a compound graph:
 *
 * ```js
 * var g = new Graph({ compound: true });
 * g.setParent("a", "parent");
 * g.setParent("b", "parent");
 * g.parent("a");      // returns "parent"
 * g.parent("b");      // returns "parent"
 * g.parent("parent"); // returns undefined
 * ```
 *
 * ### Default Labels
 *
 * When a node or edge is created without a label, a default label can be assigned.
 * See {@link setDefaultNodeLabel} and {@link setDefaultEdgeLabel}.
 *
 * @template [GraphLabel=any] - Label of the graph.
 * @template [NodeLabel=any] - Label of a node.
 * Even though this is a "label", this could be any type that the user requires
 * (and may need to be an object for some layout/ranking algorithms in dagre).
 * @template [EdgeLabel=any] - Label of an edge.
 * Even though this is a "label", this could be any type that the user requires,
 * (and may need to be a object for ranking in dagre).
 */
export class Graph<GraphLabel = any, NodeLabel = any, EdgeLabel = any> {
    /**
     * @param {GraphOptions} [opts] - Graph options.
     */
    constructor(opts?: GraphOptions);
    /**
     * @type {boolean}
     * @private
     */
    private _isDirected;
    /**
     * @type {boolean}
     * @private
     */
    private _isMultigraph;
    /**
     * @type {boolean}
     * @private
     */
    private _isCompound;
    /**
     * @type {GraphLabel | undefined}
     * Label for the graph itself
     */
    _label: GraphLabel | undefined;
    /**
     * Default label to be set when creating a new node.
     *
     * @private
     * @type {(v: NodeID | number) => NodeLabel}
     */
    private _defaultNodeLabelFn;
    /**
     * Default label to be set when creating a new edge
     *
     * @private
     * @type {(v: NodeID, w: NodeID, name: string | undefined) => EdgeLabel}
     */
    private _defaultEdgeLabelFn;
    /**
     * @type {Record<NodeID, NodeLabel>}
     * @private
     *
     * v -> label
     */
    private _nodes;
    /**
     * @type {Record<NodeID, NodeID>}
     * @private
     * v -> parent
     */
    private _parent;
    /**
     * @type {Record<NodeID, Record<NodeID, true>>}
     * @private
     * v -> children
     */
    private _children;
    /**
     * @type {Record<NodeID, Record<EdgeID, EdgeObj>>}
     * @private
     * v -> edgeObj
     */
    private _in;
    /**
     * @type {Record<NodeID, Record<NodeID, number>>}
     * @private
     * u -> v -> Number
     */
    private _preds;
    /**
     * @type {Record<NodeID, Record<EdgeID, EdgeObj>>}
     * @private
     * v -> edgeObj
     */
    private _out;
    /**
     * @type {Record<NodeID, Record<NodeID, number>>}
     * @private
     * v -> w -> Number
     */
    private _sucs;
    /**
     * @type {Record<EdgeID, EdgeObj>}
     * @private
     * e -> edgeObj
     */
    private _edgeObjs;
    /**
     * @type {Record<EdgeID, EdgeLabel>}
     * @private
     * e -> label
     */
    private _edgeLabels;
    /**
     *
     * @returns {boolean} `true` if the graph is [directed](https://en.wikipedia.org/wiki/Directed_graph).
     * A directed graph treats the order of nodes in an edge as significant whereas an
     * [undirected](https://en.wikipedia.org/wiki/Graph_(mathematics)#Undirected_graph)
     * graph does not.
     * This example demonstrates the difference:
     *
     * @example
     *
     * ```js
     * var directed = new Graph({ directed: true });
     * directed.setEdge("a", "b", "my-label");
     * directed.edge("a", "b"); // returns "my-label"
     * directed.edge("b", "a"); // returns undefined
     *
     * var undirected = new Graph({ directed: false });
     * undirected.setEdge("a", "b", "my-label");
     * undirected.edge("a", "b"); // returns "my-label"
     * undirected.edge("b", "a"); // returns "my-label"
     * ```
     */
    isDirected(): boolean;
    /**
     * @returns {boolean} `true` if the graph is a multigraph.
     */
    isMultigraph(): boolean;
    /**
     * @returns {boolean} `true` if the graph is compound.
     */
    isCompound(): boolean;
    /**
     * Sets the label for the graph to `label`.
     *
     * @param {GraphLabel} label - Label for the graph.
     * @returns {this}
     */
    setGraph(label: GraphLabel): this;
    /**
     * @returns {GraphLabel | undefined} the currently assigned label for the graph.
     * If no label has been assigned, returns `undefined`.
     *
     * @example
     *
     * ```js
     * var g = new Graph();
     * g.graph(); // returns undefined
     * g.setGraph("graph-label");
     *  g.graph(); // returns "graph-label"
     * ```
     */
    graph(): GraphLabel | undefined;
    /**
     * Sets a new default value that is assigned to nodes that are created without
     * a label.
     *
     * @param {typeof this._defaultNodeLabelFn | NodeLabel} newDefault - If a function,
     * it is called with the id of the node being created.
     * Otherwise, it is assigned as the label directly.
     * @returns {this}
     */
    setDefaultNodeLabel(newDefault: typeof this._defaultNodeLabelFn | NodeLabel): this;
    /**
     * @returns {number} the number of nodes in the graph.
     */
    nodeCount(): number;
    /**
     * @returns {NodeID[]} the ids of the nodes in the graph.
     *
     * @remarks
     * Use {@link node()} to get the label for each node.
     * Takes `O(|V|)` time.
     */
    nodes(): NodeID[];
    /**
     * @returns {NodeID[]} those nodes in the graph that have no in-edges.
     * @remarks Takes `O(|V|)` time.
     */
    sources(): NodeID[];
    /**
     * @returns {NodeID[]} those nodes in the graph that have no out-edges.
     * @remarks Takes `O(|V|)` time.
     */
    sinks(): NodeID[];
    /**
     * Invokes setNode method for each node in `vs` list.
     *
     * @param {Collection<NodeID | number>} vs - List of node IDs to create/set.
     * @param {NodeLabel} [value] - If set, update all nodes with this value.
     * @returns {this}
     * @remarks Complexity: O(|names|).
     */
    setNodes(vs: Collection<NodeID | number>, value?: NodeLabel, ...args: any[]): this;
    /**
     * Creates or updates the value for the node `v` in the graph.
     *
     * @param {NodeID | number} v - ID of the node to create/set.
     * @param {NodeLabel} [value] - If supplied, it is set as the value for the node.
     * If not supplied and the node was created by this call then
     * {@link setDefaultNodeLabel} will be used to set the node's value.
     * @returns {this} the graph, allowing this to be chained with other functions.
     * @remarks Takes `O(1)` time.
     */
    setNode(v: NodeID | number, value?: NodeLabel, ...args: any[]): this;
    /**
     * Gets the label of node with specified name.
     *
     * @param {NodeID | number} v - Node ID.
     * @returns {NodeLabel | undefined} the label assigned to the node with the id `v`
     * if it is in the graph.
     * Otherwise returns `undefined`.
     * @remarks Takes `O(1)` time.
     */
    node(v: NodeID | number): NodeLabel | undefined;
    /**
     * Detects whether graph has a node with specified name or not.
     *
     * @param {NodeID | number} v - Node ID.
     * @returns {boolean} Returns `true` the graph has a node with the id.
     * @remarks Takes `O(1)` time.
     */
    hasNode(v: NodeID | number): boolean;
    /**
     * Remove the node with the id `v` in the graph or do nothing if the node is
     * not in the graph.
     *
     * If the node was removed this function also removes any incident edges.
     *
     * @param {NodeID | number} v - Node ID to remove.
     * @returns {this} the graph, allowing this to be chained with other functions.
     * @remarks Takes `O(|E|)` time.
     */
    removeNode(v: NodeID | number): this;
    /**
     * Sets the parent for `v` to `parent` if it is defined or removes the parent
     * for `v` if `parent` is undefined.
     *
     * @param {NodeID | number} v - Node ID to set the parent for.
     * @param {NodeID | number} [parent] - Parent node ID. If not defined, removes the parent.
     * @returns {this} the graph, allowing this to be chained with other functions.
     * @throws if the graph is not compound.
     * @throws if setting the parent would create a cycle.
     * @remarks Takes `O(1)` time.
     */
    setParent(v: NodeID | number, parent?: NodeID | number): this;
    /**
     * @private
     * @param {NodeID | number} v - Node ID.
     */
    private _removeFromParentsChildList;
    /**
     * Get parent node for node `v`.
     *
     * @param {NodeID | number} v - Node ID.
     * @returns {NodeID | undefined} the node that is a parent of node `v`
     * or `undefined` if node `v` does not have a parent or is not a member of
     * the graph.
     * Always returns `undefined` for graphs that are not compound.
     * @remarks Takes `O(1)` time.
     */
    parent(v: NodeID | number): NodeID | undefined;
    /**
     * Gets list of direct children of node v.
     *
     * @param {NodeID | number} [v] - Node ID. If not specified, gets nodes
     * with no parent (top-level nodes).
     * @returns {NodeID[] | undefined} all nodes that are children of node `v` or
     * `undefined` if node `v` is not in the graph.
     * Always returns `[]` for graphs that are not compound.
     * @remarks Takes `O(|V|)` time.
     */
    children(v?: NodeID | number): NodeID[] | undefined;
    /**
     * @param {NodeID | number} v - Node ID.
     * @returns {NodeID[] | undefined} all nodes that are predecessors of the
     * specified node or `undefined` if node `v` is not in the graph.
     * @remarks
     * Behavior is undefined for undirected graphs - use {@link neighbors} instead.
     * Takes `O(|V|)` time.
     */
    predecessors(v: NodeID | number): NodeID[] | undefined;
    /**
     * @param {NodeID | number} v - Node ID.
     * @returns {NodeID[] | undefined} all nodes that are successors of the
     * specified node or `undefined` if node `v` is not in the graph.
     * @remarks
     * Behavior is undefined for undirected graphs - use {@link neighbors} instead.
     * Takes `O(|V|)` time.
     */
    successors(v: NodeID | number): NodeID[] | undefined;
    /**
     * @param {NodeID | number} v - Node ID.
     * @returns {NodeID[] | undefined} all nodes that are predecessors or
     * successors of the specified node
     * or `undefined` if node `v` is not in the graph.
     * @remarks Takes `O(|V|)` time.
     */
    neighbors(v: NodeID | number): NodeID[] | undefined;
    /**
     * @param {NodeID | number} v - Node ID.
     * @returns {boolean} True if the node is a leaf (has no successors), false otherwise.
     */
    isLeaf(v: NodeID | number): boolean;
    /**
     * Creates new graph with nodes filtered via `filter`.
     * Edges incident to rejected node
     * are also removed.
     *
     * In case of compound graph, if parent is rejected by `filter`,
     * than all its children are rejected too.
  
     * @param {(v: NodeID) => boolean} filter - Function that returns `true` for nodes to keep.
     * @returns {Graph<GraphLabel, NodeLabel, EdgeLabel>} A new graph containing only the nodes for which `filter` returns `true`.
     * @remarks Average-case complexity: O(|E|+|V|).
     */
    filterNodes(filter: (v: NodeID) => boolean): Graph<GraphLabel, NodeLabel, EdgeLabel>;
    /**
     * Sets a new default value that is assigned to edges that are created without
     * a label.
     *
     * @param {typeof this._defaultEdgeLabelFn | EdgeLabel} newDefault - If a function,
     * it is called with the parameters `(v, w, name)`.
     * Otherwise, it is assigned as the label directly.
     * @returns {this}
     */
    setDefaultEdgeLabel(newDefault: typeof this._defaultEdgeLabelFn | EdgeLabel): this;
    /**
     * @returns {number} the number of edges in the graph.
     * @remarks Complexity: O(1).
     */
    edgeCount(): number;
    /**
     * Gets edges of the graph.
     *
     * @returns {EdgeObj[]} the {@link EdgeObj} for each edge in the graph.
     *
     * @remarks
     * In case of compound graph subgraphs are not considered.
     * Use {@link edge()} to get the label for each edge.
     * Takes `O(|E|)` time.
     */
    edges(): EdgeObj[];
    /**
     * Establish an edges path over the nodes in nodes list.
     *
     * If some edge is already exists, it will update its label, otherwise it will
     * create an edge between pair of nodes with label provided or default label
     * if no label provided.
     *
     * @param {Collection<NodeID>} vs - List of node IDs to create edges between.
     * @param {EdgeLabel} [value] - If set, update all edges with this value.
     * @returns {this}
     * @remarks Complexity: O(|nodes|).
     */
    setPath(vs: Collection<NodeID>, value?: EdgeLabel, ...args: any[]): this;
    /**
     * Creates or updates the label for the edge (`v`, `w`) with the optionally
     * supplied `name`.
     *
     * @overload
     * @param {EdgeObj} arg0 - Edge object.
     * @param {EdgeLabel} [value] - If supplied, it is set as the label for the edge.
     * If not supplied and the edge was created by this call then
     * {@link setDefaultEdgeLabel} will be used to assign the edge's label.
     * @returns {this} the graph, allowing this to be chained with other functions.
     * @remarks Takes `O(1)` time.
     */
    setEdge(arg0: EdgeObj, value?: EdgeLabel): this;
    /**
     * Creates or updates the label for the edge (`v`, `w`) with the optionally
     * supplied `name`.
     *
     * @overload
     * @param {NodeID | number} v - Source node ID. Number values will be coerced to strings.
     * @param {NodeID | number} w - Target node ID. Number values will be coerced to strings.
     * @param {EdgeLabel} [value] - If supplied, it is set as the label for the edge.
     * If not supplied and the edge was created by this call then
     * {@link setDefaultEdgeLabel} will be used to assign the edge's label.
     * @param {string | number} [name] - Edge name. Only useful with multigraphs.
     * @returns {this} the graph, allowing this to be chained with other functions.
     * @remarks Takes `O(1)` time.
     */
    setEdge(v: NodeID | number, w: NodeID | number, value?: EdgeLabel, name?: string | number): this;
    /**
     * Gets the label for the specified edge.
     *
     * @overload
     * @param {EdgeObj} v - Edge object.
     * @returns {EdgeLabel | undefined} the label for the edge (`v`, `w`) if the
     * graph has an edge between `v` and `w` with the optional `name`.
     * Returned `undefined` if there is no such edge in the graph.
     * @remarks
     * `v` and `w` can be interchanged for undirected graphs.
     * Takes `O(1)` time.
     */
    edge(v: EdgeObj): EdgeLabel | undefined;
    /**
     * Gets the label for the specified edge.
     *
     * @overload
     * @param {NodeID | number} v - Source node ID.
     * @param {NodeID | number} w - Target node ID.
     * @param {string | number} [name] - Edge name. Only useful with multigraphs.
     * @returns {EdgeLabel | undefined} the label for the edge (`v`, `w`) if the
     * graph has an edge between `v` and `w` with the optional `name`.
     * Returned `undefined` if there is no such edge in the graph.
     * @remarks
     * `v` and `w` can be interchanged for undirected graphs.
     * Takes `O(1)` time.
     */
    edge(v: NodeID | number, w: NodeID | number, name?: string | number): EdgeLabel | undefined;
    /**
     * Detects whether the graph contains specified edge or not.
     *
     * @overload
     * @param {EdgeObj} v - Edge object.
     * @returns {boolean} `true` if the graph has an edge between `v` and `w`
     * with the optional `name`.
     * @remarks
     * `v` and `w` can be interchanged for undirected graphs.
     * No subgraphs are considered.
     * Takes `O(1)` time.
     */
    hasEdge(v: EdgeObj): boolean;
    /**
     * Detects whether the graph contains specified edge or not.
     *
     * @overload
     * @param {NodeID | number} v - Source node ID.
     * @param {NodeID | number} w - Target node ID.
     * @param {string | number} [name] - Edge name. Only useful with multigraphs.
     * @returns {boolean} `true` if the graph has an edge between `v` and `w`
     * with the optional `name`.
     * @remarks
     * `v` and `w` can be interchanged for undirected graphs.
     * No subgraphs are considered.
     * Takes `O(1)` time.
     */
    hasEdge(v: NodeID | number, w: NodeID | number, name?: string | number): boolean;
    /**
     * Removes the edge (`v`, `w`) if the graph has an edge between `v` and `w`
     * with the optional `name`. If not this function does nothing.
     *
     * @overload
     * @param {EdgeObj} v - Edge object.
     * @returns {this}
     * @remarks
     * `v` and `w` can be interchanged for undirected graphs.
     * No subgraphs are considered.
     * Takes `O(1)` time.
     */
    removeEdge(v: EdgeObj): this;
    /**
     * Removes the edge (`v`, `w`) if the graph has an edge between `v` and `w`
     * with the optional `name`. If not this function does nothing.
     *
     * @overload
     * @param {NodeID | number} v - Source node ID.
     * @param {NodeID | number} w - Target node ID.
     * @param {string | number} [name] - Edge name. Only useful with multigraphs.
     * @returns {this}
     * @remarks
     * `v` and `w` can be interchanged for undirected graphs.
     * Takes `O(1)` time.
     */
    removeEdge(v: NodeID | number, w: NodeID | number, name?: string | number): this;
    /**
     * @param {NodeID | number} v - Target node ID.
     * @param {NodeID | number} [u] - Optionally filters edges down to just those
     * coming from node `u`.
     * @returns {EdgeObj[] | undefined} all edges that point to the node `v`.
     * Returns `undefined` if node `v` is not in the graph.
     * @remarks
     * Behavior is undefined for undirected graphs - use {@link nodeEdges} instead.
     * Takes `O(|E|)` time.
     */
    inEdges(v: NodeID | number, u?: NodeID | number): EdgeObj[] | undefined;
    /**
     * @param {NodeID | number} v - Target node ID.
     * @param {NodeID | number} [w] - Optionally filters edges down to just those
     * that point to `w`.
     * @returns {EdgeObj[] | undefined} all edges that point to the node `v`.
     * Returns `undefined` if node `v` is not in the graph.
     * @remarks
     * Behavior is undefined for undirected graphs - use {@link nodeEdges} instead.
     * Takes `O(|E|)` time.
     */
    outEdges(v: NodeID | number, w?: NodeID | number): EdgeObj[] | undefined;
    /**
     * @param {NodeID | number} v - Target Node ID.
     * @param {NodeID | number} [w] - If set, filters those edges down to just
     * those between nodes `v` and `w` regardless of direction
     * @returns {EdgeObj[] | undefined} all edges to or from node `v` regardless
     * of direction. Returns `undefined` if node `v` is not in the graph.
     * @remarks Takes `O(|E|)` time.
     */
    nodeEdges(v: NodeID | number, w?: NodeID | number): EdgeObj[] | undefined;
    _nodeCount: number;
    _edgeCount: number;
}
/**
 * ID of a node.
 */
export type NodeID = string;
/**
 * ID of an edge.
 */
export type EdgeID = `${string}${typeof EDGE_KEY_DELIM}${string}${typeof EDGE_KEY_DELIM}${string}`;
export type EdgeObj = {
    /**
     * the id of the source or tail node of an edge
     */
    v: NodeID;
    /**
     * the id of the target or head node of an edge
     */
    w: NodeID;
    /**
     * Name of the edge. Needed to uniquely identify
     * multiple edges between the same pair of nodes in a multigraph.
     */
    name?: string | number;
};
/**
 * Lodash object that can be iterated over with `_.each`.
 *
 * Beware, objects with `.length` are treated as arrays, see
 * https://lodash.com/docs/4.17.15#forEach
 */
export type Collection<T extends unknown> = T[] | Record<any, T>;
export type GraphOptions = {
    /**
     * - set to `true` to get a
     * directed graph and `false` to get an undirected graph.
     * An undirected graph does not treat the order of nodes in an edge as
     * significant.
     * In other words, `g.edge("a", "b") === g.edge("b", "a")` for
     * an undirected graph.
     * Default: `true`
     */
    directed?: boolean | undefined;
    /**
     * - set to `true` to allow a
     * graph to have multiple edges between the same pair of nodes.
     * Default: `false`.
     */
    multigraph?: boolean | undefined;
    /**
     * - set to `true` to allow a
     * graph to have compound nodes - nodes which can be the parent of other
     * nodes.
     * Default: `false`.
     */
    compound?: boolean | undefined;
};
declare var EDGE_KEY_DELIM: string;
export {};
