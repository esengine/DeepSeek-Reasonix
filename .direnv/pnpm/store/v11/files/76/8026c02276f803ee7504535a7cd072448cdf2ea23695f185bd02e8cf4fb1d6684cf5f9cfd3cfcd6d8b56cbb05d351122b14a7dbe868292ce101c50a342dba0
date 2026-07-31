import * as _ from 'lodash-es';

var DEFAULT_EDGE_NAME = '\x00';
var GRAPH_NODE = '\x00';
var EDGE_KEY_DELIM = '\x01';

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

// Implementation notes:
//
//  * Node id query functions should return string ids for the nodes
//  * Edge id query functions should return an "edgeObj", edge object, that is
//    composed of enough information to uniquely identify an edge: {v, w, name}.
//  * Internally we use an "edgeId", a stringified form of the edgeObj, to
//    reference edges. This is because we need a performant way to look these
//    edges up and, object properties, which have string keys, are the closest
//    we're going to get to a performant hashtable in JavaScript.

// Implementation notes:
//
//  * Node id query functions should return string ids for the nodes
//  * Edge id query functions should return an "edgeObj", edge object, that is
//    composed of enough information to uniquely identify an edge: {v, w, name}.
//  * Internally we use an "edgeId", a stringified form of the edgeObj, to
//    reference edges. This is because we need a performant way to look these
//    edges up and, object properties, which have string keys, are the closest
//    we're going to get to a performant hashtable in JavaScript.

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
export class Graph {
  /**
   * @param {GraphOptions} [opts] - Graph options.
   */
  constructor(opts = {}) {
    /**
     * @type {boolean}
     * @private
     */
    this._isDirected = Object.prototype.hasOwnProperty.call(opts, 'directed')
      ? opts.directed
      : true;
    /**
     * @type {boolean}
     * @private
     */
    this._isMultigraph = Object.prototype.hasOwnProperty.call(opts, 'multigraph')
      ? opts.multigraph
      : false;
    /**
     * @type {boolean}
     * @private
     */
    this._isCompound = Object.prototype.hasOwnProperty.call(opts, 'compound')
      ? opts.compound
      : false;

    /**
     * @type {GraphLabel | undefined}
     * Label for the graph itself
     */
    this._label = undefined;

    /**
     * Default label to be set when creating a new node.
     *
     * @private
     * @type {(v: NodeID | number) => NodeLabel}
     */
    this._defaultNodeLabelFn = _.constant(undefined);

    /**
     * Default label to be set when creating a new edge
     *
     * @private
     * @type {(v: NodeID, w: NodeID, name: string | undefined) => EdgeLabel}
     */
    this._defaultEdgeLabelFn = _.constant(undefined);

    /**
     * @type {Record<NodeID, NodeLabel>}
     * @private
     *
     * v -> label
     */
    this._nodes = {};

    if (this._isCompound) {
      /**
       * @type {Record<NodeID, NodeID>}
       * @private
       * v -> parent
       */
      this._parent = {};

      /**
       * @type {Record<NodeID, Record<NodeID, true>>}
       * @private
       * v -> children
       */
      this._children = {};
      this._children[GRAPH_NODE] = {};
    }

    /**
     * @type {Record<NodeID, Record<EdgeID, EdgeObj>>}
     * @private
     * v -> edgeObj
     */
    this._in = {};

    /**
     * @type {Record<NodeID, Record<NodeID, number>>}
     * @private
     * u -> v -> Number
     */
    this._preds = {};

    /**
     * @type {Record<NodeID, Record<EdgeID, EdgeObj>>}
     * @private
     * v -> edgeObj
     */
    this._out = {};

    /**
     * @type {Record<NodeID, Record<NodeID, number>>}
     * @private
     * v -> w -> Number
     */
    this._sucs = {};

    /**
     * @type {Record<EdgeID, EdgeObj>}
     * @private
     * e -> edgeObj
     */
    this._edgeObjs = {};

    /**
     * @type {Record<EdgeID, EdgeLabel>}
     * @private
     * e -> label
     */
    this._edgeLabels = {};
  }

