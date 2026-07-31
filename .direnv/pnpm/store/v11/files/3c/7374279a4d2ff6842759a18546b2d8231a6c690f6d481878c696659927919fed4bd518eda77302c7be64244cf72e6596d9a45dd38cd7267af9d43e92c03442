import {
  getIconStyles
} from "./chunk-RTBOCTTP.mjs";
import {
  getDiagramElement
} from "./chunk-HPLX5OYV.mjs";
import {
  setupViewPortForSVG
} from "./chunk-DRARJEGU.mjs";
import {
  JSON_SCHEMA,
  load
} from "./chunk-PWCFYZI5.mjs";
import {
  getRegisteredLayoutAlgorithm,
  render
} from "./chunk-FW7PRHBN.mjs";
import {
  createTooltip
} from "./chunk-QVUG6NDL.mjs";
import {
  isValidShape
} from "./chunk-65BZPYT2.mjs";
import {
  getEdgeId,
  utils_default
} from "./chunk-MMGVDTGO.mjs";
import {
  channel_default,
  clear,
  common_default,
  defaultConfig2 as defaultConfig,
  getAccDescription,
  getAccTitle,
  getConfig2 as getConfig,
  getDiagramTitle,
  getUserDefinedConfig,
  purify,
  rgba_default,
  setAccDescription,
  setAccTitle,
  setConfig2 as setConfig,
  setDiagramTitle
} from "./chunk-CHAKFXHA.mjs";
import {
  log,
  select_default
} from "./chunk-FO5PYUIK.mjs";
import {
  __name
} from "./chunk-PTVI3W5X.mjs";