  /* === Graph functions ========= */

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
  isDirected() {
    return this._isDirected;
  }
  /**
   * @returns {boolean} `true` if the graph is a multigraph.
   */
  isMultigraph() {
    return this._isMultigraph;
  }
  /**
   * @returns {boolean} `true` if the graph is compound.
   */
  isCompound() {
    return this._isCompound;
  }

  /**
   * Sets the label for the graph to `label`.
   *
   * @param {GraphLabel} label - Label for the graph.
   * @returns {this}
   */
  setGraph(label) {
    this._label = label;
    return this;
  }

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
  graph() {
    return this._label;
  }
  /* === Node functions ========== */

  /**
   * Sets a new default value that is assigned to nodes that are created without
   * a label.
   *
   * @param {typeof this._defaultNodeLabelFn | NodeLabel} newDefault - If a function,
   * it is called with the id of the node being created.
   * Otherwise, it is assigned as the label directly.
   * @returns {this}
   */
  setDefaultNodeLabel(newDefault) {
    if (!_.isFunction(newDefault)) {
      newDefault = _.constant(newDefault);
    }
    this._defaultNodeLabelFn = newDefault;
    return this;
  }

  /**
   * @returns {number} the number of nodes in the graph.
   */
  nodeCount() {
    return this._nodeCount;
  }

  /**
   * @returns {NodeID[]} the ids of the nodes in the graph.
   *
   * @remarks
   * Use {@link node()} to get the label for each node.
   * Takes `O(|V|)` time.
   */
  nodes() {
    return _.keys(this._nodes);
  }
  /**
   * @returns {NodeID[]} those nodes in the graph that have no in-edges.
   * @remarks Takes `O(|V|)` time.
   */
  sources() {
    var self = this;
    return _.filter(this.nodes(), function (v) {
      return _.isEmpty(self._in[v]);
    });
  }
  /**
   * @returns {NodeID[]} those nodes in the graph that have no out-edges.
   * @remarks Takes `O(|V|)` time.
   */
  sinks() {
    var self = this;
    return _.filter(this.nodes(), function (v) {
      return _.isEmpty(self._out[v]);
    });
  }

  /**
   * Invokes setNode method for each node in `vs` list.
   *
   * @param {Collection<NodeID | number>} vs - List of node IDs to create/set.
   * @param {NodeLabel} [value] - If set, update all nodes with this value.
   * @returns {this}
   * @remarks Complexity: O(|names|).
   */
  setNodes(vs, value) {
    var args = arguments;
    var self = this;
    _.each(vs, function (v) {
      if (args.length > 1) {
        self.setNode(v, value);
      } else {
        self.setNode(v);
      }
    });
    return this;
  }

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
  setNode(v, value) {
    if (Object.prototype.hasOwnProperty.call(this._nodes, v)) {
      if (arguments.length > 1) {
        this._nodes[v] = value;
      }
      return this;
    }

    this._nodes[v] = arguments.length > 1 ? value : this._defaultNodeLabelFn(v);
    if (this._isCompound) {
      this._parent[v] = GRAPH_NODE;
      this._children[v] = {};
      this._children[GRAPH_NODE][v] = true;
    }
    this._in[v] = {};
    this._preds[v] = {};
    this._out[v] = {};
    this._sucs[v] = {};
    ++this._nodeCount;
    return this;
  }

  /**
   * Gets the label of node with specified name.
   *
   * @param {NodeID | number} v - Node ID.
   * @returns {NodeLabel | undefined} the label assigned to the node with the id `v`
   * if it is in the graph.
   * Otherwise returns `undefined`.
   * @remarks Takes `O(1)` time.
   */
  node(v) {
    return this._nodes[v];
  }