// src/diagrams/flowchart/flowDb.ts
var MERMAID_DOM_ID_PREFIX = "flowchart-";
var FlowDB = class {
  // cspell:ignore funs
  constructor() {
    this.vertexCounter = 0;
    this.config = getConfig();
    this.diagramId = "";
    this.vertices = /* @__PURE__ */ new Map();
    this.edges = [];
    this.classes = /* @__PURE__ */ new Map();
    this.subGraphs = [];
    this.subGraphLookup = /* @__PURE__ */ new Map();
    this.tooltips = /* @__PURE__ */ new Map();
    this.subCount = 0;
    this.firstGraphFlag = true;
    // As in graph
    this.secCount = -1;
    this.posCrossRef = [];
    // Functions to be run after graph rendering
    this.funs = [];
    this.setAccTitle = setAccTitle;
    this.setAccDescription = setAccDescription;
    this.setDiagramTitle = setDiagramTitle;
    this.getAccTitle = getAccTitle;
    this.getAccDescription = getAccDescription;
    this.getDiagramTitle = getDiagramTitle;
    this.funs.push(this.setupToolTips.bind(this));
    this.addVertex = this.addVertex.bind(this);
    this.firstGraph = this.firstGraph.bind(this);
    this.setDirection = this.setDirection.bind(this);
    this.addSubGraph = this.addSubGraph.bind(this);
    this.addLink = this.addLink.bind(this);
    this.setLink = this.setLink.bind(this);
    this.updateLink = this.updateLink.bind(this);
    this.addClass = this.addClass.bind(this);
    this.setClass = this.setClass.bind(this);
    this.destructLink = this.destructLink.bind(this);
    this.setClickEvent = this.setClickEvent.bind(this);
    this.setTooltip = this.setTooltip.bind(this);
    this.updateLinkInterpolate = this.updateLinkInterpolate.bind(this);
    this.setClickFun = this.setClickFun.bind(this);
    this.bindFunctions = this.bindFunctions.bind(this);
    this.lex = {
      firstGraph: this.firstGraph.bind(this)
    };
    this.clear();
    this.setGen("gen-2");
  }
  static {
    __name(this, "FlowDB");
  }
  sanitizeText(txt) {
    return common_default.sanitizeText(txt, this.config);
  }
  sanitizeNodeLabelType(labelType) {
    switch (labelType) {
      case "markdown":
      case "string":
      case "text":
        return labelType;
      default:
        return "markdown";
    }
  }
  /**
   * Sets the diagram's SVG element ID, used to prefix domIds for uniqueness
   * across multiple diagrams on the same page.
   */
  setDiagramId(svgElementId) {
    this.diagramId = svgElementId;
  }
  /**
   * Function to lookup domId from id in the graph definition.
   * When diagramId is set, returns the prefixed version for DOM uniqueness.
   *
   * @param id - id of the node
   */
  lookUpDomId(id) {
    for (const vertex of this.vertices.values()) {
      if (vertex.id === id) {
        return this.diagramId ? `${this.diagramId}-${vertex.domId}` : vertex.domId;
      }
    }
    return this.diagramId ? `${this.diagramId}-${id}` : id;
  }
  /**
   * Function called by parser when a node definition has been found
   */
  addVertex(id, textObj, type, style, classes, dir, props = {}, metadata) {
    if (!id || id.trim().length === 0) {
      return;
    }
    let doc;
    if (metadata !== void 0) {
      let yamlData;
      if (!metadata.includes("\n")) {
        yamlData = "{\n" + metadata + "\n}";
      } else {
        yamlData = metadata + "\n";
      }
      doc = load(yamlData, { schema: JSON_SCHEMA });
    }
    const edge = this.edges.find((e) => e.id === id);
    if (edge) {
      const edgeDoc = doc;
      if (edgeDoc?.animate !== void 0) {
        edge.animate = edgeDoc.animate;
      }
      if (edgeDoc?.animation !== void 0) {
        edge.animation = edgeDoc.animation;
      }
      if (edgeDoc?.curve !== void 0) {
        edge.interpolate = edgeDoc.curve;
      }
      return;
    }
    let txt;
    let vertex = this.vertices.get(id);
    if (vertex === void 0) {
      if (textObj === void 0 && type === void 0 && style !== void 0 && style !== null) {
        log.warn(
          `Style applied to unknown node "${id}". This may indicate a typo. The node will be created automatically.`
        );
      }
      vertex = {
        id,
        labelType: "text",
        domId: MERMAID_DOM_ID_PREFIX + id + "-" + this.vertexCounter,
        styles: [],
        classes: []
      };
      this.vertices.set(id, vertex);
    }
    this.vertexCounter++;
    if (textObj !== void 0) {
      this.config = getConfig();
      txt = this.sanitizeText(textObj.text.trim());
      vertex.labelType = textObj.type;
      if (txt.startsWith('"') && txt.endsWith('"')) {
        txt = txt.substring(1, txt.length - 1);
      }
      vertex.text = txt;
    } else {
      if (vertex.text === void 0) {
        vertex.text = id;
      }
    }
    if (type !== void 0) {
      vertex.type = type;
    }
    if (style !== void 0 && style !== null) {
      style.forEach((s) => {
        vertex.styles.push(s);
      });
    }
    if (classes !== void 0 && classes !== null) {
      classes.forEach((s) => {
        vertex.classes.push(s);
      });
    }
    if (dir !== void 0) {
      vertex.dir = dir;
    }
    if (vertex.props === void 0) {
      vertex.props = props;
    } else if (props !== void 0) {
      Object.assign(vertex.props, props);
    }
    if (doc !== void 0) {
      if (doc.shape) {
        if (doc.shape !== doc.shape.toLowerCase() || doc.shape.includes("_")) {
          throw new Error(`No such shape: ${doc.shape}. Shape names should be lowercase.`);
        } else if (!isValidShape(doc.shape)) {
          throw new Error(`No such shape: ${doc.shape}.`);
        }
        vertex.type = doc?.shape;
      }
      if (doc?.label) {
        vertex.text = doc?.label;
        vertex.labelType = this.sanitizeNodeLabelType(doc?.labelType);
      }
      if (doc?.icon) {
        vertex.icon = doc?.icon;
        if (!doc.label?.trim() && vertex.text === id) {
          vertex.text = "";
        }
      }
      if (doc?.form) {
        vertex.form = doc?.form;
      }
      if (doc?.pos) {
        vertex.pos = doc?.pos;
      }
      if (doc?.img) {
        vertex.img = doc?.img;
        if (!doc.label?.trim() && vertex.text === id) {
          vertex.text = "";
        }
      }
      if (doc?.constraint) {
        vertex.constraint = doc.constraint;
      }
      if (doc.w) {
        vertex.assetWidth = Number(doc.w);
      }
      if (doc.h) {
        vertex.assetHeight = Number(doc.h);
      }
    }
  }
  /**
   * Function called by parser when a link/edge definition has been found
   *
   */
  addSingleLink(_start, _end, type, id) {
    const start = _start;
    const end = _end;
    const edge = {
      start,
      end,
      type: void 0,
      text: "",
      labelType: "text",
      classes: [],
      isUserDefinedId: false,
      interpolate: this.edges.defaultInterpolate
    };
    log.info("abc78 Got edge...", edge);
    const linkTextObj = type.text;
    if (linkTextObj !== void 0) {
      edge.text = this.sanitizeText(linkTextObj.text.trim());
      if (edge.text.startsWith('"') && edge.text.endsWith('"')) {
        edge.text = edge.text.substring(1, edge.text.length - 1);
      }
      edge.labelType = this.sanitizeNodeLabelType(linkTextObj.type);
    }
    if (type !== void 0) {
      edge.type = type.type;
      edge.stroke = type.stroke;
      edge.length = type.length > 10 ? 10 : type.length;
    }
    if (id && !this.edges.some((e) => e.id === id)) {
      edge.id = id;
      edge.isUserDefinedId = true;
    } else {
      const existingLinks = this.edges.filter((e) => e.start === edge.start && e.end === edge.end);
      if (existingLinks.length === 0) {
        edge.id = getEdgeId(edge.start, edge.end, { counter: 0, prefix: "L" });
      } else {
        edge.id = getEdgeId(edge.start, edge.end, {
          counter: existingLinks.length + 1,
          prefix: "L"
        });
      }
    }
    if (this.edges.length < (this.config.maxEdges ?? 500)) {
      log.info("Pushing edge...");
      this.edges.push(edge);
    } else {
      throw new Error(
        `Edge limit exceeded. ${this.edges.length} edges found, but the limit is ${this.config.maxEdges}.

Initialize mermaid with maxEdges set to a higher number to allow more edges.
You cannot set this config via configuration inside the diagram as it is a secure config.
You have to call mermaid.initialize.`
      );
    }
  }
  isLinkData(value) {
    return value !== null && typeof value === "object" && "id" in value && typeof value.id === "string";
  }
  addLink(_start, _end, linkData) {
    const id = this.isLinkData(linkData) ? linkData.id.replace("@", "") : void 0;
    log.info("addLink", _start, _end, id);
    for (const start of _start) {
      for (const end of _end) {
        const isLastStart = start === _start[_start.length - 1];
        const isFirstEnd = end === _end[0];
        if (isLastStart && isFirstEnd) {
          this.addSingleLink(start, end, linkData, id);
        } else {
          this.addSingleLink(start, end, linkData, void 0);
        }
      }
    }
  }
  /**
   * Updates a link's line interpolation algorithm
   */
  updateLinkInterpolate(positions, interpolate) {
    positions.forEach((pos) => {
      if (pos === "default") {
        this.edges.defaultInterpolate = interpolate;
      } else {
        this.edges[pos].interpolate = interpolate;
      }
    });
  }
  /**
   * Updates a link with a style
   *
   */
  updateLink(positions, style) {
    positions.forEach((pos) => {
      if (typeof pos === "number" && pos >= this.edges.length) {
        throw new Error(
          `The index ${pos} for linkStyle is out of bounds. Valid indices for linkStyle are between 0 and ${this.edges.length - 1}. (Help: Ensure that the index is within the range of existing edges.)`
        );
      }
      if (pos === "default") {
        this.edges.defaultStyle = style;
      } else {
        this.edges[pos].style = style;
        if ((this.edges[pos]?.style?.length ?? 0) > 0 && !this.edges[pos]?.style?.some((s) => s?.startsWith("fill"))) {
          this.edges[pos]?.style?.push("fill:none");
        }
      }
    });
  }
  addClass(ids, _style) {
    const style = _style.join().replace(/\\,/g, "\xA7\xA7\xA7").replace(/,/g, ";").replace(/§§§/g, ",").split(";");
    ids.split(",").forEach((id) => {
      let classNode = this.classes.get(id);
      if (classNode === void 0) {
        classNode = { id, styles: [], textStyles: [] };
        this.classes.set(id, classNode);
      }
      if (style !== void 0 && style !== null) {
        style.forEach((s) => {
          if (/color/.exec(s)) {
            const newStyle = s.replace("fill", "bgFill");
            classNode.textStyles.push(newStyle);
          }
          classNode.styles.push(s);
        });
      }
    });
  }
  /**
   * Called by parser when a graph definition is found, stores the direction of the chart.
   *
   */
  setDirection(dir) {
    this.direction = dir.trim();
    if (/.*</.exec(this.direction)) {
      this.direction = "RL";
    }
    if (/.*\^/.exec(this.direction)) {
      this.direction = "BT";
    }
    if (/.*>/.exec(this.direction)) {
      this.direction = "LR";
    }
    if (/.*v/.exec(this.direction)) {
      this.direction = "TB";
    }
    if (this.direction === "TD") {
      this.direction = "TB";
    }
  }
  /**
   * Called by parser when a special node is found, e.g. a clickable element.
   *
   * @param ids - Comma separated list of ids
   * @param className - Class to add
   */
  setClass(ids, className) {
    for (const id of ids.split(",")) {
      const vertex = this.vertices.get(id);
      if (vertex) {
        vertex.classes.push(className);
      }
      const edge = this.edges.find((e) => e.id === id);
      if (edge) {
        edge.classes.push(className);
      }
      const subGraph = this.subGraphLookup.get(id);
      if (subGraph) {
        subGraph.classes.push(className);
      }
    }
  }
  setTooltip(ids, tooltip) {
    if (tooltip === void 0) {
      return;
    }
    tooltip = this.sanitizeText(tooltip);
    for (const id of ids.split(",")) {
      this.tooltips.set(this.version === "gen-1" ? this.lookUpDomId(id) : id, tooltip);
    }
  }
  setClickFun(id, functionName, functionArgs) {
    if (getConfig().securityLevel !== "loose") {
      return;
    }
    if (functionName === void 0) {
      return;
    }
    let argList = [];
    if (typeof functionArgs === "string") {
      argList = functionArgs.split(/,(?=(?:(?:[^"]*"){2})*[^"]*$)/);
      for (let i = 0; i < argList.length; i++) {
        let item = argList[i].trim();
        if (item.startsWith('"') && item.endsWith('"')) {
          item = item.substr(1, item.length - 2);
        }
        argList[i] = item;
      }
    }
    if (argList.length === 0) {
      argList.push(id);
    }
    const vertex = this.vertices.get(id);
    if (vertex) {
      vertex.haveCallback = true;
      this.funs.push(() => {
        const domId = this.lookUpDomId(id);
        const elem = document.querySelector(`[id="${domId}"]`);
        if (elem !== null) {
          elem.addEventListener(
            "click",
            () => {
              utils_default.runFunc(functionName, ...argList);
            },
            false
          );
        }
      });
    }
  }
  /**
   * Called by parser when a link is found. Adds the URL to the vertex data.
   *
   * @param ids - Comma separated list of ids
   * @param linkStr - URL to create a link for
   * @param target - Target attribute for the link
   */
  setLink(ids, linkStr, target) {
    ids.split(",").forEach((id) => {
      const vertex = this.vertices.get(id);
      if (vertex !== void 0) {
        vertex.link = utils_default.formatUrl(linkStr, this.config);
        vertex.linkTarget = target;
      }
    });
    this.setClass(ids, "clickable");
  }
  getTooltip(id) {
    return this.tooltips.get(id);
  }
  /**
   * Called by parser when a click definition is found. Registers an event handler.
   *
   * @param ids - Comma separated list of ids
   * @param functionName - Function to be called on click
   * @param functionArgs - Arguments to be passed to the function
   */
  setClickEvent(ids, functionName, functionArgs) {
    ids.split(",").forEach((id) => {
      this.setClickFun(id, functionName, functionArgs);
    });
    this.setClass(ids, "clickable");
  }
  bindFunctions(element) {
    this.funs.forEach((fun) => {
      fun(element);
    });
  }
  getDirection() {
    return this.direction?.trim();
  }
  /**
   * Retrieval function for fetching the found nodes after parsing has completed.
   *
   */
  getVertices() {
    return this.vertices;
  }
  /**
   * Retrieval function for fetching the found links after parsing has completed.
   *
   */
  getEdges() {
    return this.edges;
  }
  /**
   * Retrieval function for fetching the found class definitions after parsing has completed.
   *
   */
  getClasses() {
    return this.classes;
  }
  setupToolTips(element) {
    const tooltipElem = createTooltip();
    const svg = select_default(element).select("svg");
    const nodes = svg.selectAll("g.node");
    nodes.on("mouseover", (e) => {
      const el = select_default(e.currentTarget);
      const title = el.attr("title");
      if (title === null) {
        return;
      }
      const rect = e.currentTarget?.getBoundingClientRect();
      tooltipElem.transition().duration(200).style("opacity", ".9");
      tooltipElem.text(el.attr("title")).style("left", window.scrollX + rect.left + (rect.right - rect.left) / 2 + "px").style("top", window.scrollY + rect.bottom + "px");
      tooltipElem.html(purify.sanitize(title));
      el.classed("hover", true);
    }).on("mouseout", (e) => {
      tooltipElem.transition().duration(500).style("opacity", 0);
      const el = select_default(e.currentTarget);
      el.classed("hover", false);
    });
  }
  /**
   * Clears the internal graph db so that a new graph can be parsed.
   *
   */
  clear(ver = "gen-2") {
    this.vertices = /* @__PURE__ */ new Map();
    this.classes = /* @__PURE__ */ new Map();
    this.edges = [];
    this.funs = [this.setupToolTips.bind(this)];
    this.diagramId = "";
    this.subGraphs = [];
    this.subGraphLookup = /* @__PURE__ */ new Map();
    this.subCount = 0;
    this.tooltips = /* @__PURE__ */ new Map();
    this.firstGraphFlag = true;
    this.version = ver;
    this.config = getConfig();
    clear();
  }
  setGen(ver) {
    this.version = ver || "gen-2";
  }
  defaultStyle() {
    return "fill:#ffa;stroke: #f66; stroke-width: 3px; stroke-dasharray: 5, 5;fill:#ffa;stroke: #666;";
  }
  addSubGraph(_id, list, _title) {
    let id = _id.text.trim();
    let title = _title.text;
    if (_id === _title && /\s/.exec(_title.text)) {
      id = void 0;
    }
    const uniq = /* @__PURE__ */ __name((a) => {
      const prims = { boolean: {}, number: {}, string: {} };
      const objs = [];
      let dir2;
      const nodeList2 = a.filter(function(item) {
        const type = typeof item;
        if (item.stmt && item.stmt === "dir") {
          dir2 = item.value;
          return false;
        }
        if (item.trim() === "") {
          return false;
        }
        if (type in prims) {
          return prims[type].hasOwnProperty(item) ? false : prims[type][item] = true;
        } else {
          return objs.includes(item) ? false : objs.push(item);
        }
      });
      return { nodeList: nodeList2, dir: dir2 };
    }, "uniq");
    const result = uniq(list.flat());
    const nodeList = result.nodeList;
    const rawDir = result.dir;
    const hasExplicitDir = rawDir !== void 0;
    const flowchartConfig = getConfig().flowchart ?? {};
    const dir = rawDir ?? (flowchartConfig.inheritDir ? this.getDirection() ?? getConfig().direction ?? void 0 : void 0);
    if (this.version === "gen-1") {
      for (let i = 0; i < nodeList.length; i++) {
        nodeList[i] = this.lookUpDomId(nodeList[i]);
      }
    }
    id = id ?? "subGraph" + this.subCount;
    title = title || "";
    title = this.sanitizeText(title);
    this.subCount = this.subCount + 1;
    const subGraph = {
      id,
      nodes: nodeList,
      title: title.trim(),
      classes: [],
      dir,
      hasExplicitDir,
      labelType: this.sanitizeNodeLabelType(_title?.type)
    };
    log.info("Adding", subGraph.id, subGraph.nodes, subGraph.dir);
    subGraph.nodes = this.makeUniq(subGraph, this.subGraphs).nodes;
    this.subGraphs.push(subGraph);
    this.subGraphLookup.set(id, subGraph);
    return id;
  }
  getPosForId(id) {
    for (const [i, subGraph] of this.subGraphs.entries()) {
      if (subGraph.id === id) {
        return i;
      }
    }
    return -1;
  }
  indexNodes2(id, pos) {
    const nodes = this.subGraphs[pos].nodes;
    this.secCount = this.secCount + 1;
    if (this.secCount > 2e3) {
      return {
        result: false,
        count: 0
      };
    }
    this.posCrossRef[this.secCount] = pos;
    if (this.subGraphs[pos].id === id) {
      return {
        result: true,
        count: 0
      };
    }
    let count = 0;
    let posCount = 1;
    while (count < nodes.length) {
      const childPos = this.getPosForId(nodes[count]);
      if (childPos >= 0) {
        const res = this.indexNodes2(id, childPos);
        if (res.result) {
          return {
            result: true,
            count: posCount + res.count
          };
        } else {
          posCount = posCount + res.count;
        }
      }
      count = count + 1;
    }
    return {
      result: false,
      count: posCount
    };
  }
  getDepthFirstPos(pos) {
    return this.posCrossRef[pos];
  }
  indexNodes() {
    this.secCount = -1;
    if (this.subGraphs.length > 0) {
      this.indexNodes2("none", this.subGraphs.length - 1);
    }
  }
  getSubGraphs() {
    return this.subGraphs;
  }
  firstGraph() {
    if (this.firstGraphFlag) {
      this.firstGraphFlag = false;
      return true;
    }
    return false;
  }
  destructStartLink(_str) {
    let str = _str.trim();
    let type = "arrow_open";
    switch (str[0]) {
      case "<":
        type = "arrow_point";
        str = str.slice(1);
        break;
      case "x":
        type = "arrow_cross";
        str = str.slice(1);
        break;
      case "o":
        type = "arrow_circle";
        str = str.slice(1);
        break;
    }
    let stroke = "normal";
    if (str.includes("=")) {
      stroke = "thick";
    }
    if (str.includes(".")) {
      stroke = "dotted";
    }
    return { type, stroke };
  }
  countChar(char, str) {
    const length = str.length;
    let count = 0;
    for (let i = 0; i < length; ++i) {
      if (str[i] === char) {
        ++count;
      }
    }
    return count;
  }
  destructEndLink(_str) {
    const str = _str.trim();
    let line = str.slice(0, -1);
    let type = "arrow_open";
    switch (str.slice(-1)) {
      case "x":
        type = "arrow_cross";
        if (str.startsWith("x")) {
          type = "double_" + type;
          line = line.slice(1);
        }
        break;
      case ">":
        type = "arrow_point";
        if (str.startsWith("<")) {
          type = "double_" + type;
          line = line.slice(1);
        }
        break;
      case "o":
        type = "arrow_circle";
        if (str.startsWith("o")) {
          type = "double_" + type;
          line = line.slice(1);
        }
        break;
    }
    let stroke = "normal";
    let length = line.length - 1;
    if (line.startsWith("=")) {
      stroke = "thick";
    }
    if (line.startsWith("~")) {
      stroke = "invisible";
    }
    const dots = this.countChar(".", line);
    if (dots) {
      stroke = "dotted";
      length = dots;
    }
    return { type, stroke, length };
  }
  destructLink(_str, _startStr) {
    const info = this.destructEndLink(_str);
    let startInfo;
    if (_startStr) {
      startInfo = this.destructStartLink(_startStr);
      if (startInfo.stroke !== info.stroke) {
        return { type: "INVALID", stroke: "INVALID" };
      }
      if (startInfo.type === "arrow_open") {
        startInfo.type = info.type;
      } else {
        if (startInfo.type !== info.type) {
          return { type: "INVALID", stroke: "INVALID" };
        }
        startInfo.type = "double_" + startInfo.type;
      }
      if (startInfo.type === "double_arrow") {
        startInfo.type = "double_arrow_point";
      }
      startInfo.length = info.length;
      return startInfo;
    }
    return info;
  }
  // Todo optimizer this by caching existing nodes
  exists(allSgs, _id) {
    for (const sg of allSgs) {
      if (sg.nodes.includes(_id)) {
        return true;
      }
    }
    return false;
  }
  /**
   * Deletes an id from all subgraphs
   *
   */
  makeUniq(sg, allSubgraphs) {
    const res = [];
    sg.nodes.forEach((_id, pos) => {
      if (!this.exists(allSubgraphs, _id)) {
        res.push(sg.nodes[pos]);
      }
    });
    return { nodes: res };
  }
  getTypeFromVertex(vertex) {
    if (vertex.img) {
      return "imageSquare";
    }
    if (vertex.icon) {
      if (vertex.form === "circle") {
        return "iconCircle";
      }
      if (vertex.form === "square") {
        return "iconSquare";
      }
      if (vertex.form === "rounded") {
        return "iconRounded";
      }
      return "icon";
    }
    switch (vertex.type) {
      case "square":
      case void 0:
        return "squareRect";
      case "round":
        return "roundedRect";
      case "ellipse":
        return "ellipse";
      default:
        return vertex.type;
    }
  }
  findNode(nodes, id) {
    return nodes.find((node) => node.id === id);
  }
  destructEdgeType(type) {
    let arrowTypeStart = "none";
    let arrowTypeEnd = "arrow_point";
    switch (type) {
      case "arrow_point":
      case "arrow_circle":
      case "arrow_cross":
        arrowTypeEnd = type;
        break;
      case "double_arrow_point":
      case "double_arrow_circle":
      case "double_arrow_cross":
        arrowTypeStart = type.replace("double_", "");
        arrowTypeEnd = arrowTypeStart;
        break;
    }
    return { arrowTypeStart, arrowTypeEnd };
  }
  addNodeFromVertex(vertex, nodes, parentDB, subGraphDB, config, look) {
    const parentId = parentDB.get(vertex.id);
    const isGroup = subGraphDB.get(vertex.id) ?? false;
    const node = this.findNode(nodes, vertex.id);
    if (node) {
      node.cssStyles = vertex.styles;
      node.cssCompiledStyles = this.getCompiledStyles(vertex.classes);
      node.cssClasses = vertex.classes.join(" ");
    } else {
      const baseNode = {
        id: vertex.id,
        label: vertex.text,
        labelType: vertex.labelType,
        labelStyle: "",
        parentId,
        padding: config.flowchart?.padding || 8,
        cssStyles: vertex.styles,
        cssCompiledStyles: this.getCompiledStyles(["default", "node", ...vertex.classes]),
        cssClasses: "default " + vertex.classes.join(" "),
        dir: vertex.dir,
        domId: vertex.domId,
        look,
        link: vertex.link,
        linkTarget: vertex.linkTarget,
        tooltip: this.getTooltip(vertex.id),
        icon: vertex.icon,
        pos: vertex.pos,
        img: vertex.img,
        assetWidth: vertex.assetWidth,
        assetHeight: vertex.assetHeight,
        constraint: vertex.constraint
      };
      if (isGroup) {
        nodes.push({
          ...baseNode,
          isGroup: true,
          shape: "rect"
        });
      } else {
        nodes.push({
          ...baseNode,
          isGroup: false,
          shape: this.getTypeFromVertex(vertex)
        });
      }
    }
  }
  getCompiledStyles(classDefs) {
    let compiledStyles = [];
    for (const customClass of classDefs) {
      const cssClass = this.classes.get(customClass);
      if (cssClass?.styles) {
        compiledStyles = [...compiledStyles, ...cssClass.styles ?? []].map((s) => s.trim());
      }
      if (cssClass?.textStyles) {
        compiledStyles = [...compiledStyles, ...cssClass.textStyles ?? []].map((s) => s.trim());
      }
    }
    return compiledStyles;
  }
  getData() {
    const config = getConfig();
    const nodes = [];
    const edges = [];
    const subGraphs = this.getSubGraphs();
    const parentDB = /* @__PURE__ */ new Map();
    const subGraphDB = /* @__PURE__ */ new Map();
    for (let i = subGraphs.length - 1; i >= 0; i--) {
      const subGraph = subGraphs[i];
      if (subGraph.nodes.length > 0) {
        subGraphDB.set(subGraph.id, true);
      }
      for (const id of subGraph.nodes) {
        parentDB.set(id, subGraph.id);
      }
    }
    for (let i = subGraphs.length - 1; i >= 0; i--) {
      const subGraph = subGraphs[i];
      nodes.push({
        id: subGraph.id,
        label: subGraph.title,
        labelStyle: "",
        labelType: subGraph.labelType,
        parentId: parentDB.get(subGraph.id),
        padding: 8,
        cssCompiledStyles: this.getCompiledStyles(subGraph.classes),
        cssClasses: subGraph.classes.join(" "),
        shape: "rect",
        dir: subGraph.dir === "TD" ? "TB" : subGraph.dir,
        // normalize TD→TB for dagre
        explicitDir: subGraph.hasExplicitDir,
        // true only when the user wrote an explicit 'direction X' keyword
        isGroup: true,
        look: config.look
      });
    }
    const n = this.getVertices();
    n.forEach((vertex) => {
      this.addNodeFromVertex(vertex, nodes, parentDB, subGraphDB, config, config.look || "classic");
    });
    const e = this.getEdges();
    e.forEach((rawEdge, index) => {
      const { arrowTypeStart, arrowTypeEnd } = this.destructEdgeType(rawEdge.type);
      const styles = [...e.defaultStyle ?? []];
      if (rawEdge.style) {
        styles.push(...rawEdge.style);
      }
      const edge = {
        id: getEdgeId(rawEdge.start, rawEdge.end, { counter: index, prefix: "L" }, rawEdge.id),
        isUserDefinedId: rawEdge.isUserDefinedId,
        start: rawEdge.start,
        end: rawEdge.end,
        type: rawEdge.type ?? "normal",
        label: rawEdge.text,
        labelType: rawEdge.labelType,
        labelpos: "c",
        thickness: rawEdge.stroke,
        minlen: rawEdge.length,
        classes: rawEdge?.stroke === "invisible" ? "" : "edge-thickness-normal edge-pattern-solid flowchart-link",
        arrowTypeStart: rawEdge?.stroke === "invisible" || rawEdge?.type === "arrow_open" ? "none" : arrowTypeStart,
        arrowTypeEnd: rawEdge?.stroke === "invisible" || rawEdge?.type === "arrow_open" ? "none" : arrowTypeEnd,
        arrowheadStyle: "fill: #333",
        cssCompiledStyles: this.getCompiledStyles(rawEdge.classes),
        labelStyle: styles,
        style: styles,
        pattern: rawEdge.stroke,
        look: config.look,
        animate: rawEdge.animate,
        animation: rawEdge.animation,
        curve: rawEdge.interpolate || this.edges.defaultInterpolate || config.flowchart?.curve
      };
      edges.push(edge);
    });
    return { nodes, edges, other: {}, config };
  }
  defaultConfig() {
    return defaultConfig.flowchart;
  }
};

// src/diagrams/flowchart/flowRenderer-v3-unified.ts
var getClasses = /* @__PURE__ */ __name(function(text, diagramObj) {
  return diagramObj.db.getClasses();
}, "getClasses");
var draw = /* @__PURE__ */ __name(async function(text, id, _version, diag, positions) {
  log.info("REF0:");
  log.info("Drawing state diagram (v2)", id);
  const { securityLevel, flowchart: conf, layout } = getConfig();
  diag.db.setDiagramId(id);
  log.debug("Before getData: ");
  const data4Layout = diag.db.getData();
  log.debug("Data: ", data4Layout);
  const svg = getDiagramElement(id, securityLevel);
  const direction = diag.db.getDirection();
  data4Layout.type = diag.type;
  data4Layout.layoutAlgorithm = getRegisteredLayoutAlgorithm(layout);
  if (data4Layout.layoutAlgorithm === "dagre" && layout === "elk") {
    log.warn(
      "flowchart-elk was moved to an external package in Mermaid v11. Please refer [release notes](https://github.com/mermaid-js/mermaid/releases/tag/v11.0.0) for more details. This diagram will be rendered using `dagre` layout as a fallback."
    );
  }
  data4Layout.direction = direction;
  data4Layout.nodeSpacing = conf?.nodeSpacing || 50;
  data4Layout.rankSpacing = conf?.rankSpacing || 50;
  data4Layout.markers = ["point", "circle", "cross"];
  data4Layout.diagramId = id;
  log.debug("REF1:", data4Layout);
  await render(data4Layout, svg, positions);
  const padding = data4Layout.config.flowchart?.diagramPadding ?? 8;
  utils_default.insertTitle(
    svg,
    "flowchartTitleText",
    conf?.titleTopMargin || 0,
    diag.db.getDiagramTitle()
  );
  setupViewPortForSVG(svg, padding, "flowchart", conf?.useMaxWidth || false);
}, "draw");
var flowRenderer_v3_unified_default = {
  getClasses,
  draw
};

// src/diagrams/flowchart/parser/flow.jison
var parser = (function() {
  var o = /* @__PURE__ */ __name(function(k, v, o2, l) {
    for (o2 = o2 || {}, l = k.length; l--; o2[k[l]] = v) ;
    return o2;
  }, "o"), $V0 = [1, 4], $V1 = [1, 3], $V2 = [1, 5], $V3 = [1, 8, 9, 10, 11, 27, 34, 36, 38, 44, 60, 84, 85, 86, 87, 88, 89, 102, 105, 106, 109, 111, 114, 115, 116, 121, 122, 123, 124, 125], $V4 = [2, 2], $V5 = [1, 13], $V6 = [1, 14], $V7 = [1, 15], $V8 = [1, 16], $V9 = [1, 23], $Va = [1, 25], $Vb = [1, 26], $Vc = [1, 27], $Vd = [1, 50], $Ve = [1, 49], $Vf = [1, 29], $Vg = [1, 30], $Vh = [1, 31], $Vi = [1, 32], $Vj = [1, 33], $Vk = [1, 45], $Vl = [1, 47], $Vm = [1, 43], $Vn = [1, 48], $Vo = [1, 44], $Vp = [1, 51], $Vq = [1, 46], $Vr = [1, 52], $Vs = [1, 53], $Vt = [1, 34], $Vu = [1, 35], $Vv = [1, 36], $Vw = [1, 37], $Vx = [1, 38], $Vy = [1, 58], $Vz = [1, 8, 9, 10, 11, 27, 32, 34, 36, 38, 44, 60, 84, 85, 86, 87, 88, 89, 102, 105, 106, 109, 111, 114, 115, 116, 121, 122, 123, 124, 125], $VA = [1, 62], $VB = [1, 61], $VC = [1, 63], $VD = [8, 9, 11, 75, 77, 78], $VE = [1, 79], $VF = [1, 92], $VG = [1, 97], $VH = [1, 96], $VI = [1, 93], $VJ = [1, 89], $VK = [1, 95], $VL = [1, 91], $VM = [1, 98], $VN = [1, 94], $VO = [1, 99], $VP = [1, 90], $VQ = [8, 9, 10, 11, 40, 75, 77, 78], $VR = [8, 9, 10, 11, 40, 46, 75, 77, 78], $VS = [8, 9, 10, 11, 29, 40, 44, 46, 48, 50, 52, 54, 56, 58, 60, 63, 65, 67, 68, 70, 75, 77, 78, 89, 102, 105, 106, 109, 111, 114, 115, 116], $VT = [8, 9, 11, 44, 60, 75, 77, 78, 89, 102, 105, 106, 109, 111, 114, 115, 116], $VU = [44, 60, 89, 102, 105, 106, 109, 111, 114, 115, 116], $VV = [1, 122], $VW = [1, 123], $VX = [1, 125], $VY = [1, 124], $VZ = [44, 60, 62, 74, 89, 102, 105, 106, 109, 111, 114, 115, 116], $V_ = [1, 134], $V$ = [1, 148], $V01 = [1, 149], $V11 = [1, 150], $V21 = [1, 151], $V31 = [1, 136], $V41 = [1, 138], $V51 = [1, 142], $V61 = [1, 143], $V71 = [1, 144], $V81 = [1, 145], $V91 = [1, 146], $Va1 = [1, 147], $Vb1 = [1, 152], $Vc1 = [1, 153], $Vd1 = [1, 132], $Ve1 = [1, 133], $Vf1 = [1, 140], $Vg1 = [1, 135], $Vh1 = [1, 139], $Vi1 = [1, 137], $Vj1 = [8, 9, 10, 11, 27, 32, 34, 36, 38, 44, 60, 84, 85, 86, 87, 88, 89, 102, 105, 106, 109, 111, 114, 115, 116, 121, 122, 123, 124, 125], $Vk1 = [1, 155], $Vl1 = [1, 157], $Vm1 = [8, 9, 11], $Vn1 = [8, 9, 10, 11, 14, 44, 60, 89, 105, 106, 109, 111, 114, 115, 116], $Vo1 = [1, 177], $Vp1 = [1, 173], $Vq1 = [1, 174], $Vr1 = [1, 178], $Vs1 = [1, 175], $Vt1 = [1, 176], $Vu1 = [77, 116, 119], $Vv1 = [8, 9, 10, 11, 12, 14, 27, 29, 32, 44, 60, 75, 84, 85, 86, 87, 88, 89, 90, 105, 109, 111, 114, 115, 116], $Vw1 = [10, 106], $Vx1 = [31, 49, 51, 53, 55, 57, 62, 64, 66, 67, 69, 71, 116, 117, 118], $Vy1 = [1, 248], $Vz1 = [1, 246], $VA1 = [1, 250], $VB1 = [1, 244], $VC1 = [1, 245], $VD1 = [1, 247], $VE1 = [1, 249], $VF1 = [1, 251], $VG1 = [1, 269], $VH1 = [8, 9, 11, 106], $VI1 = [8, 9, 10, 11, 60, 84, 105, 106, 109, 110, 111, 112];
  var parser2 = {
    trace: /* @__PURE__ */ __name(function trace() {
    }, "trace"),
    yy: {},
    symbols_: { "error": 2, "start": 3, "graphConfig": 4, "document": 5, "line": 6, "statement": 7, "SEMI": 8, "NEWLINE": 9, "SPACE": 10, "EOF": 11, "GRAPH": 12, "NODIR": 13, "DIR": 14, "FirstStmtSeparator": 15, "ending": 16, "endToken": 17, "spaceList": 18, "spaceListNewline": 19, "vertexStatement": 20, "separator": 21, "styleStatement": 22, "linkStyleStatement": 23, "classDefStatement": 24, "classStatement": 25, "clickStatement": 26, "subgraph": 27, "textNoTags": 28, "SQS": 29, "text": 30, "SQE": 31, "end": 32, "direction": 33, "acc_title": 34, "acc_title_value": 35, "acc_descr": 36, "acc_descr_value": 37, "acc_descr_multiline_value": 38, "shapeData": 39, "SHAPE_DATA": 40, "link": 41, "node": 42, "styledVertex": 43, "AMP": 44, "vertex": 45, "STYLE_SEPARATOR": 46, "idString": 47, "DOUBLECIRCLESTART": 48, "DOUBLECIRCLEEND": 49, "PS": 50, "PE": 51, "(-": 52, "-)": 53, "STADIUMSTART": 54, "STADIUMEND": 55, "SUBROUTINESTART": 56, "SUBROUTINEEND": 57, "VERTEX_WITH_PROPS_START": 58, "NODE_STRING[field]": 59, "COLON": 60, "NODE_STRING[value]": 61, "PIPE": 62, "CYLINDERSTART": 63, "CYLINDEREND": 64, "DIAMOND_START": 65, "DIAMOND_STOP": 66, "TAGEND": 67, "TRAPSTART": 68, "TRAPEND": 69, "INVTRAPSTART": 70, "INVTRAPEND": 71, "linkStatement": 72, "arrowText": 73, "TESTSTR": 74, "START_LINK": 75, "edgeText": 76, "LINK": 77, "LINK_ID": 78, "edgeTextToken": 79, "STR": 80, "MD_STR": 81, "textToken": 82, "keywords": 83, "STYLE": 84, "LINKSTYLE": 85, "CLASSDEF": 86, "CLASS": 87, "CLICK": 88, "DOWN": 89, "UP": 90, "textNoTagsToken": 91, "stylesOpt": 92, "idString[vertex]": 93, "idString[class]": 94, "CALLBACKNAME": 95, "CALLBACKARGS": 96, "HREF": 97, "LINK_TARGET": 98, "STR[link]": 99, "STR[tooltip]": 100, "alphaNum": 101, "DEFAULT": 102, "numList": 103, "INTERPOLATE": 104, "NUM": 105, "COMMA": 106, "style": 107, "styleComponent": 108, "NODE_STRING": 109, "UNIT": 110, "BRKT": 111, "PCT": 112, "idStringToken": 113, "MINUS": 114, "MULT": 115, "UNICODE_TEXT": 116, "TEXT": 117, "TAGSTART": 118, "EDGE_TEXT": 119, "alphaNumToken": 120, "direction_tb": 121, "direction_bt": 122, "direction_rl": 123, "direction_lr": 124, "direction_td": 125, "$accept": 0, "$end": 1 },
    terminals_: { 2: "error", 8: "SEMI", 9: "NEWLINE", 10: "SPACE", 11: "EOF", 12: "GRAPH", 13: "NODIR", 14: "DIR", 27: "subgraph", 29: "SQS", 31: "SQE", 32: "end", 34: "acc_title", 35: "acc_title_value", 36: "acc_descr", 37: "acc_descr_value", 38: "acc_descr_multiline_value", 40: "SHAPE_DATA", 44: "AMP", 46: "STYLE_SEPARATOR", 48: "DOUBLECIRCLESTART", 49: "DOUBLECIRCLEEND", 50: "PS", 51: "PE", 52: "(-", 53: "-)", 54: "STADIUMSTART", 55: "STADIUMEND", 56: "SUBROUTINESTART", 57: "SUBROUTINEEND", 58: "VERTEX_WITH_PROPS_START", 59: "NODE_STRING[field]", 60: "COLON", 61: "NODE_STRING[value]", 62: "PIPE", 63: "CYLINDERSTART", 64: "CYLINDEREND", 65: "DIAMOND_START", 66: "DIAMOND_STOP", 67: "TAGEND", 68: "TRAPSTART", 69: "TRAPEND", 70: "INVTRAPSTART", 71: "INVTRAPEND", 74: "TESTSTR", 75: "START_LINK", 77: "LINK", 78: "LINK_ID", 80: "STR", 81: "MD_STR", 84: "STYLE", 85: "LINKSTYLE", 86: "CLASSDEF", 87: "CLASS", 88: "CLICK", 89: "DOWN", 90: "UP", 93: "idString[vertex]", 94: "idString[class]", 95: "CALLBACKNAME", 96: "CALLBACKARGS", 97: "HREF", 98: "LINK_TARGET", 99: "STR[link]", 100: "STR[tooltip]", 102: "DEFAULT", 104: "INTERPOLATE", 105: "NUM", 106: "COMMA", 109: "NODE_STRING", 110: "UNIT", 111: "BRKT", 112: "PCT", 114: "MINUS", 115: "MULT", 116: "UNICODE_TEXT", 117: "TEXT", 118: "TAGSTART", 119: "EDGE_TEXT", 121: "direction_tb", 122: "direction_bt", 123: "direction_rl", 124: "direction_lr", 125: "direction_td" },
    productions_: [0, [3, 2], [5, 0], [5, 2], [6, 1], [6, 1], [6, 1], [6, 1], [6, 1], [4, 2], [4, 2], [4, 2], [4, 3], [16, 2], [16, 1], [17, 1], [17, 1], [17, 1], [15, 1], [15, 1], [15, 2], [19, 2], [19, 2], [19, 1], [19, 1], [18, 2], [18, 1], [7, 2], [7, 2], [7, 2], [7, 2], [7, 2], [7, 2], [7, 9], [7, 6], [7, 4], [7, 1], [7, 2], [7, 2], [7, 1], [21, 1], [21, 1], [21, 1], [39, 2], [39, 1], [20, 4], [20, 3], [20, 4], [20, 2], [20, 2], [20, 1], [42, 1], [42, 6], [42, 5], [43, 1], [43, 3], [45, 4], [45, 4], [45, 6], [45, 4], [45, 4], [45, 4], [45, 8], [45, 4], [45, 4], [45, 4], [45, 6], [45, 4], [45, 4], [45, 4], [45, 4], [45, 4], [45, 1], [41, 2], [41, 3], [41, 3], [41, 1], [41, 3], [41, 4], [76, 1], [76, 2], [76, 1], [76, 1], [72, 1], [72, 2], [73, 3], [30, 1], [30, 2], [30, 1], [30, 1], [83, 1], [83, 1], [83, 1], [83, 1], [83, 1], [83, 1], [83, 1], [83, 1], [83, 1], [83, 1], [83, 1], [28, 1], [28, 2], [28, 1], [28, 1], [24, 5], [25, 5], [26, 2], [26, 4], [26, 3], [26, 5], [26, 3], [26, 5], [26, 5], [26, 7], [26, 2], [26, 4], [26, 2], [26, 4], [26, 4], [26, 6], [22, 5], [23, 5], [23, 5], [23, 9], [23, 9], [23, 7], [23, 7], [103, 1], [103, 3], [92, 1], [92, 3], [107, 1], [107, 2], [108, 1], [108, 1], [108, 1], [108, 1], [108, 1], [108, 1], [108, 1], [108, 1], [113, 1], [113, 1], [113, 1], [113, 1], [113, 1], [113, 1], [113, 1], [113, 1], [113, 1], [113, 1], [113, 1], [82, 1], [82, 1], [82, 1], [82, 1], [91, 1], [91, 1], [91, 1], [91, 1], [91, 1], [91, 1], [91, 1], [91, 1], [91, 1], [91, 1], [91, 1], [79, 1], [79, 1], [120, 1], [120, 1], [120, 1], [120, 1], [120, 1], [120, 1], [120, 1], [120, 1], [120, 1], [120, 1], [120, 1], [47, 1], [47, 2], [101, 1], [101, 2], [33, 1], [33, 1], [33, 1], [33, 1], [33, 1]],
    performAction: /* @__PURE__ */ __name(function anonymous(yytext, yyleng, yylineno, yy, yystate, $$, _$) {
      var $0 = $$.length - 1;
      switch (yystate) {
        case 2:
          this.$ = [];
          break;
        case 3:
          if (!Array.isArray($$[$0]) || $$[$0].length > 0) {
            $$[$0 - 1].push($$[$0]);
          }
          this.$ = $$[$0 - 1];
          break;
        case 4:
        case 183:
          this.$ = $$[$0];
          break;
        case 11:
          yy.setDirection("TB");
          this.$ = "TB";
          break;
        case 12:
          yy.setDirection($$[$0 - 1]);
          this.$ = $$[$0 - 1];
          break;
        case 27:
          this.$ = $$[$0 - 1].nodes;
          break;
        case 28:
        case 29:
        case 30:
        case 31:
        case 32:
          this.$ = [];
          break;
        case 33:
          this.$ = yy.addSubGraph($$[$0 - 6], $$[$0 - 1], $$[$0 - 4]);
          break;
        case 34:
          this.$ = yy.addSubGraph($$[$0 - 3], $$[$0 - 1], $$[$0 - 3]);
          break;
        case 35:
          this.$ = yy.addSubGraph(void 0, $$[$0 - 1], void 0);
          break;
        case 37:
          this.$ = $$[$0].trim();
          yy.setAccTitle(this.$);
          break;
        case 38:
        case 39:
          this.$ = $$[$0].trim();
          yy.setAccDescription(this.$);
          break;
        case 43:
          this.$ = $$[$0 - 1] + $$[$0];
          break;
        case 44:
          this.$ = $$[$0];
          break;
        case 45:
          yy.addVertex($$[$0 - 1][$$[$0 - 1].length - 1], void 0, void 0, void 0, void 0, void 0, void 0, $$[$0]);
          yy.addLink($$[$0 - 3].stmt, $$[$0 - 1], $$[$0 - 2]);
          this.$ = { stmt: $$[$0 - 1], nodes: $$[$0 - 1].concat($$[$0 - 3].nodes) };
          break;
        case 46:
          yy.addLink($$[$0 - 2].stmt, $$[$0], $$[$0 - 1]);
          this.$ = { stmt: $$[$0], nodes: $$[$0].concat($$[$0 - 2].nodes) };
          break;
        case 47:
          yy.addLink($$[$0 - 3].stmt, $$[$0 - 1], $$[$0 - 2]);
          this.$ = { stmt: $$[$0 - 1], nodes: $$[$0 - 1].concat($$[$0 - 3].nodes) };
          break;
        case 48:
          this.$ = { stmt: $$[$0 - 1], nodes: $$[$0 - 1] };
          break;
        case 49:
          yy.addVertex($$[$0 - 1][$$[$0 - 1].length - 1], void 0, void 0, void 0, void 0, void 0, void 0, $$[$0]);
          this.$ = { stmt: $$[$0 - 1], nodes: $$[$0 - 1], shapeData: $$[$0] };
          break;
        case 50:
          this.$ = { stmt: $$[$0], nodes: $$[$0] };
          break;
        case 51:
          this.$ = [$$[$0]];
          break;
        case 52:
          yy.addVertex($$[$0 - 5][$$[$0 - 5].length - 1], void 0, void 0, void 0, void 0, void 0, void 0, $$[$0 - 4]);
          this.$ = $$[$0 - 5].concat($$[$0]);
          break;
        case 53:
          this.$ = $$[$0 - 4].concat($$[$0]);
          break;
        case 54:
          this.$ = $$[$0];
          break;
        case 55:
          this.$ = $$[$0 - 2];
          yy.setClass($$[$0 - 2], $$[$0]);
          break;
        case 56:
          this.$ = $$[$0 - 3];
          yy.addVertex($$[$0 - 3], $$[$0 - 1], "square");
          break;
        case 57:
          this.$ = $$[$0 - 3];
          yy.addVertex($$[$0 - 3], $$[$0 - 1], "doublecircle");
          break;
        case 58:
          this.$ = $$[$0 - 5];
          yy.addVertex($$[$0 - 5], $$[$0 - 2], "circle");
          break;
        case 59:
          this.$ = $$[$0 - 3];
          yy.addVertex($$[$0 - 3], $$[$0 - 1], "ellipse");
          break;
        case 60:
          this.$ = $$[$0 - 3];
          yy.addVertex($$[$0 - 3], $$[$0 - 1], "stadium");
          break;
        case 61:
          this.$ = $$[$0 - 3];
          yy.addVertex($$[$0 - 3], $$[$0 - 1], "subroutine");
          break;
        case 62:
          this.$ = $$[$0 - 7];
          yy.addVertex($$[$0 - 7], $$[$0 - 1], "rect", void 0, void 0, void 0, Object.fromEntries([[$$[$0 - 5], $$[$0 - 3]]]));
          break;
        case 63:
          this.$ = $$[$0 - 3];
          yy.addVertex($$[$0 - 3], $$[$0 - 1], "cylinder");
          break;
        case 64:
          this.$ = $$[$0 - 3];
          yy.addVertex($$[$0 - 3], $$[$0 - 1], "round");
          break;
        case 65:
          this.$ = $$[$0 - 3];
          yy.addVertex($$[$0 - 3], $$[$0 - 1], "diamond");
          break;
        case 66:
          this.$ = $$[$0 - 5];
          yy.addVertex($$[$0 - 5], $$[$0 - 2], "hexagon");
          break;
        case 67:
          this.$ = $$[$0 - 3];
          yy.addVertex($$[$0 - 3], $$[$0 - 1], "odd");
          break;
        case 68:
          this.$ = $$[$0 - 3];
          yy.addVertex($$[$0 - 3], $$[$0 - 1], "trapezoid");
          break;
        case 69:
          this.$ = $$[$0 - 3];
          yy.addVertex($$[$0 - 3], $$[$0 - 1], "inv_trapezoid");
          break;
        case 70:
          this.$ = $$[$0 - 3];
          yy.addVertex($$[$0 - 3], $$[$0 - 1], "lean_right");
          break;
        case 71:
          this.$ = $$[$0 - 3];
          yy.addVertex($$[$0 - 3], $$[$0 - 1], "lean_left");
          break;
        case 72:
          this.$ = $$[$0];
          yy.addVertex($$[$0]);
          break;
        case 73:
          $$[$0 - 1].text = $$[$0];
          this.$ = $$[$0 - 1];
          break;
        case 74:
        case 75:
          $$[$0 - 2].text = $$[$0 - 1];
          this.$ = $$[$0 - 2];
          break;
        case 76:
          this.$ = $$[$0];
          break;
        case 77:
          var inf = yy.destructLink($$[$0], $$[$0 - 2]);
          this.$ = { "type": inf.type, "stroke": inf.stroke, "length": inf.length, "text": $$[$0 - 1] };
          break;
        case 78:
          var inf = yy.destructLink($$[$0], $$[$0 - 2]);
          this.$ = { "type": inf.type, "stroke": inf.stroke, "length": inf.length, "text": $$[$0 - 1], "id": $$[$0 - 3] };
          break;
        case 79:
          this.$ = { text: $$[$0], type: "text" };
          break;
        case 80:
          this.$ = { text: $$[$0 - 1].text + "" + $$[$0], type: $$[$0 - 1].type };
          break;
        case 81:
          this.$ = { text: $$[$0], type: "string" };
          break;
        case 82:
          this.$ = { text: $$[$0], type: "markdown" };
          break;
        case 83:
          var inf = yy.destructLink($$[$0]);
          this.$ = { "type": inf.type, "stroke": inf.stroke, "length": inf.length };
          break;
        case 84:
          var inf = yy.destructLink($$[$0]);
          this.$ = { "type": inf.type, "stroke": inf.stroke, "length": inf.length, "id": $$[$0 - 1] };
          break;
        case 85:
          this.$ = $$[$0 - 1];
          break;
        case 86:
          this.$ = { text: $$[$0], type: "text" };
          break;
        case 87:
          this.$ = { text: $$[$0 - 1].text + "" + $$[$0], type: $$[$0 - 1].type };
          break;
        case 88:
          this.$ = { text: $$[$0], type: "string" };
          break;
        case 89:
        case 104:
          this.$ = { text: $$[$0], type: "markdown" };
          break;
        case 101:
          this.$ = { text: $$[$0], type: "text" };
          break;
        case 102:
          this.$ = { text: $$[$0 - 1].text + "" + $$[$0], type: $$[$0 - 1].type };
          break;
        case 103:
          this.$ = { text: $$[$0], type: "text" };
          break;
        case 105:
          this.$ = $$[$0 - 4];
          yy.addClass($$[$0 - 2], $$[$0]);
          break;
        case 106:
          this.$ = $$[$0 - 4];
          yy.setClass($$[$0 - 2], $$[$0]);
          break;
        case 107:
        case 115:
          this.$ = $$[$0 - 1];
          yy.setClickEvent($$[$0 - 1], $$[$0]);
          break;
        case 108:
        case 116:
          this.$ = $$[$0 - 3];
          yy.setClickEvent($$[$0 - 3], $$[$0 - 2]);
          yy.setTooltip($$[$0 - 3], $$[$0]);
          break;
        case 109:
          this.$ = $$[$0 - 2];
          yy.setClickEvent($$[$0 - 2], $$[$0 - 1], $$[$0]);
          break;
        case 110:
          this.$ = $$[$0 - 4];
          yy.setClickEvent($$[$0 - 4], $$[$0 - 3], $$[$0 - 2]);
          yy.setTooltip($$[$0 - 4], $$[$0]);
          break;
        case 111:
          this.$ = $$[$0 - 2];
          yy.setLink($$[$0 - 2], $$[$0]);
          break;
        case 112:
          this.$ = $$[$0 - 4];
          yy.setLink($$[$0 - 4], $$[$0 - 2]);
          yy.setTooltip($$[$0 - 4], $$[$0]);
          break;
        case 113:
          this.$ = $$[$0 - 4];
          yy.setLink($$[$0 - 4], $$[$0 - 2], $$[$0]);
          break;
        case 114:
          this.$ = $$[$0 - 6];
          yy.setLink($$[$0 - 6], $$[$0 - 4], $$[$0]);
          yy.setTooltip($$[$0 - 6], $$[$0 - 2]);
          break;
        case 117:
          this.$ = $$[$0 - 1];
          yy.setLink($$[$0 - 1], $$[$0]);
          break;
        case 118:
          this.$ = $$[$0 - 3];
          yy.setLink($$[$0 - 3], $$[$0 - 2]);
          yy.setTooltip($$[$0 - 3], $$[$0]);
          break;
        case 119:
          this.$ = $$[$0 - 3];
          yy.setLink($$[$0 - 3], $$[$0 - 2], $$[$0]);
          break;
        case 120:
          this.$ = $$[$0 - 5];
          yy.setLink($$[$0 - 5], $$[$0 - 4], $$[$0]);
          yy.setTooltip($$[$0 - 5], $$[$0 - 2]);
          break;
        case 121:
          this.$ = $$[$0 - 4];
          yy.addVertex($$[$0 - 2], void 0, void 0, $$[$0]);
          break;
        case 122:
          this.$ = $$[$0 - 4];
          yy.updateLink([$$[$0 - 2]], $$[$0]);
          break;
        case 123:
          this.$ = $$[$0 - 4];
          yy.updateLink($$[$0 - 2], $$[$0]);
          break;
        case 124:
          this.$ = $$[$0 - 8];
          yy.updateLinkInterpolate([$$[$0 - 6]], $$[$0 - 2]);
          yy.updateLink([$$[$0 - 6]], $$[$0]);
          break;
        case 125:
          this.$ = $$[$0 - 8];
          yy.updateLinkInterpolate($$[$0 - 6], $$[$0 - 2]);
          yy.updateLink($$[$0 - 6], $$[$0]);
          break;
        case 126:
          this.$ = $$[$0 - 6];
          yy.updateLinkInterpolate([$$[$0 - 4]], $$[$0]);
          break;
        case 127:
          this.$ = $$[$0 - 6];
          yy.updateLinkInterpolate($$[$0 - 4], $$[$0]);
          break;
        case 128:
        case 130:
          this.$ = [$$[$0]];
          break;
        case 129:
        case 131:
          $$[$0 - 2].push($$[$0]);
          this.$ = $$[$0 - 2];
          break;
        case 133:
          this.$ = $$[$0 - 1] + $$[$0];
          break;
        case 181:
          this.$ = $$[$0];
          break;
        case 182:
          this.$ = $$[$0 - 1] + "" + $$[$0];
          break;
        case 184:
          this.$ = $$[$0 - 1] + "" + $$[$0];
          break;
        case 185:
          this.$ = { stmt: "dir", value: "TB" };
          break;
        case 186:
          this.$ = { stmt: "dir", value: "BT" };
          break;
        case 187:
          this.$ = { stmt: "dir", value: "RL" };
          break;
        case 188:
          this.$ = { stmt: "dir", value: "LR" };
          break;
        case 189:
          this.$ = { stmt: "dir", value: "TD" };
          break;
      }
    }, "anonymous"),
    table: [{ 3: 1, 4: 2, 9: $V0, 10: $V1, 12: $V2 }, { 1: [3] }, o($V3, $V4, { 5: 6 }), { 4: 7, 9: $V0, 10: $V1, 12: $V2 }, { 4: 8, 9: $V0, 10: $V1, 12: $V2 }, { 13: [1, 9], 14: [1, 10] }, { 1: [2, 1], 6: 11, 7: 12, 8: $V5, 9: $V6, 10: $V7, 11: $V8, 20: 17, 22: 18, 23: 19, 24: 20, 25: 21, 26: 22, 27: $V9, 33: 24, 34: $Va, 36: $Vb, 38: $Vc, 42: 28, 43: 39, 44: $Vd, 45: 40, 47: 41, 60: $Ve, 84: $Vf, 85: $Vg, 86: $Vh, 87: $Vi, 88: $Vj, 89: $Vk, 102: $Vl, 105: $Vm, 106: $Vn, 109: $Vo, 111: $Vp, 113: 42, 114: $Vq, 115: $Vr, 116: $Vs, 121: $Vt, 122: $Vu, 123: $Vv, 124: $Vw, 125: $Vx }, o($V3, [2, 9]), o($V3, [2, 10]), o($V3, [2, 11]), { 8: [1, 55], 9: [1, 56], 10: $Vy, 15: 54, 18: 57 }, o($Vz, [2, 3]), o($Vz, [2, 4]), o($Vz, [2, 5]), o($Vz, [2, 6]), o($Vz, [2, 7]), o($Vz, [2, 8]), { 8: $VA, 9: $VB, 11: $VC, 21: 59, 41: 60, 72: 64, 75: [1, 65], 77: [1, 67], 78: [1, 66] }, { 8: $VA, 9: $VB, 11: $VC, 21: 68 }, { 8: $VA, 9: $VB, 11: $VC, 21: 69 }, { 8: $VA, 9: $VB, 11: $VC, 21: 70 }, { 8: $VA, 9: $VB, 11: $VC, 21: 71 }, { 8: $VA, 9: $VB, 11: $VC, 21: 72 }, { 8: $VA, 9: $VB, 10: [1, 73], 11: $VC, 21: 74 }, o($Vz, [2, 36]), { 35: [1, 75] }, { 37: [1, 76] }, o($Vz, [2, 39]), o($VD, [2, 50], { 18: 77, 39: 78, 10: $Vy, 40: $VE }), { 10: [1, 80] }, { 10: [1, 81] }, { 10: [1, 82] }, { 10: [1, 83] }, { 14: $VF, 44: $VG, 60: $VH, 80: [1, 87], 89: $VI, 95: [1, 84], 97: [1, 85], 101: 86, 105: $VJ, 106: $VK, 109: $VL, 111: $VM, 114: $VN, 115: $VO, 116: $VP, 120: 88 }, o($Vz, [2, 185]), o($Vz, [2, 186]), o($Vz, [2, 187]), o($Vz, [2, 188]), o($Vz, [2, 189]), o($VQ, [2, 51]), o($VQ, [2, 54], { 46: [1, 100] }), o($VR, [2, 72], { 113: 113, 29: [1, 101], 44: $Vd, 48: [1, 102], 50: [1, 103], 52: [1, 104], 54: [1, 105], 56: [1, 106], 58: [1, 107], 60: $Ve, 63: [1, 108], 65: [1, 109], 67: [1, 110], 68: [1, 111], 70: [1, 112], 89: $Vk, 102: $Vl, 105: $Vm, 106: $Vn, 109: $Vo, 111: $Vp, 114: $Vq, 115: $Vr, 116: $Vs }), o($VS, [2, 181]), o($VS, [2, 142]), o($VS, [2, 143]), o($VS, [2, 144]), o($VS, [2, 145]), o($VS, [2, 146]), o($VS, [2, 147]), o($VS, [2, 148]), o($VS, [2, 149]), o($VS, [2, 150]), o($VS, [2, 151]), o($VS, [2, 152]), o($V3, [2, 12]), o($V3, [2, 18]), o($V3, [2, 19]), { 9: [1, 114] }, o($VT, [2, 26], { 18: 115, 10: $Vy }), o($Vz, [2, 27]), { 42: 116, 43: 39, 44: $Vd, 45: 40, 47: 41, 60: $Ve, 89: $Vk, 102: $Vl, 105: $Vm, 106: $Vn, 109: $Vo, 111: $Vp, 113: 42, 114: $Vq, 115: $Vr, 116: $Vs }, o($Vz, [2, 40]), o($Vz, [2, 41]), o($Vz, [2, 42]), o($VU, [2, 76], { 73: 117, 62: [1, 119], 74: [1, 118] }), { 76: 120, 79: 121, 80: $VV, 81: $VW, 116: $VX, 119: $VY }, { 75: [1, 126], 77: [1, 127] }, o($VZ, [2, 83]), o($Vz, [2, 28]), o($Vz, [2, 29]), o($Vz, [2, 30]), o($Vz, [2, 31]), o($Vz, [2, 32]), { 10: $V_, 12: $V$, 14: $V01, 27: $V11, 28: 128, 32: $V21, 44: $V31, 60: $V41, 75: $V51, 80: [1, 130], 81: [1, 131], 83: 141, 84: $V61, 85: $V71, 86: $V81, 87: $V91, 88: $Va1, 89: $Vb1, 90: $Vc1, 91: 129, 105: $Vd1, 109: $Ve1, 111: $Vf1, 114: $Vg1, 115: $Vh1, 116: $Vi1 }, o($Vj1, $V4, { 5: 154 }), o($Vz, [2, 37]), o($Vz, [2, 38]), o($VD, [2, 48], { 44: $Vk1 }), o($VD, [2, 49], { 18: 156, 10: $Vy, 40: $Vl1 }), o($VQ, [2, 44]), { 44: $Vd, 47: 158, 60: $Ve, 89: $Vk, 102: $Vl, 105: $Vm, 106: $Vn, 109: $Vo, 111: $Vp, 113: 42, 114: $Vq, 115: $Vr, 116: $Vs }, { 102: [1, 159], 103: 160, 105: [1, 161] }, { 44: $Vd, 47: 162, 60: $Ve, 89: $Vk, 102: $Vl, 105: $Vm, 106: $Vn, 109: $Vo, 111: $Vp, 113: 42, 114: $Vq, 115: $Vr, 116: $Vs }, { 44: $Vd, 47: 163, 60: $Ve, 89: $Vk, 102: $Vl, 105: $Vm, 106: $Vn, 109: $Vo, 111: $Vp, 113: 42, 114: $Vq, 115: $Vr, 116: $Vs }, o($Vm1, [2, 107], { 10: [1, 164], 96: [1, 165] }), { 80: [1, 166] }, o($Vm1, [2, 115], { 120: 168, 10: [1, 167], 14: $VF, 44: $VG, 60: $VH, 89: $VI, 105: $VJ, 106: $VK, 109: $VL, 111: $VM, 114: $VN, 115: $VO, 116: $VP }), o($Vm1, [2, 117], { 10: [1, 169] }), o($Vn1, [2, 183]), o($Vn1, [2, 170]), o($Vn1, [2, 171]), o($Vn1, [2, 172]), o($Vn1, [2, 173]), o($Vn1, [2, 174]), o($Vn1, [2, 175]), o($Vn1, [2, 176]), o($Vn1, [2, 177]), o($Vn1, [2, 178]), o($Vn1, [2, 179]), o($Vn1, [2, 180]), { 44: $Vd, 47: 170, 60: $Ve, 89: $Vk, 102: $Vl, 105: $Vm, 106: $Vn, 109: $Vo, 111: $Vp, 113: 42, 114: $Vq, 115: $Vr, 116: $Vs }, { 30: 171, 67: $Vo1, 80: $Vp1, 81: $Vq1, 82: 172, 116: $Vr1, 117: $Vs1, 118: $Vt1 }, { 30: 179, 67: $Vo1, 80: $Vp1, 81: $Vq1, 82: 172, 116: $Vr1, 117: $Vs1, 118: $Vt1 }, { 30: 181, 50: [1, 180], 67: $Vo1, 80: $Vp1, 81: $Vq1, 82: 172, 116: $Vr1, 117: $Vs1, 118: $Vt1 }, { 30: 182, 67: $Vo1, 80: $Vp1, 81: $Vq1, 82: 172, 116: $Vr1, 117: $Vs1, 118: $Vt1 }, { 30: 183, 67: $Vo1, 80: $Vp1, 81: $Vq1, 82: 172, 116: $Vr1, 117: $Vs1, 118: $Vt1 }, { 30: 184, 67: $Vo1, 80: $Vp1, 81: $Vq1, 82: 172, 116: $Vr1, 117: $Vs1, 118: $Vt1 }, { 109: [1, 185] }, { 30: 186, 67: $Vo1, 80: $Vp1, 81: $Vq1, 82: 172, 116: $Vr1, 117: $Vs1, 118: $Vt1 }, { 30: 187, 65: [1, 188], 67: $Vo1, 80: $Vp1, 81: $Vq1, 82: 172, 116: $Vr1, 117: $Vs1, 118: $Vt1 }, { 30: 189, 67: $Vo1, 80: $Vp1, 81: $Vq1, 82: 172, 116: $Vr1, 117: $Vs1, 118: $Vt1 }, { 30: 190, 67: $Vo1, 80: $Vp1, 81: $Vq1, 82: 172, 116: $Vr1, 117: $Vs1, 118: $Vt1 }, { 30: 191, 67: $Vo1, 80: $Vp1, 81: $Vq1, 82: 172, 116: $Vr1, 117: $Vs1, 118: $Vt1 }, o($VS, [2, 182]), o($V3, [2, 20]), o($VT, [2, 25]), o($VD, [2, 46], { 39: 192, 18: 193, 10: $Vy, 40: $VE }), o($VU, [2, 73], { 10: [1, 194] }), { 10: [1, 195] }, { 30: 196, 67: $Vo1, 80: $Vp1, 81: $Vq1, 82: 172, 116: $Vr1, 117: $Vs1, 118: $Vt1 }, { 77: [1, 197], 79: 198, 116: $VX, 119: $VY }, o($Vu1, [2, 79]), o($Vu1, [2, 81]), o($Vu1, [2, 82]), o($Vu1, [2, 168]), o($Vu1, [2, 169]), { 76: 199, 79: 121, 80: $VV, 81: $VW, 116: $VX, 119: $VY }, o($VZ, [2, 84]), { 8: $VA, 9: $VB, 10: $V_, 11: $VC, 12: $V$, 14: $V01, 21: 201, 27: $V11, 29: [1, 200], 32: $V21, 44: $V31, 60: $V41, 75: $V51, 83: 141, 84: $V61, 85: $V71, 86: $V81, 87: $V91, 88: $Va1, 89: $Vb1, 90: $Vc1, 91: 202, 105: $Vd1, 109: $Ve1, 111: $Vf1, 114: $Vg1, 115: $Vh1, 116: $Vi1 }, o($Vv1, [2, 101]), o($Vv1, [2, 103]), o($Vv1, [2, 104]), o($Vv1, [2, 157]), o($Vv1, [2, 158]), o($Vv1, [2, 159]), o($Vv1, [2, 160]), o($Vv1, [2, 161]), o($Vv1, [2, 162]), o($Vv1, [2, 163]), o($Vv1, [2, 164]), o($Vv1, [2, 165]), o($Vv1, [2, 166]), o($Vv1, [2, 167]), o($Vv1, [2, 90]), o($Vv1, [2, 91]), o($Vv1, [2, 92]), o($Vv1, [2, 93]), o($Vv1, [2, 94]), o($Vv1, [2, 95]), o($Vv1, [2, 96]), o($Vv1, [2, 97]), o($Vv1, [2, 98]), o($Vv1, [2, 99]), o($Vv1, [2, 100]), { 6: 11, 7: 12, 8: $V5, 9: $V6, 10: $V7, 11: $V8, 20: 17, 22: 18, 23: 19, 24: 20, 25: 21, 26: 22, 27: $V9, 32: [1, 203], 33: 24, 34: $Va, 36: $Vb, 38: $Vc, 42: 28, 43: 39, 44: $Vd, 45: 40, 47: 41, 60: $Ve, 84: $Vf, 85: $Vg, 86: $Vh, 87: $Vi, 88: $Vj, 89: $Vk, 102: $Vl, 105: $Vm, 106: $Vn, 109: $Vo, 111: $Vp, 113: 42, 114: $Vq, 115: $Vr, 116: $Vs, 121: $Vt, 122: $Vu, 123: $Vv, 124: $Vw, 125: $Vx }, { 10: $Vy, 18: 204 }, { 44: [1, 205] }, o($VQ, [2, 43]), { 10: [1, 206], 44: $Vd, 60: $Ve, 89: $Vk, 102: $Vl, 105: $Vm, 106: $Vn, 109: $Vo, 111: $Vp, 113: 113, 114: $Vq, 115: $Vr, 116: $Vs }, { 10: [1, 207] }, { 10: [1, 208], 106: [1, 209] }, o($Vw1, [2, 128]), { 10: [1, 210], 44: $Vd, 60: $Ve, 89: $Vk, 102: $Vl, 105: $Vm, 106: $Vn, 109: $Vo, 111: $Vp, 113: 113, 114: $Vq, 115: $Vr, 116: $Vs }, { 10: [1, 211], 44: $Vd, 60: $Ve, 89: $Vk, 102: $Vl, 105: $Vm, 106: $Vn, 109: $Vo, 111: $Vp, 113: 113, 114: $Vq, 115: $Vr, 116: $Vs }, { 80: [1, 212] }, o($Vm1, [2, 109], { 10: [1, 213] }), o($Vm1, [2, 111], { 10: [1, 214] }), { 80: [1, 215] }, o($Vn1, [2, 184]), { 80: [1, 216], 98: [1, 217] }, o($VQ, [2, 55], { 113: 113, 44: $Vd, 60: $Ve, 89: $Vk, 102: $Vl, 105: $Vm, 106: $Vn, 109: $Vo, 111: $Vp, 114: $Vq, 115: $Vr, 116: $Vs }), { 31: [1, 218], 67: $Vo1, 82: 219, 116: $Vr1, 117: $Vs1, 118: $Vt1 }, o($Vx1, [2, 86]), o($Vx1, [2, 88]), o($Vx1, [2, 89]), o($Vx1, [2, 153]), o($Vx1, [2, 154]), o($Vx1, [2, 155]), o($Vx1, [2, 156]), { 49: [1, 220], 67: $Vo1, 82: 219, 116: $Vr1, 117: $Vs1, 118: $Vt1 }, { 30: 221, 67: $Vo1, 80: $Vp1, 81: $Vq1, 82: 172, 116: $Vr1, 117: $Vs1, 118: $Vt1 }, { 51: [1, 222], 67: $Vo1, 82: 219, 116: $Vr1, 117: $Vs1, 118: $Vt1 }, { 53: [1, 223], 67: $Vo1, 82: 219, 116: $Vr1, 117: $Vs1, 118: $Vt1 }, { 55: [1, 224], 67: $Vo1, 82: 219, 116: $Vr1, 117: $Vs1, 118: $Vt1 }, { 57: [1, 225], 67: $Vo1, 82: 219, 116: $Vr1, 117: $Vs1, 118: $Vt1 }, { 60: [1, 226] }, { 64: [1, 227], 67: $Vo1, 82: 219, 116: $Vr1, 117: $Vs1, 118: $Vt1 }, { 66: [1, 228], 67: $Vo1, 82: 219, 116: $Vr1, 117: $Vs1, 118: $Vt1 }, { 30: 229, 67: $Vo1, 80: $Vp1, 81: $Vq1, 82: 172, 116: $Vr1, 117: $Vs1, 118: $Vt1 }, { 31: [1, 230], 67: $Vo1, 82: 219, 116: $Vr1, 117: $Vs1, 118: $Vt1 }, { 67: $Vo1, 69: [1, 231], 71: [1, 232], 82: 219, 116: $Vr1, 117: $Vs1, 118: $Vt1 }, { 67: $Vo1, 69: [1, 234], 71: [1, 233], 82: 219, 116: $Vr1, 117: $Vs1, 118: $Vt1 }, o($VD, [2, 45], { 18: 156, 10: $Vy, 40: $Vl1 }), o($VD, [2, 47], { 44: $Vk1 }), o($VU, [2, 75]), o($VU, [2, 74]), { 62: [1, 235], 67: $Vo1, 82: 219, 116: $Vr1, 117: $Vs1, 118: $Vt1 }, o($VU, [2, 77]), o($Vu1, [2, 80]), { 77: [1, 236], 79: 198, 116: $VX, 119: $VY }, { 30: 237, 67: $Vo1, 80: $Vp1, 81: $Vq1, 82: 172, 116: $Vr1, 117: $Vs1, 118: $Vt1 }, o($Vj1, $V4, { 5: 238 }), o($Vv1, [2, 102]), o($Vz, [2, 35]), { 43: 239, 44: $Vd, 45: 40, 47: 41, 60: $Ve, 89: $Vk, 102: $Vl, 105: $Vm, 106: $Vn, 109: $Vo, 111: $Vp, 113: 42, 114: $Vq, 115: $Vr, 116: $Vs }, { 10: $Vy, 18: 240 }, { 10: $Vy1, 60: $Vz1, 84: $VA1, 92: 241, 105: $VB1, 107: 242, 108: 243, 109: $VC1, 110: $VD1, 111: $VE1, 112: $VF1 }, { 10: $Vy1, 60: $Vz1, 84: $VA1, 92: 252, 104: [1, 253], 105: $VB1, 107: 242, 108: 243, 109: $VC1, 110: $VD1, 111: $VE1, 112: $VF1 }, { 10: $Vy1, 60: $Vz1, 84: $VA1, 92: 254, 104: [1, 255], 105: $VB1, 107: 242, 108: 243, 109: $VC1, 110: $VD1, 111: $VE1, 112: $VF1 }, { 105: [1, 256] }, { 10: $Vy1, 60: $Vz1, 84: $VA1, 92: 257, 105: $VB1, 107: 242, 108: 243, 109: $VC1, 110: $VD1, 111: $VE1, 112: $VF1 }, { 44: $Vd, 47: 258, 60: $Ve, 89: $Vk, 102: $Vl, 105: $Vm, 106: $Vn, 109: $Vo, 111: $Vp, 113: 42, 114: $Vq, 115: $Vr, 116: $Vs }, o($Vm1, [2, 108]), { 80: [1, 259] }, { 80: [1, 260], 98: [1, 261] }, o($Vm1, [2, 116]), o($Vm1, [2, 118], { 10: [1, 262] }), o($Vm1, [2, 119]), o($VR, [2, 56]), o($Vx1, [2, 87]), o($VR, [2, 57]), { 51: [1, 263], 67: $Vo1, 82: 219, 116: $Vr1, 117: $Vs1, 118: $Vt1 }, o($VR, [2, 64]), o($VR, [2, 59]), o($VR, [2, 60]), o($VR, [2, 61]), { 109: [1, 264] }, o($VR, [2, 63]), o($VR, [2, 65]), { 66: [1, 265], 67: $Vo1, 82: 219, 116: $Vr1, 117: $Vs1, 118: $Vt1 }, o($VR, [2, 67]), o($VR, [2, 68]), o($VR, [2, 70]), o($VR, [2, 69]), o($VR, [2, 71]), o([10, 44, 60, 89, 102, 105, 106, 109, 111, 114, 115, 116], [2, 85]), o($VU, [2, 78]), { 31: [1, 266], 67: $Vo1, 82: 219, 116: $Vr1, 117: $Vs1, 118: $Vt1 }, { 6: 11, 7: 12, 8: $V5, 9: $V6, 10: $V7, 11: $V8, 20: 17, 22: 18, 23: 19, 24: 20, 25: 21, 26: 22, 27: $V9, 32: [1, 267], 33: 24, 34: $Va, 36: $Vb, 38: $Vc, 42: 28, 43: 39, 44: $Vd, 45: 40, 47: 41, 60: $Ve, 84: $Vf, 85: $Vg, 86: $Vh, 87: $Vi, 88: $Vj, 89: $Vk, 102: $Vl, 105: $Vm, 106: $Vn, 109: $Vo, 111: $Vp, 113: 42, 114: $Vq, 115: $Vr, 116: $Vs, 121: $Vt, 122: $Vu, 123: $Vv, 124: $Vw, 125: $Vx }, o($VQ, [2, 53]), { 43: 268, 44: $Vd, 45: 40, 47: 41, 60: $Ve, 89: $Vk, 102: $Vl, 105: $Vm, 106: $Vn, 109: $Vo, 111: $Vp, 113: 42, 114: $Vq, 115: $Vr, 116: $Vs }, o($Vm1, [2, 121], { 106: $VG1 }), o($VH1, [2, 130], { 108: 270, 10: $Vy1, 60: $Vz1, 84: $VA1, 105: $VB1, 109: $VC1, 110: $VD1, 111: $VE1, 112: $VF1 }), o($VI1, [2, 132]), o($VI1, [2, 134]), o($VI1, [2, 135]), o($VI1, [2, 136]), o($VI1, [2, 137]), o($VI1, [2, 138]), o($VI1, [2, 139]), o($VI1, [2, 140]), o($VI1, [2, 141]), o($Vm1, [2, 122], { 106: $VG1 }), { 10: [1, 271] }, o($Vm1, [2, 123], { 106: $VG1 }), { 10: [1, 272] }, o($Vw1, [2, 129]), o($Vm1, [2, 105], { 106: $VG1 }), o($Vm1, [2, 106], { 113: 113, 44: $Vd, 60: $Ve, 89: $Vk, 102: $Vl, 105: $Vm, 106: $Vn, 109: $Vo, 111: $Vp, 114: $Vq, 115: $Vr, 116: $Vs }), o($Vm1, [2, 110]), o($Vm1, [2, 112], { 10: [1, 273] }), o($Vm1, [2, 113]), { 98: [1, 274] }, { 51: [1, 275] }, { 62: [1, 276] }, { 66: [1, 277] }, { 8: $VA, 9: $VB, 11: $VC, 21: 278 }, o($Vz, [2, 34]), o($VQ, [2, 52]), { 10: $Vy1, 60: $Vz1, 84: $VA1, 105: $VB1, 107: 279, 108: 243, 109: $VC1, 110: $VD1, 111: $VE1, 112: $VF1 }, o($VI1, [2, 133]), { 14: $VF, 44: $VG, 60: $VH, 89: $VI, 101: 280, 105: $VJ, 106: $VK, 109: $VL, 111: $VM, 114: $VN, 115: $VO, 116: $VP, 120: 88 }, { 14: $VF, 44: $VG, 60: $VH, 89: $VI, 101: 281, 105: $VJ, 106: $VK, 109: $VL, 111: $VM, 114: $VN, 115: $VO, 116: $VP, 120: 88 }, { 98: [1, 282] }, o($Vm1, [2, 120]), o($VR, [2, 58]), { 30: 283, 67: $Vo1, 80: $Vp1, 81: $Vq1, 82: 172, 116: $Vr1, 117: $Vs1, 118: $Vt1 }, o($VR, [2, 66]), o($Vj1, $V4, { 5: 284 }), o($VH1, [2, 131], { 108: 270, 10: $Vy1, 60: $Vz1, 84: $VA1, 105: $VB1, 109: $VC1, 110: $VD1, 111: $VE1, 112: $VF1 }), o($Vm1, [2, 126], { 120: 168, 10: [1, 285], 14: $VF, 44: $VG, 60: $VH, 89: $VI, 105: $VJ, 106: $VK, 109: $VL, 111: $VM, 114: $VN, 115: $VO, 116: $VP }), o($Vm1, [2, 127], { 120: 168, 10: [1, 286], 14: $VF, 44: $VG, 60: $VH, 89: $VI, 105: $VJ, 106: $VK, 109: $VL, 111: $VM, 114: $VN, 115: $VO, 116: $VP }), o($Vm1, [2, 114]), { 31: [1, 287], 67: $Vo1, 82: 219, 116: $Vr1, 117: $Vs1, 118: $Vt1 }, { 6: 11, 7: 12, 8: $V5, 9: $V6, 10: $V7, 11: $V8, 20: 17, 22: 18, 23: 19, 24: 20, 25: 21, 26: 22, 27: $V9, 32: [1, 288], 33: 24, 34: $Va, 36: $Vb, 38: $Vc, 42: 28, 43: 39, 44: $Vd, 45: 40, 47: 41, 60: $Ve, 84: $Vf, 85: $Vg, 86: $Vh, 87: $Vi, 88: $Vj, 89: $Vk, 102: $Vl, 105: $Vm, 106: $Vn, 109: $Vo, 111: $Vp, 113: 42, 114: $Vq, 115: $Vr, 116: $Vs, 121: $Vt, 122: $Vu, 123: $Vv, 124: $Vw, 125: $Vx }, { 10: $Vy1, 60: $Vz1, 84: $VA1, 92: 289, 105: $VB1, 107: 242, 108: 243, 109: $VC1, 110: $VD1, 111: $VE1, 112: $VF1 }, { 10: $Vy1, 60: $Vz1, 84: $VA1, 92: 290, 105: $VB1, 107: 242, 108: 243, 109: $VC1, 110: $VD1, 111: $VE1, 112: $VF1 }, o($VR, [2, 62]), o($Vz, [2, 33]), o($Vm1, [2, 124], { 106: $VG1 }), o($Vm1, [2, 125], { 106: $VG1 })],
    defaultActions: {},
    parseError: /* @__PURE__ */ __name(function parseError(str, hash) {
      if (hash.recoverable) {
        this.trace(str);
      } else {
        var error = new Error(str);
        error.hash = hash;
        throw error;
      }
    }, "parseError"),
    parse: /* @__PURE__ */ __name(function parse(input) {
      var self = this, stack = [0], tstack = [], vstack = [null], lstack = [], table = this.table, yytext = "", yylineno = 0, yyleng = 0, recovering = 0, TERROR = 2, EOF = 1;
      var args = lstack.slice.call(arguments, 1);
      var lexer2 = Object.create(this.lexer);
      var sharedState = { yy: {} };
      for (var k in this.yy) {
        if (Object.prototype.hasOwnProperty.call(this.yy, k)) {
          sharedState.yy[k] = this.yy[k];
        }
      }
      lexer2.setInput(input, sharedState.yy);
      sharedState.yy.lexer = lexer2;
      sharedState.yy.parser = this;
      if (typeof lexer2.yylloc == "undefined") {
        lexer2.yylloc = {};
      }
      var yyloc = lexer2.yylloc;
      lstack.push(yyloc);
      var ranges = lexer2.options && lexer2.options.ranges;
      if (typeof sharedState.yy.parseError === "function") {
        this.parseError = sharedState.yy.parseError;
      } else {
        this.parseError = Object.getPrototypeOf(this).parseError;
      }
      function popStack(n) {
        stack.length = stack.length - 2 * n;
        vstack.length = vstack.length - n;
        lstack.length = lstack.length - n;
      }
      __name(popStack, "popStack");
      function lex() {
        var token;
        token = tstack.pop() || lexer2.lex() || EOF;
        if (typeof token !== "number") {
          if (token instanceof Array) {
            tstack = token;
            token = tstack.pop();
          }
          token = self.symbols_[token] || token;
        }
        return token;
      }
      __name(lex, "lex");
      var symbol, preErrorSymbol, state, action, a, r, yyval = {}, p, len, newState, expected;
      while (true) {
        state = stack[stack.length - 1];
        if (this.defaultActions[state]) {
          action = this.defaultActions[state];
        } else {
          if (symbol === null || typeof symbol == "undefined") {
            symbol = lex();
          }
          action = table[state] && table[state][symbol];
        }
        if (typeof action === "undefined" || !action.length || !action[0]) {
          var errStr = "";
          expected = [];
          for (p in table[state]) {
            if (this.terminals_[p] && p > TERROR) {
              expected.push("'" + this.terminals_[p] + "'");
            }
          }
          if (lexer2.showPosition) {
            errStr = "Parse error on line " + (yylineno + 1) + ":\n" + lexer2.showPosition() + "\nExpecting " + expected.join(", ") + ", got '" + (this.terminals_[symbol] || symbol) + "'";
          } else {
            errStr = "Parse error on line " + (yylineno + 1) + ": Unexpected " + (symbol == EOF ? "end of input" : "'" + (this.terminals_[symbol] || symbol) + "'");
          }
          this.parseError(errStr, {
            text: lexer2.match,
            token: this.terminals_[symbol] || symbol,
            line: lexer2.yylineno,
            loc: yyloc,
            expected
          });
        }
        if (action[0] instanceof Array && action.length > 1) {
          throw new Error("Parse Error: multiple actions possible at state: " + state + ", token: " + symbol);
        }
        switch (action[0]) {
          case 1:
            stack.push(symbol);
            vstack.push(lexer2.yytext);
            lstack.push(lexer2.yylloc);
            stack.push(action[1]);
            symbol = null;
            if (!preErrorSymbol) {
              yyleng = lexer2.yyleng;
              yytext = lexer2.yytext;
              yylineno = lexer2.yylineno;
              yyloc = lexer2.yylloc;
              if (recovering > 0) {
                recovering--;
              }
            } else {
              symbol = preErrorSymbol;
              preErrorSymbol = null;
            }
            break;
          case 2:
            len = this.productions_[action[1]][1];
            yyval.$ = vstack[vstack.length - len];
            yyval._$ = {
              first_line: lstack[lstack.length - (len || 1)].first_line,
              last_line: lstack[lstack.length - 1].last_line,
              first_column: lstack[lstack.length - (len || 1)].first_column,
              last_column: lstack[lstack.length - 1].last_column
            };
            if (ranges) {
              yyval._$.range = [
                lstack[lstack.length - (len || 1)].range[0],
                lstack[lstack.length - 1].range[1]
              ];
            }
            r = this.performAction.apply(yyval, [
              yytext,
              yyleng,
              yylineno,
              sharedState.yy,
              action[1],
              vstack,
              lstack
            ].concat(args));
            if (typeof r !== "undefined") {
              return r;
            }
            if (len) {
              stack = stack.slice(0, -1 * len * 2);
              vstack = vstack.slice(0, -1 * len);
              lstack = lstack.slice(0, -1 * len);
            }
            stack.push(this.productions_[action[1]][0]);
            vstack.push(yyval.$);
            lstack.push(yyval._$);
            newState = table[stack[stack.length - 2]][stack[stack.length - 1]];
            stack.push(newState);
            break;
          case 3:
            return true;
        }
      }
      return true;
    }, "parse")
  };
  var lexer = /* @__PURE__ */ (function() {
    var lexer2 = {
      EOF: 1,
      parseError: /* @__PURE__ */ __name(function parseError(str, hash) {
        if (this.yy.parser) {
          this.yy.parser.parseError(str, hash);
        } else {
          throw new Error(str);
        }
      }, "parseError"),
      // resets the lexer, sets new input
      setInput: /* @__PURE__ */ __name(function(input, yy) {
        this.yy = yy || this.yy || {};
        this._input = input;
        this._more = this._backtrack = this.done = false;
        this.yylineno = this.yyleng = 0;
        this.yytext = this.matched = this.match = "";
        this.conditionStack = ["INITIAL"];
        this.yylloc = {
          first_line: 1,
          first_column: 0,
          last_line: 1,
          last_column: 0
        };
        if (this.options.ranges) {
          this.yylloc.range = [0, 0];
        }
        this.offset = 0;
        return this;
      }, "setInput"),
      // consumes and returns one char from the input
      input: /* @__PURE__ */ __name(function() {
        var ch = this._input[0];
        this.yytext += ch;
        this.yyleng++;
        this.offset++;
        this.match += ch;
        this.matched += ch;
        var lines = ch.match(/(?:\r\n?|\n).*/g);
        if (lines) {
          this.yylineno++;
          this.yylloc.last_line++;
        } else {
          this.yylloc.last_column++;
        }
        if (this.options.ranges) {
          this.yylloc.range[1]++;
        }
        this._input = this._input.slice(1);
        return ch;
      }, "input"),
      // unshifts one char (or a string) into the input
      unput: /* @__PURE__ */ __name(function(ch) {
        var len = ch.length;
        var lines = ch.split(/(?:\r\n?|\n)/g);
        this._input = ch + this._input;
        this.yytext = this.yytext.substr(0, this.yytext.length - len);
        this.offset -= len;
        var oldLines = this.match.split(/(?:\r\n?|\n)/g);
        this.match = this.match.substr(0, this.match.length - 1);
        this.matched = this.matched.substr(0, this.matched.length - 1);
        if (lines.length - 1) {
          this.yylineno -= lines.length - 1;
        }
        var r = this.yylloc.range;
        this.yylloc = {
          first_line: this.yylloc.first_line,
          last_line: this.yylineno + 1,
          first_column: this.yylloc.first_column,
          last_column: lines ? (lines.length === oldLines.length ? this.yylloc.first_column : 0) + oldLines[oldLines.length - lines.length].length - lines[0].length : this.yylloc.first_column - len
        };
        if (this.options.ranges) {
          this.yylloc.range = [r[0], r[0] + this.yyleng - len];
        }
        this.yyleng = this.yytext.length;
        return this;
      }, "unput"),
      // When called from action, caches matched text and appends it on next action
      more: /* @__PURE__ */ __name(function() {
        this._more = true;
        return this;
      }, "more"),
      // When called from action, signals the lexer that this rule fails to match the input, so the next matching rule (regex) should be tested instead.
      reject: /* @__PURE__ */ __name(function() {
        if (this.options.backtrack_lexer) {
          this._backtrack = true;
        } else {
          return this.parseError("Lexical error on line " + (this.yylineno + 1) + ". You can only invoke reject() in the lexer when the lexer is of the backtracking persuasion (options.backtrack_lexer = true).\n" + this.showPosition(), {
            text: "",
            token: null,
            line: this.yylineno
          });
        }
        return this;
      }, "reject"),
      // retain first n characters of the match
      less: /* @__PURE__ */ __name(function(n) {
        this.unput(this.match.slice(n));
      }, "less"),
      // displays already matched input, i.e. for error messages
      pastInput: /* @__PURE__ */ __name(function() {
        var past = this.matched.substr(0, this.matched.length - this.match.length);
        return (past.length > 20 ? "..." : "") + past.substr(-20).replace(/\n/g, "");
      }, "pastInput"),
      // displays upcoming input, i.e. for error messages
      upcomingInput: /* @__PURE__ */ __name(function() {
        var next = this.match;
        if (next.length < 20) {
          next += this._input.substr(0, 20 - next.length);
        }
        return (next.substr(0, 20) + (next.length > 20 ? "..." : "")).replace(/\n/g, "");
      }, "upcomingInput"),
      // displays the character position where the lexing error occurred, i.e. for error messages
      showPosition: /* @__PURE__ */ __name(function() {
        var pre = this.pastInput();
        var c = new Array(pre.length + 1).join("-");
        return pre + this.upcomingInput() + "\n" + c + "^";
      }, "showPosition"),
      // test the lexed token: return FALSE when not a match, otherwise return token
      test_match: /* @__PURE__ */ __name(function(match, indexed_rule) {
        var token, lines, backup;
        if (this.options.backtrack_lexer) {
          backup = {
            yylineno: this.yylineno,
            yylloc: {
              first_line: this.yylloc.first_line,
              last_line: this.last_line,
              first_column: this.yylloc.first_column,
              last_column: this.yylloc.last_column
            },
            yytext: this.yytext,
            match: this.match,
            matches: this.matches,
            matched: this.matched,
            yyleng: this.yyleng,
            offset: this.offset,
            _more: this._more,
            _input: this._input,
            yy: this.yy,
            conditionStack: this.conditionStack.slice(0),
            done: this.done
          };
          if (this.options.ranges) {
            backup.yylloc.range = this.yylloc.range.slice(0);
          }
        }
        lines = match[0].match(/(?:\r\n?|\n).*/g);
        if (lines) {
          this.yylineno += lines.length;
        }
        this.yylloc = {
          first_line: this.yylloc.last_line,
          last_line: this.yylineno + 1,
          first_column: this.yylloc.last_column,
          last_column: lines ? lines[lines.length - 1].length - lines[lines.length - 1].match(/\r?\n?/)[0].length : this.yylloc.last_column + match[0].length
        };
        this.yytext += match[0];
        this.match += match[0];
        this.matches = match;
        this.yyleng = this.yytext.length;
        if (this.options.ranges) {
          this.yylloc.range = [this.offset, this.offset += this.yyleng];
        }
        this._more = false;
        this._backtrack = false;
        this._input = this._input.slice(match[0].length);
        this.matched += match[0];
        token = this.performAction.call(this, this.yy, this, indexed_rule, this.conditionStack[this.conditionStack.length - 1]);
        if (this.done && this._input) {
          this.done = false;
        }
        if (token) {
          return token;
        } else if (this._backtrack) {
          for (var k in backup) {
            this[k] = backup[k];
          }
          return false;
        }
        return false;
      }, "test_match"),
      // return next match in input
      next: /* @__PURE__ */ __name(function() {
        if (this.done) {
          return this.EOF;
        }
        if (!this._input) {
          this.done = true;
        }
        var token, match, tempMatch, index;
        if (!this._more) {
          this.yytext = "";
          this.match = "";
        }
        var rules = this._currentRules();
        for (var i = 0; i < rules.length; i++) {
          tempMatch = this._input.match(this.rules[rules[i]]);
          if (tempMatch && (!match || tempMatch[0].length > match[0].length)) {
            match = tempMatch;
            index = i;
            if (this.options.backtrack_lexer) {
              token = this.test_match(tempMatch, rules[i]);
              if (token !== false) {
                return token;
              } else if (this._backtrack) {
                match = false;
                continue;
              } else {
                return false;
              }
            } else if (!this.options.flex) {
              break;
            }
          }
        }
        if (match) {
          token = this.test_match(match, rules[index]);
          if (token !== false) {
            return token;
          }
          return false;
        }
        if (this._input === "") {
          return this.EOF;
        } else {
          return this.parseError("Lexical error on line " + (this.yylineno + 1) + ". Unrecognized text.\n" + this.showPosition(), {
            text: "",
            token: null,
            line: this.yylineno
          });
        }
      }, "next"),
      // return next match that has a token
      lex: /* @__PURE__ */ __name(function lex() {
        var r = this.next();
        if (r) {
          return r;
        } else {
          return this.lex();
        }
      }, "lex"),
      // activates a new lexer condition state (pushes the new lexer condition state onto the condition stack)
      begin: /* @__PURE__ */ __name(function begin(condition) {
        this.conditionStack.push(condition);
      }, "begin"),
      // pop the previously active lexer condition state off the condition stack
      popState: /* @__PURE__ */ __name(function popState() {
        var n = this.conditionStack.length - 1;
        if (n > 0) {
          return this.conditionStack.pop();
        } else {
          return this.conditionStack[0];
        }
      }, "popState"),
      // produce the lexer rule set which is active for the currently active lexer condition state
      _currentRules: /* @__PURE__ */ __name(function _currentRules() {
        if (this.conditionStack.length && this.conditionStack[this.conditionStack.length - 1]) {
          return this.conditions[this.conditionStack[this.conditionStack.length - 1]].rules;
        } else {
          return this.conditions["INITIAL"].rules;
        }
      }, "_currentRules"),
      // return the currently active lexer condition state; when an index argument is provided it produces the N-th previous condition state, if available
      topState: /* @__PURE__ */ __name(function topState(n) {
        n = this.conditionStack.length - 1 - Math.abs(n || 0);
        if (n >= 0) {
          return this.conditionStack[n];
        } else {
          return "INITIAL";
        }
      }, "topState"),
      // alias for begin(condition)
      pushState: /* @__PURE__ */ __name(function pushState(condition) {
        this.begin(condition);
      }, "pushState"),
      // return the number of states currently on the stack
      stateStackSize: /* @__PURE__ */ __name(function stateStackSize() {
        return this.conditionStack.length;
      }, "stateStackSize"),
      options: {},
      performAction: /* @__PURE__ */ __name(function anonymous(yy, yy_, $avoiding_name_collisions, YY_START) {
        var YYSTATE = YY_START;
        switch ($avoiding_name_collisions) {
          case 0:
            this.begin("acc_title");
            return 34;
            break;
          case 1:
            this.popState();
            return "acc_title_value";
            break;
          case 2:
            this.begin("acc_descr");
            return 36;
            break;
          case 3:
            this.popState();
            return "acc_descr_value";
            break;
          case 4:
            this.begin("acc_descr_multiline");
            break;
          case 5:
            this.popState();
            break;
          case 6:
            return "acc_descr_multiline_value";
            break;
          case 7:
            this.pushState("shapeData");
            yy_.yytext = "";
            return 40;
            break;
          case 8:
            this.pushState("shapeDataStr");
            return 40;
            break;
          case 9:
            this.popState();
            return 40;
            break;
          case 10:
            const re = /\n\s*/g;
            yy_.yytext = yy_.yytext.replace(re, "<br/>");
            return 40;
            break;
          case 11:
            return 40;
            break;
          case 12:
            this.popState();
            break;
          case 13:
            this.begin("callbackname");
            break;
          case 14:
            this.popState();
            break;
          case 15:
            this.popState();
            this.begin("callbackargs");
            break;
          case 16:
            return 95;
            break;
          case 17:
            this.popState();
            break;
          case 18:
            return 96;
            break;
          case 19:
            return "MD_STR";
            break;
          case 20:
            this.popState();
            break;
          case 21:
            this.begin("md_string");
            break;
          case 22:
            return "STR";
            break;
          case 23:
            this.popState();
            break;
          case 24:
            this.pushState("string");
            break;
          case 25:
            return 84;
            break;
          case 26:
            return 102;
            break;
          case 27:
            return 85;
            break;
          case 28:
            return 104;
            break;
          case 29:
            return 86;
            break;
          case 30:
            return 87;
            break;
          case 31:
            return 97;
            break;
          case 32:
            this.begin("click");
            break;
          case 33:
            this.popState();
            break;
          case 34:
            return 88;
            break;
          case 35:
            if (yy.lex.firstGraph()) {
              this.begin("dir");
            }
            return 12;
            break;
          case 36:
            if (yy.lex.firstGraph()) {
              this.begin("dir");
            }
            return 12;
            break;
          case 37:
            if (yy.lex.firstGraph()) {
              this.begin("dir");
            }
            return 12;
            break;
          case 38:
            if (yy.lex.firstGraph()) {
              this.begin("dir");
            }
            return 12;
            break;
          case 39:
            return 27;
            break;
          case 40:
            return 32;
            break;
          case 41:
            return 98;
            break;
          case 42:
            return 98;
            break;
          case 43:
            return 98;
            break;
          case 44:
            return 98;
            break;
          case 45:
            this.popState();
            return 13;
            break;
          case 46:
            this.popState();
            return 14;
            break;
          case 47:
            this.popState();
            return 14;
            break;
          case 48:
            this.popState();
            return 14;
            break;
          case 49:
            this.popState();
            return 14;
            break;
          case 50:
            this.popState();
            return 14;
            break;
          case 51:
            this.popState();
            return 14;
            break;
          case 52:
            this.popState();
            return 14;
            break;
          case 53:
            this.popState();
            return 14;
            break;
          case 54:
            this.popState();
            return 14;
            break;
          case 55:
            this.popState();
            return 14;
            break;
          case 56:
            return 121;
            break;
          case 57:
            return 122;
            break;
          case 58:
            return 123;
            break;
          case 59:
            return 124;
            break;
          case 60:
            return 125;
            break;
          case 61:
            return 78;
            break;
          case 62:
            return 105;
            break;
          case 63:
            return 111;
            break;
          case 64:
            return 46;
            break;
          case 65:
            return 60;
            break;
          case 66:
            return 44;
            break;
          case 67:
            return 8;
            break;
          case 68:
            return 106;
            break;
          case 69:
            return 115;
            break;
          case 70:
            this.popState();
            return 77;
            break;
          case 71:
            this.pushState("edgeText");
            return 75;
            break;
          case 72:
            return 119;
            break;
          case 73:
            this.popState();
            return 77;
            break;
          case 74:
            this.pushState("thickEdgeText");
            return 75;
            break;
          case 75:
            return 119;
            break;
          case 76:
            this.popState();
            return 77;
            break;
          case 77:
            this.pushState("dottedEdgeText");
            return 75;
            break;
          case 78:
            return 119;
            break;
          case 79:
            return 77;
            break;
          case 80:
            this.popState();
            return 53;
            break;
          case 81:
            return "TEXT";
            break;
          case 82:
            this.pushState("ellipseText");
            return 52;
            break;
          case 83:
            this.popState();
            return 55;
            break;
          case 84:
            this.pushState("text");
            return 54;
            break;
          case 85:
            this.popState();
            return 57;
            break;
          case 86:
            this.pushState("text");
            return 56;
            break;
          case 87:
            return 58;
            break;
          case 88:
            this.pushState("text");
            return 67;
            break;
          case 89:
            this.popState();
            return 64;
            break;
          case 90:
            this.pushState("text");
            return 63;
            break;
          case 91:
            this.popState();
            return 49;
            break;
          case 92:
            this.pushState("text");
            return 48;
            break;
          case 93:
            this.popState();
            return 69;
            break;
          case 94:
            this.popState();
            return 71;
            break;
          case 95:
            return 117;
            break;
          case 96:
            this.pushState("trapText");
            return 68;
            break;
          case 97:
            this.pushState("trapText");
            return 70;
            break;
          case 98:
            return 118;
            break;
          case 99:
            return 67;
            break;
          case 100:
            return 90;
            break;
          case 101:
            return "SEP";
            break;
          case 102:
            return 89;
            break;
          case 103:
            return 115;
            break;
          case 104:
            return 111;
            break;
          case 105:
            return 44;
            break;
          case 106:
            return 109;
            break;
          case 107:
            return 114;
            break;
          case 108:
            return 116;
            break;
          case 109:
            this.popState();
            return 62;
            break;
          case 110:
            this.pushState("text");
            return 62;
            break;
          case 111:
            this.popState();
            return 51;
            break;
          case 112:
            this.pushState("text");
            return 50;
            break;
          case 113:
            this.popState();
            return 31;
            break;
          case 114:
            this.pushState("text");
            return 29;
            break;
          case 115:
            this.popState();
            return 66;
            break;
          case 116:
            this.pushState("text");
            return 65;
            break;
          case 117:
            return "TEXT";
            break;
          case 118:
            return "QUOTE";
            break;
          case 119:
            return 9;
            break;
          case 120:
            return 10;
            break;
          case 121:
            return 11;
            break;
        }
      }, "anonymous"),
      rules: [/^(?:accTitle\s*:\s*)/, /^(?:(?!\n||)*[^\n]*)/, /^(?:accDescr\s*:\s*)/, /^(?:(?!\n||)*[^\n]*)/, /^(?:accDescr\s*\{\s*)/, /^(?:[\}])/, /^(?:[^\}]*)/, /^(?:@\{)/, /^(?:["])/, /^(?:["])/, /^(?:[^\"]+)/, /^(?:[^}^"]+)/, /^(?:\})/, /^(?:call[\s]+)/, /^(?:\([\s]*\))/, /^(?:\()/, /^(?:[^(]*)/, /^(?:\))/, /^(?:[^)]*)/, /^(?:[^`"]+)/, /^(?:[`]["])/, /^(?:["][`])/, /^(?:[^"]+)/, /^(?:["])/, /^(?:["])/, /^(?:style\b)/, /^(?:default\b)/, /^(?:linkStyle\b)/, /^(?:interpolate\b)/, /^(?:classDef\b)/, /^(?:class\b)/, /^(?:href[\s])/, /^(?:click[\s]+)/, /^(?:[\s\n])/, /^(?:[^\s\n]*)/, /^(?:flowchart-elk\b)/, /^(?:swimlane-beta\b)/, /^(?:graph\b)/, /^(?:flowchart\b)/, /^(?:subgraph\b)/, /^(?:end\b\s*)/, /^(?:_self\b)/, /^(?:_blank\b)/, /^(?:_parent\b)/, /^(?:_top\b)/, /^(?:(\r?\n)*\s*\n)/, /^(?:\s*LR\b)/, /^(?:\s*RL\b)/, /^(?:\s*TB\b)/, /^(?:\s*BT\b)/, /^(?:\s*TD\b)/, /^(?:\s*BR\b)/, /^(?:\s*<)/, /^(?:\s*>)/, /^(?:\s*\^)/, /^(?:\s*v\b)/, /^(?:.*direction\s+TB[^\n]*)/, /^(?:.*direction\s+BT[^\n]*)/, /^(?:.*direction\s+RL[^\n]*)/, /^(?:.*direction\s+LR[^\n]*)/, /^(?:.*direction\s+TD[^\n]*)/, /^(?:[^\s\"]+@(?=[^\{\"]))/, /^(?:[0-9]+)/, /^(?:#)/, /^(?::::)/, /^(?::)/, /^(?:&)/, /^(?:;)/, /^(?:,)/, /^(?:\*)/, /^(?:\s*[xo<]?--+[-xo>]\s*)/, /^(?:\s*[xo<]?--\s*)/, /^(?:[^-]|-(?!-)+)/, /^(?:\s*[xo<]?==+[=xo>]\s*)/, /^(?:\s*[xo<]?==\s*)/, /^(?:[^=]|=(?!))/, /^(?:\s*[xo<]?-?\.+-[xo>]?\s*)/, /^(?:\s*[xo<]?-\.\s*)/, /^(?:[^\.]|\.(?!))/, /^(?:\s*~~[\~]+\s*)/, /^(?:[-/\)][\)])/, /^(?:[^\(\)\[\]\{\}]|!\)+)/, /^(?:\(-)/, /^(?:\]\))/, /^(?:\(\[)/, /^(?:\]\])/, /^(?:\[\[)/, /^(?:\[\|)/, /^(?:>)/, /^(?:\)\])/, /^(?:\[\()/, /^(?:\)\)\))/, /^(?:\(\(\()/, /^(?:[\\(?=\])][\]])/, /^(?:\/(?=\])\])/, /^(?:\/(?!\])|\\(?!\])|[^\\\[\]\(\)\{\}\/]+)/, /^(?:\[\/)/, /^(?:\[\\)/, /^(?:<)/, /^(?:>)/, /^(?:\^)/, /^(?:\\\|)/, /^(?:v\b)/, /^(?:\*)/, /^(?:#)/, /^(?:&)/, /^(?:([A-Za-z0-9!"\#$%&'*+\.`?\\_\/]|-(?=[^\>\-\.])|(?!))+)/, /^(?:-)/, /^(?:[\u00AA\u00B5\u00BA\u00C0-\u00D6\u00D8-\u00F6]|[\u00F8-\u02C1\u02C6-\u02D1\u02E0-\u02E4\u02EC\u02EE\u0370-\u0374\u0376\u0377]|[\u037A-\u037D\u0386\u0388-\u038A\u038C\u038E-\u03A1\u03A3-\u03F5]|[\u03F7-\u0481\u048A-\u0527\u0531-\u0556\u0559\u0561-\u0587\u05D0-\u05EA]|[\u05F0-\u05F2\u0620-\u064A\u066E\u066F\u0671-\u06D3\u06D5\u06E5\u06E6\u06EE]|[\u06EF\u06FA-\u06FC\u06FF\u0710\u0712-\u072F\u074D-\u07A5\u07B1\u07CA-\u07EA]|[\u07F4\u07F5\u07FA\u0800-\u0815\u081A\u0824\u0828\u0840-\u0858\u08A0]|[\u08A2-\u08AC\u0904-\u0939\u093D\u0950\u0958-\u0961\u0971-\u0977]|[\u0979-\u097F\u0985-\u098C\u098F\u0990\u0993-\u09A8\u09AA-\u09B0\u09B2]|[\u09B6-\u09B9\u09BD\u09CE\u09DC\u09DD\u09DF-\u09E1\u09F0\u09F1\u0A05-\u0A0A]|[\u0A0F\u0A10\u0A13-\u0A28\u0A2A-\u0A30\u0A32\u0A33\u0A35\u0A36\u0A38\u0A39]|[\u0A59-\u0A5C\u0A5E\u0A72-\u0A74\u0A85-\u0A8D\u0A8F-\u0A91\u0A93-\u0AA8]|[\u0AAA-\u0AB0\u0AB2\u0AB3\u0AB5-\u0AB9\u0ABD\u0AD0\u0AE0\u0AE1\u0B05-\u0B0C]|[\u0B0F\u0B10\u0B13-\u0B28\u0B2A-\u0B30\u0B32\u0B33\u0B35-\u0B39\u0B3D\u0B5C]|[\u0B5D\u0B5F-\u0B61\u0B71\u0B83\u0B85-\u0B8A\u0B8E-\u0B90\u0B92-\u0B95\u0B99]|[\u0B9A\u0B9C\u0B9E\u0B9F\u0BA3\u0BA4\u0BA8-\u0BAA\u0BAE-\u0BB9\u0BD0]|[\u0C05-\u0C0C\u0C0E-\u0C10\u0C12-\u0C28\u0C2A-\u0C33\u0C35-\u0C39\u0C3D]|[\u0C58\u0C59\u0C60\u0C61\u0C85-\u0C8C\u0C8E-\u0C90\u0C92-\u0CA8\u0CAA-\u0CB3]|[\u0CB5-\u0CB9\u0CBD\u0CDE\u0CE0\u0CE1\u0CF1\u0CF2\u0D05-\u0D0C\u0D0E-\u0D10]|[\u0D12-\u0D3A\u0D3D\u0D4E\u0D60\u0D61\u0D7A-\u0D7F\u0D85-\u0D96\u0D9A-\u0DB1]|[\u0DB3-\u0DBB\u0DBD\u0DC0-\u0DC6\u0E01-\u0E30\u0E32\u0E33\u0E40-\u0E46\u0E81]|[\u0E82\u0E84\u0E87\u0E88\u0E8A\u0E8D\u0E94-\u0E97\u0E99-\u0E9F\u0EA1-\u0EA3]|[\u0EA5\u0EA7\u0EAA\u0EAB\u0EAD-\u0EB0\u0EB2\u0EB3\u0EBD\u0EC0-\u0EC4\u0EC6]|[\u0EDC-\u0EDF\u0F00\u0F40-\u0F47\u0F49-\u0F6C\u0F88-\u0F8C\u1000-\u102A]|[\u103F\u1050-\u1055\u105A-\u105D\u1061\u1065\u1066\u106E-\u1070\u1075-\u1081]|[\u108E\u10A0-\u10C5\u10C7\u10CD\u10D0-\u10FA\u10FC-\u1248\u124A-\u124D]|[\u1250-\u1256\u1258\u125A-\u125D\u1260-\u1288\u128A-\u128D\u1290-\u12B0]|[\u12B2-\u12B5\u12B8-\u12BE\u12C0\u12C2-\u12C5\u12C8-\u12D6\u12D8-\u1310]|[\u1312-\u1315\u1318-\u135A\u1380-\u138F\u13A0-\u13F4\u1401-\u166C]|[\u166F-\u167F\u1681-\u169A\u16A0-\u16EA\u1700-\u170C\u170E-\u1711]|[\u1720-\u1731\u1740-\u1751\u1760-\u176C\u176E-\u1770\u1780-\u17B3\u17D7]|[\u17DC\u1820-\u1877\u1880-\u18A8\u18AA\u18B0-\u18F5\u1900-\u191C]|[\u1950-\u196D\u1970-\u1974\u1980-\u19AB\u19C1-\u19C7\u1A00-\u1A16]|[\u1A20-\u1A54\u1AA7\u1B05-\u1B33\u1B45-\u1B4B\u1B83-\u1BA0\u1BAE\u1BAF]|[\u1BBA-\u1BE5\u1C00-\u1C23\u1C4D-\u1C4F\u1C5A-\u1C7D\u1CE9-\u1CEC]|[\u1CEE-\u1CF1\u1CF5\u1CF6\u1D00-\u1DBF\u1E00-\u1F15\u1F18-\u1F1D]|[\u1F20-\u1F45\u1F48-\u1F4D\u1F50-\u1F57\u1F59\u1F5B\u1F5D\u1F5F-\u1F7D]|[\u1F80-\u1FB4\u1FB6-\u1FBC\u1FBE\u1FC2-\u1FC4\u1FC6-\u1FCC\u1FD0-\u1FD3]|[\u1FD6-\u1FDB\u1FE0-\u1FEC\u1FF2-\u1FF4\u1FF6-\u1FFC\u2071\u207F]|[\u2090-\u209C\u2102\u2107\u210A-\u2113\u2115\u2119-\u211D\u2124\u2126\u2128]|[\u212A-\u212D\u212F-\u2139\u213C-\u213F\u2145-\u2149\u214E\u2183\u2184]|[\u2C00-\u2C2E\u2C30-\u2C5E\u2C60-\u2CE4\u2CEB-\u2CEE\u2CF2\u2CF3]|[\u2D00-\u2D25\u2D27\u2D2D\u2D30-\u2D67\u2D6F\u2D80-\u2D96\u2DA0-\u2DA6]|[\u2DA8-\u2DAE\u2DB0-\u2DB6\u2DB8-\u2DBE\u2DC0-\u2DC6\u2DC8-\u2DCE]|[\u2DD0-\u2DD6\u2DD8-\u2DDE\u2E2F\u3005\u3006\u3031-\u3035\u303B\u303C]|[\u3041-\u3096\u309D-\u309F\u30A1-\u30FA\u30FC-\u30FF\u3105-\u312D]|[\u3131-\u318E\u31A0-\u31BA\u31F0-\u31FF\u3400-\u4DB5\u4E00-\u9FCC]|[\uA000-\uA48C\uA4D0-\uA4FD\uA500-\uA60C\uA610-\uA61F\uA62A\uA62B]|[\uA640-\uA66E\uA67F-\uA697\uA6A0-\uA6E5\uA717-\uA71F\uA722-\uA788]|[\uA78B-\uA78E\uA790-\uA793\uA7A0-\uA7AA\uA7F8-\uA801\uA803-\uA805]|[\uA807-\uA80A\uA80C-\uA822\uA840-\uA873\uA882-\uA8B3\uA8F2-\uA8F7\uA8FB]|[\uA90A-\uA925\uA930-\uA946\uA960-\uA97C\uA984-\uA9B2\uA9CF\uAA00-\uAA28]|[\uAA40-\uAA42\uAA44-\uAA4B\uAA60-\uAA76\uAA7A\uAA80-\uAAAF\uAAB1\uAAB5]|[\uAAB6\uAAB9-\uAABD\uAAC0\uAAC2\uAADB-\uAADD\uAAE0-\uAAEA\uAAF2-\uAAF4]|[\uAB01-\uAB06\uAB09-\uAB0E\uAB11-\uAB16\uAB20-\uAB26\uAB28-\uAB2E]|[\uABC0-\uABE2\uAC00-\uD7A3\uD7B0-\uD7C6\uD7CB-\uD7FB\uF900-\uFA6D]|[\uFA70-\uFAD9\uFB00-\uFB06\uFB13-\uFB17\uFB1D\uFB1F-\uFB28\uFB2A-\uFB36]|[\uFB38-\uFB3C\uFB3E\uFB40\uFB41\uFB43\uFB44\uFB46-\uFBB1\uFBD3-\uFD3D]|[\uFD50-\uFD8F\uFD92-\uFDC7\uFDF0-\uFDFB\uFE70-\uFE74\uFE76-\uFEFC]|[\uFF21-\uFF3A\uFF41-\uFF5A\uFF66-\uFFBE\uFFC2-\uFFC7\uFFCA-\uFFCF]|[\uFFD2-\uFFD7\uFFDA-\uFFDC])/, /^(?:\|)/, /^(?:\|)/, /^(?:\))/, /^(?:\()/, /^(?:\])/, /^(?:\[)/, /^(?:(\}))/, /^(?:\{)/, /^(?:[^\[\]\(\)\{\}\|\"]+)/, /^(?:")/, /^(?:(\r?\n)+)/, /^(?:\s)/, /^(?:$)/],
      conditions: { "shapeDataEndBracket": { "rules": [21, 24, 79, 82, 84, 86, 90, 92, 96, 97, 110, 112, 114, 116], "inclusive": false }, "shapeDataStr": { "rules": [9, 10, 21, 24, 79, 82, 84, 86, 90, 92, 96, 97, 110, 112, 114, 116], "inclusive": false }, "shapeData": { "rules": [8, 11, 12, 21, 24, 79, 82, 84, 86, 90, 92, 96, 97, 110, 112, 114, 116], "inclusive": false }, "callbackargs": { "rules": [17, 18, 21, 24, 79, 82, 84, 86, 90, 92, 96, 97, 110, 112, 114, 116], "inclusive": false }, "callbackname": { "rules": [14, 15, 16, 21, 24, 79, 82, 84, 86, 90, 92, 96, 97, 110, 112, 114, 116], "inclusive": false }, "href": { "rules": [21, 24, 79, 82, 84, 86, 90, 92, 96, 97, 110, 112, 114, 116], "inclusive": false }, "click": { "rules": [21, 24, 33, 34, 79, 82, 84, 86, 90, 92, 96, 97, 110, 112, 114, 116], "inclusive": false }, "dottedEdgeText": { "rules": [21, 24, 76, 78, 79, 82, 84, 86, 90, 92, 96, 97, 110, 112, 114, 116], "inclusive": false }, "thickEdgeText": { "rules": [21, 24, 73, 75, 79, 82, 84, 86, 90, 92, 96, 97, 110, 112, 114, 116], "inclusive": false }, "edgeText": { "rules": [21, 24, 70, 72, 79, 82, 84, 86, 90, 92, 96, 97, 110, 112, 114, 116], "inclusive": false }, "trapText": { "rules": [21, 24, 79, 82, 84, 86, 90, 92, 93, 94, 95, 96, 97, 110, 112, 114, 116], "inclusive": false }, "ellipseText": { "rules": [21, 24, 79, 80, 81, 82, 84, 86, 90, 92, 96, 97, 110, 112, 114, 116], "inclusive": false }, "text": { "rules": [21, 24, 79, 82, 83, 84, 85, 86, 89, 90, 91, 92, 96, 97, 109, 110, 111, 112, 113, 114, 115, 116, 117], "inclusive": false }, "vertex": { "rules": [21, 24, 79, 82, 84, 86, 90, 92, 96, 97, 110, 112, 114, 116], "inclusive": false }, "dir": { "rules": [21, 24, 45, 46, 47, 48, 49, 50, 51, 52, 53, 54, 55, 79, 82, 84, 86, 90, 92, 96, 97, 110, 112, 114, 116], "inclusive": false }, "acc_descr_multiline": { "rules": [5, 6, 21, 24, 79, 82, 84, 86, 90, 92, 96, 97, 110, 112, 114, 116], "inclusive": false }, "acc_descr": { "rules": [3, 21, 24, 79, 82, 84, 86, 90, 92, 96, 97, 110, 112, 114, 116], "inclusive": false }, "acc_title": { "rules": [1, 21, 24, 79, 82, 84, 86, 90, 92, 96, 97, 110, 112, 114, 116], "inclusive": false }, "md_string": { "rules": [19, 20, 21, 24, 79, 82, 84, 86, 90, 92, 96, 97, 110, 112, 114, 116], "inclusive": false }, "string": { "rules": [21, 22, 23, 24, 79, 82, 84, 86, 90, 92, 96, 97, 110, 112, 114, 116], "inclusive": false }, "INITIAL": { "rules": [0, 2, 4, 7, 13, 21, 24, 25, 26, 27, 28, 29, 30, 31, 32, 35, 36, 37, 38, 39, 40, 41, 42, 43, 44, 56, 57, 58, 59, 60, 61, 62, 63, 64, 65, 66, 67, 68, 69, 70, 71, 73, 74, 76, 77, 79, 82, 84, 86, 87, 88, 90, 92, 96, 97, 98, 99, 100, 101, 102, 103, 104, 105, 106, 107, 108, 110, 112, 114, 116, 118, 119, 120, 121], "inclusive": true } }
    };
    return lexer2;
  })();
  parser2.lexer = lexer;
  function Parser() {
    this.yy = {};
  }
  __name(Parser, "Parser");
  Parser.prototype = parser2;
  parser2.Parser = Parser;
  return new Parser();
})();
parser.parser = parser;
var flow_default = parser;

// src/diagrams/flowchart/parser/flowParser.ts
var newParser = Object.assign({}, flow_default);
newParser.parse = (src) => {
  const newSrc = src.replace(/}\s*\n/g, "}\n");
  return flow_default.parse(newSrc);
};
var flowParser_default = newParser;

// src/diagrams/flowchart/styles.ts
var fade = /* @__PURE__ */ __name((color, opacity) => {
  const channel = channel_default;
  const r = channel(color, "r");
  const g = channel(color, "g");
  const b = channel(color, "b");
  return rgba_default(r, g, b, opacity);
}, "fade");
var getStyles = /* @__PURE__ */ __name((options) => `.label {
    font-family: ${options.fontFamily};
    color: ${options.nodeTextColor || options.textColor};
  }
  .cluster-label text {
    fill: ${options.titleColor};
  }
  .cluster-label span {
    color: ${options.titleColor};
  }
  .cluster-label span p {
    background-color: transparent;
  }

  .label text,span {
    fill: ${options.nodeTextColor || options.textColor};
    color: ${options.nodeTextColor || options.textColor};
  }

  .node rect,
  .node circle,
  .node ellipse,
  .node polygon,
  .node path {
    fill: ${options.mainBkg};
    stroke: ${options.nodeBorder};
    stroke-width: ${options.strokeWidth ?? 1}px;
  }
  .rough-node .label text , .node .label text, .image-shape .label, .icon-shape .label {
    text-anchor: middle;
  }

  .node .katex path {
    fill: #000;
    stroke: #000;
    stroke-width: 1px;
  }

  .rough-node .label,.node .label, .image-shape .label, .icon-shape .label {
    text-align: center;
  }
  .node.clickable {
    cursor: pointer;
  }


  .root .anchor path {
    fill: ${options.lineColor} !important;
    stroke-width: 0;
    stroke: ${options.lineColor};
  }

  .arrowheadPath {
    fill: ${options.arrowheadColor};
  }

  .edgePath .path {
    stroke: ${options.lineColor};
    stroke-width: ${options.strokeWidth ?? 2}px;
  }

  .flowchart-link {
    stroke: ${options.lineColor};
    fill: none;
  }

  .edgeLabel {
    background-color: ${options.edgeLabelBackground};
    p {
      background-color: ${options.edgeLabelBackground};
    }
    rect {
      opacity: 0.5;
      background-color: ${options.edgeLabelBackground};
      fill: ${options.edgeLabelBackground};
    }
    text-align: center;
  }

  /* For html labels only */
  .labelBkg {
    background-color: ${fade(options.edgeLabelBackground, 0.5)};
    // background-color:
  }

  .cluster rect {
    fill: ${options.clusterBkg};
    stroke: ${options.clusterBorder};
    stroke-width: 1px;
  }

  .cluster text {
    fill: ${options.titleColor};
  }

  .cluster span {
    color: ${options.titleColor};
  }
  /* .cluster div {
    color: ${options.titleColor};
  } */

  div.mermaidTooltip {
    position: absolute;
    text-align: center;
    max-width: 200px;
    padding: 2px;
    font-family: ${options.fontFamily};
    font-size: 12px;
    background: ${options.tertiaryColor};
    border: 1px solid ${options.border2};
    border-radius: 2px;
    pointer-events: none;
    z-index: 100;
  }

  .flowchartTitleText {
    text-anchor: middle;
    font-size: 18px;
    fill: ${options.textColor};
  }

  rect.text {
    fill: none;
    stroke-width: 0;
  }

  .icon-shape, .image-shape {
    background-color: ${options.edgeLabelBackground};
    p {
      background-color: ${options.edgeLabelBackground};
      padding: 2px;
    }
    .label rect {
      opacity: 0.5;
      background-color: ${options.edgeLabelBackground};
      fill: ${options.edgeLabelBackground};
    }
    text-align: center;
  }
  ${getIconStyles()}
`, "getStyles");
var styles_default = getStyles;

// src/diagrams/flowchart/flowDiagram.ts
var createFlowDiagram = /* @__PURE__ */ __name(({
  defaultLayout,
  styles = styles_default
} = {}) => ({
  parser: flowParser_default,
  get db() {
    return new FlowDB();
  },
  renderer: flowRenderer_v3_unified_default,
  styles,
  init: /* @__PURE__ */ __name((cnf) => {
    if (!cnf.flowchart) {
      cnf.flowchart = {};
    }
    const layout = getUserDefinedConfig().layout ?? defaultLayout ?? cnf.layout;
    if (layout) {
      setConfig({ layout });
    }
    cnf.flowchart.arrowMarkerAbsolute = cnf.arrowMarkerAbsolute;
    setConfig({ flowchart: { arrowMarkerAbsolute: cnf.arrowMarkerAbsolute } });
  }, "init")
}), "createFlowDiagram");
var diagram = createFlowDiagram();

export {
  styles_default,
  createFlowDiagram,
  diagram
};