  /**
   * Detects whether graph has a node with specified name or not.
   *
   * @param {NodeID | number} v - Node ID.
   * @returns {boolean} Returns `true` the graph has a node with the id.
   * @remarks Takes `O(1)` time.
   */
  hasNode(v) {
    return Object.prototype.hasOwnProperty.call(this._nodes, v);
  }

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
  removeNode(v) {
    if (Object.prototype.hasOwnProperty.call(this._nodes, v)) {
      var removeEdge = (e) => this.removeEdge(this._edgeObjs[e]);
      delete this._nodes[v];
      if (this._isCompound) {
        this._removeFromParentsChildList(v);
        delete this._parent[v];
        _.each(this.children(v), (child) => {
          this.setParent(child);
        });
        delete this._children[v];
      }
      _.each(_.keys(this._in[v]), removeEdge);
      delete this._in[v];
      delete this._preds[v];
      _.each(_.keys(this._out[v]), removeEdge);
      delete this._out[v];
      delete this._sucs[v];
      --this._nodeCount;
    }
    return this;
  }

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
  setParent(v, parent) {
    if (!this._isCompound) {
      throw new Error('Cannot set parent in a non-compound graph');
    }

    if (_.isUndefined(parent)) {
      parent = GRAPH_NODE;
    } else {
      // Coerce parent to string
      parent += '';
      for (var ancestor = parent; !_.isUndefined(ancestor); ancestor = this.parent(ancestor)) {
        if (ancestor === v) {
          throw new Error('Setting ' + parent + ' as parent of ' + v + ' would create a cycle');
        }
      }

      this.setNode(parent);
    }

    this.setNode(v);
    this._removeFromParentsChildList(v);
    // @ts-expect-error -- We coerced parent to a string above
    this._parent[v] = parent;
    this._children[parent][v] = true;
    return this;
  }

  /**
   * @private
   * @param {NodeID | number} v - Node ID.
   */
  _removeFromParentsChildList(v) {
    delete this._children[this._parent[v]][v];
  }

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
  parent(v) {
    if (this._isCompound) {
      var parent = this._parent[v];
      if (parent !== GRAPH_NODE) {
        return parent;
      }
    }
  }

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
  children(v) {
    if (_.isUndefined(v)) {
      v = GRAPH_NODE;
    }

    if (this._isCompound) {
      var children = this._children[v];
      if (children) {
        return _.keys(children);
      }
    } else if (v === GRAPH_NODE) {
      return this.nodes();
    } else if (this.hasNode(v)) {
      return [];
    }
  }

  /**
   * @param {NodeID | number} v - Node ID.
   * @returns {NodeID[] | undefined} all nodes that are predecessors of the
   * specified node or `undefined` if node `v` is not in the graph.
   * @remarks
   * Behavior is undefined for undirected graphs - use {@link neighbors} instead.
   * Takes `O(|V|)` time.
   */
  predecessors(v) {
    var predsV = this._preds[v];
    if (predsV) {
      return _.keys(predsV);
    }
  }

  /**
   * @param {NodeID | number} v - Node ID.
   * @returns {NodeID[] | undefined} all nodes that are successors of the
   * specified node or `undefined` if node `v` is not in the graph.
   * @remarks
   * Behavior is undefined for undirected graphs - use {@link neighbors} instead.
   * Takes `O(|V|)` time.
   */
  successors(v) {
    var sucsV = this._sucs[v];
    if (sucsV) {
      return _.keys(sucsV);
    }
  }

  /**
   * @param {NodeID | number} v - Node ID.
   * @returns {NodeID[] | undefined} all nodes that are predecessors or
   * successors of the specified node
   * or `undefined` if node `v` is not in the graph.
   * @remarks Takes `O(|V|)` time.
   */
  neighbors(v) {
    var preds = this.predecessors(v);
    if (preds) {
      return _.union(preds, this.successors(v));
    }
  }

  /**
   * @param {NodeID | number} v - Node ID.
   * @returns {boolean} True if the node is a leaf (has no successors), false otherwise.
   */
  isLeaf(v) {
    var neighbors;
    if (this.isDirected()) {
      neighbors = this.successors(v);
    } else {
      neighbors = this.neighbors(v);
    }
    return neighbors.length === 0;
  }

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
  filterNodes(filter) {
    /**
     * @type {Graph<GraphLabel, NodeLabel, EdgeLabel>}
     */
    // @ts-expect-error
    var copy = new this.constructor({
      directed: this._isDirected,
      multigraph: this._isMultigraph,
      compound: this._isCompound,
    });

    copy.setGraph(this.graph());

    var self = this;
    _.each(this._nodes, function (value, v) {
      if (filter(v)) {
        copy.setNode(v, value);
      }
    });

    _.each(this._edgeObjs, function (e) {
      if (copy.hasNode(e.v) && copy.hasNode(e.w)) {
        copy.setEdge(e, self.edge(e));
      }
    });

    var parents = {};
    function findParent(v) {
      var parent = self.parent(v);
      if (parent === undefined || copy.hasNode(parent)) {
        parents[v] = parent;
        return parent;
      } else if (parent in parents) {
        return parents[parent];
      } else {
        return findParent(parent);
      }
    }

    if (this._isCompound) {
      _.each(copy.nodes(), function (v) {
        copy.setParent(v, findParent(v));
      });
    }

    return copy;
  }

  /* === Edge functions ========== */

  /**
   * Sets a new default value that is assigned to edges that are created without
   * a label.
   *
   * @param {typeof this._defaultEdgeLabelFn | EdgeLabel} newDefault - If a function,
   * it is called with the parameters `(v, w, name)`.
   * Otherwise, it is assigned as the label directly.
   * @returns {this}
   */
  setDefaultEdgeLabel(newDefault) {
    if (!_.isFunction(newDefault)) {
      newDefault = _.constant(newDefault);
    }
    this._defaultEdgeLabelFn = newDefault;
    return this;
  }

  /**
   * @returns {number} the number of edges in the graph.
   * @remarks Complexity: O(1).
   */
  edgeCount() {
    return this._edgeCount;
  }

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
  edges() {
    return _.values(this._edgeObjs);
  }

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
  setPath(vs, value) {
    var self = this;
    var args = arguments;
    _.reduce(vs, function (v, w) {
      if (args.length > 1) {
        self.setEdge(v, w, value);
      } else {
        self.setEdge(v, w);
      }
      return w;
    });
    return this;
  }

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
  setEdge() {
    var v, w, name, value;
    var valueSpecified = false;
    var arg0 = arguments[0];

    if (typeof arg0 === 'object' && arg0 !== null && 'v' in arg0) {
      v = arg0.v;
      w = arg0.w;
      name = arg0.name;
      if (arguments.length === 2) {
        value = arguments[1];
        valueSpecified = true;
      }
    } else {
      v = arg0;
      w = arguments[1];
      name = arguments[3];
      if (arguments.length > 2) {
        value = arguments[2];
        valueSpecified = true;
      }
    }

    v = '' + v;
    w = '' + w;
    if (!_.isUndefined(name)) {
      name = '' + name;
    }

    var e = edgeArgsToId(this._isDirected, v, w, name);
    if (Object.prototype.hasOwnProperty.call(this._edgeLabels, e)) {
      if (valueSpecified) {
        this._edgeLabels[e] = value;
      }
      return this;
    }

    if (!_.isUndefined(name) && !this._isMultigraph) {
      throw new Error('Cannot set a named edge when isMultigraph = false');
    }

    // It didn't exist, so we need to create it.
    // First ensure the nodes exist.
    this.setNode(v);
    this.setNode(w);

    this._edgeLabels[e] = valueSpecified ? value : this._defaultEdgeLabelFn(v, w, name);

    var edgeObj = edgeArgsToObj(this._isDirected, v, w, name);
    // Ensure we add undirected edges in a consistent way.
    v = edgeObj.v;
    w = edgeObj.w;

    Object.freeze(edgeObj);
    this._edgeObjs[e] = edgeObj;
    incrementOrInitEntry(this._preds[w], v);
    incrementOrInitEntry(this._sucs[v], w);
    this._in[w][e] = edgeObj;
    this._out[v][e] = edgeObj;
    this._edgeCount++;
    return this;
  }

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
  edge(v, w, name) {
    var e =
      arguments.length === 1
        ? edgeObjToId(this._isDirected, arguments[0])
        : edgeArgsToId(this._isDirected, v, w, name);
    return this._edgeLabels[e];
  }

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
  hasEdge(v, w, name) {
    var e =
      arguments.length === 1
        ? edgeObjToId(this._isDirected, arguments[0])
        : edgeArgsToId(this._isDirected, v, w, name);
    return Object.prototype.hasOwnProperty.call(this._edgeLabels, e);
  }

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
  removeEdge(v, w, name) {
    var e =
      arguments.length === 1
        ? edgeObjToId(this._isDirected, arguments[0])
        : edgeArgsToId(this._isDirected, v, w, name);
    var edge = this._edgeObjs[e];
    if (edge) {
      v = edge.v;
      w = edge.w;
      delete this._edgeLabels[e];
      delete this._edgeObjs[e];
      decrementOrRemoveEntry(this._preds[w], v);
      decrementOrRemoveEntry(this._sucs[v], w);
      delete this._in[w][e];
      delete this._out[v][e];
      this._edgeCount--;
    }
    return this;
  }

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
  inEdges(v, u) {
    var inV = this._in[v];
    if (inV) {
      var edges = _.values(inV);
      if (!u) {
        return edges;
      }
      return _.filter(edges, function (edge) {
        return edge.v === u;
      });
    }
  }

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
  outEdges(v, w) {
    var outV = this._out[v];
    if (outV) {
      var edges = _.values(outV);
      if (!w) {
        return edges;
      }
      return _.filter(edges, function (edge) {
        return edge.w === w;
      });
    }
  }

  /**
   * @param {NodeID | number} v - Target Node ID.
   * @param {NodeID | number} [w] - If set, filters those edges down to just
   * those between nodes `v` and `w` regardless of direction
   * @returns {EdgeObj[] | undefined} all edges to or from node `v` regardless
   * of direction. Returns `undefined` if node `v` is not in the graph.
   * @remarks Takes `O(|E|)` time.
   */
  nodeEdges(v, w) {
    var inEdges = this.inEdges(v, w);
    if (inEdges) {
      return inEdges.concat(this.outEdges(v, w));
    }
  }
}

/* Number of nodes in the graph. Should only be changed by the implementation. */
Graph.prototype._nodeCount = 0;

/* Number of edges in the graph. Should only be changed by the implementation. */
Graph.prototype._edgeCount = 0;

/**
 * @param {Record<NodeID, number>} map - Object mapping node IDs to counts.
 * @param {NodeID | number} k - Node ID.
 */
function incrementOrInitEntry(map, k) {
  if (map[k]) {
    map[k]++;
  } else {
    map[k] = 1;
  }
}

/**
 * @param {Record<NodeID, number>} map - Object mapping node IDs to counts.
 * @param {NodeID | number} k - Node ID.
 */
function decrementOrRemoveEntry(map, k) {
  if (!--map[k]) {
    delete map[k];
  }
}

/**
 * @param {boolean} isDirected - If `false`, sorts v and w to ensure a consistent ID.
 * @param {EdgeObj['v'] | number} v_ - Source node ID.
 * @param {EdgeObj['w'] | number} w_ - Target node ID.
 * @param {EdgeObj['name']} [name] - Edge name (for multiple edges between the same nodes).
 * @returns {EdgeID} Unique ID for the edge.
 */
function edgeArgsToId(isDirected, v_, w_, name) {
  var v = '' + v_;
  var w = '' + w_;
  if (!isDirected && v > w) {
    var tmp = v;
    v = w;
    w = tmp;
  }
  return v + EDGE_KEY_DELIM + w + EDGE_KEY_DELIM + (_.isUndefined(name) ? DEFAULT_EDGE_NAME : name);
}

/**
 * @param {boolean} isDirected - If `false`, sorts v and w to ensure a consistent ID.
 * @param {EdgeObj['v'] | number} v_ - Source node ID.
 * @param {EdgeObj['w'] | number} w_ - Target node ID.
 * @param {EdgeObj['name']} [name] - Edge name (for multiple edges between the same nodes).
 * @returns {EdgeObj}
 */
function edgeArgsToObj(isDirected, v_, w_, name) {
  var v = '' + v_;
  var w = '' + w_;
  if (!isDirected && v > w) {
    var tmp = v;
    v = w;
    w = tmp;
  }
  var edgeObj = { v: v, w: w };
  if (name) {
    edgeObj.name = name;
  }
  return edgeObj;
}

/**
 * @param {boolean} isDirected - If `false`, sorts v and w to ensure a consistent ID.
 * @param {EdgeObj} edgeObj - Edge object.
 * @returns {EdgeID} Unique ID for the edge.
 */
function edgeObjToId(isDirected, edgeObj) {
  return edgeArgsToId(isDirected, edgeObj.v, edgeObj.w, edgeObj.name);
}
