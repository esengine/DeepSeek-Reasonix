import {
  populateCommonDb
} from "./chunk-JWPE2WC7.mjs";
import {
  selectSvgElement
} from "./chunk-VAUOI2AC.mjs";
import {
  cleanAndMerge
} from "./chunk-ICXQ74PX.mjs";
import {
  clear,
  configureSvgSize,
  getAccDescription,
  getAccTitle,
  getConfig,
  getConfig2,
  getDiagramTitle,
  getThemeVariables,
  setAccDescription,
  setAccTitle,
  setDiagramTitle
} from "./chunk-WYO6CB5R.mjs";
import {
  log
} from "./chunk-X3CZISLH.mjs";
import {
  __name
} from "./chunk-Y2CYZVJY.mjs";

// src/diagrams/wardley/wardleyParser.ts
import { parse } from "@mermaid-js/parser";
var toPercent = /* @__PURE__ */ __name((value, context) => {
  const normalized = value <= 1 ? value * 100 : value;
  if (normalized < 0 || normalized > 100) {
    throw new Error(
      `${context} must be between 0-1 (decimal) or 0-100 (percentage). Received: ${value}`
    );
  }
  return normalized;
}, "toPercent");
var toCoordinates = /* @__PURE__ */ __name((visibility, evolution, context) => {
  return {
    x: toPercent(evolution, `${context} evolution`),
    y: toPercent(visibility, `${context} visibility`)
  };
}, "toCoordinates");
var getFlowFromPort = /* @__PURE__ */ __name((port) => {
  if (!port) {
    return void 0;
  }
  if (port === "+<>") {
    return "bidirectional";
  }
  if (port === "+<") {
    return "backward";
  }
  if (port === "+>") {
    return "forward";
  }
  return void 0;
}, "getFlowFromPort");
var extractFlowFromArrow = /* @__PURE__ */ __name((arrow) => {
  if (!arrow?.startsWith("+")) {
    return {};
  }
  const labelMatch = /^\+'([^']*)'/.exec(arrow);
  const flowLabel = labelMatch?.[1];
  if (arrow.includes("<>")) {
    return { flow: "bidirectional", label: flowLabel };
  }
  if (arrow.includes("<")) {
    return { flow: "backward", label: flowLabel };
  }
  if (arrow.includes(">")) {
    return { flow: "forward", label: flowLabel };
  }
  return { label: flowLabel };
}, "extractFlowFromArrow");
var populateDb = /* @__PURE__ */ __name((ast, db) => {
  populateCommonDb(ast, db);
  if (ast.size) {
    db.setSize(ast.size.width, ast.size.height);
  }
  if (ast.evolution) {
    const stages = ast.evolution.stages.map((stage) => {
      if (stage.secondName) {
        return `${stage.name.trim()} / ${stage.secondName.trim()}`;
      }
      return stage.name.trim();
    });
    const stageBoundaries = ast.evolution.stages.filter((stage) => stage.boundary !== void 0).map((stage) => stage.boundary);
    db.updateAxes({ stages, stageBoundaries });
  }
  ast.anchors.forEach((anchor) => {
    const coords = toCoordinates(anchor.visibility, anchor.evolution, `Anchor "${anchor.name}"`);
    db.addNode(anchor.name, anchor.name, coords.x, coords.y, "anchor");
  });
  ast.components.forEach((component) => {
    const coords = toCoordinates(
      component.visibility,
      component.evolution,
      `Component "${component.name}"`
    );
    const labelOffsetX = component.label ? (component.label.negX ? -1 : 1) * component.label.offsetX : void 0;
    const labelOffsetY = component.label ? (component.label.negY ? -1 : 1) * component.label.offsetY : void 0;
    const sourceStrategy = component.decorator?.strategy;
    db.addNode(
      component.name,
      component.name,
      coords.x,
      coords.y,
      "component",
      labelOffsetX,
      labelOffsetY,
      component.inertia,
      sourceStrategy
    );
  });
  ast.notes.forEach((note) => {
    const coords = toCoordinates(note.visibility, note.evolution, `Note "${note.text}"`);
    db.addNote(note.text, coords.x, coords.y);
  });
  ast.pipelines.forEach((pipeline) => {
    const parentNode = db.getNode(pipeline.parent);
    if (!parentNode || typeof parentNode.y !== "number") {
      throw new Error(
        `Pipeline "${pipeline.parent}" must reference an existing component with coordinates.`
      );
    }
    const parentY = parentNode.y;
    db.startPipeline(pipeline.parent);
    pipeline.components.forEach((component) => {
      const componentId = `${pipeline.parent}_${component.name}`;
      const labelOffsetX = component.label ? (component.label.negX ? -1 : 1) * component.label.offsetX : void 0;
      const labelOffsetY = component.label ? (component.label.negY ? -1 : 1) * component.label.offsetY : void 0;
      const x = toPercent(component.evolution, `Pipeline component "${component.name}" evolution`);
      db.addNode(
        componentId,
        component.name,
        x,
        parentY,
        "pipeline-component",
        labelOffsetX,
        labelOffsetY
      );
      db.addPipelineComponent(pipeline.parent, componentId);
    });
  });
  ast.links.forEach((link) => {
    const isDashed = !!link.arrow && (link.arrow.includes("-.->") || link.arrow.includes(".-."));
    let flow = getFlowFromPort(link.fromPort) ?? getFlowFromPort(link.toPort);
    const { flow: arrowFlow, label: flowLabel } = extractFlowFromArrow(link.arrow);
    if (!flow && arrowFlow) {
      flow = arrowFlow;
    }
    const annotation = link.linkLabel;
    const label = flowLabel ?? annotation;
    db.addLink(db.resolveNodeId(link.from), db.resolveNodeId(link.to), isDashed, label, flow);
  });
  ast.evolves.forEach((evolve) => {
    const node = db.getNode(evolve.component);
    if (node?.y !== void 0) {
      const target = toPercent(evolve.target, `Evolve target for "${evolve.component}"`);
      db.addTrend(evolve.component, target, node.y);
    }
  });
  if (ast.annotations.length > 0) {
    const annotationsBox = ast.annotations[0];
    const coords = toCoordinates(annotationsBox.x, annotationsBox.y, "Annotations box");
    db.setAnnotationsBox(coords.x, coords.y);
  }
  ast.annotation.forEach((annotation) => {
    const coords = toCoordinates(annotation.x, annotation.y, `Annotation ${annotation.number}`);
    db.addAnnotation(annotation.number, [{ x: coords.x, y: coords.y }], annotation.text);
  });
  ast.accelerators.forEach((accelerator) => {
    const coords = toCoordinates(accelerator.x, accelerator.y, `Accelerator "${accelerator.name}"`);
    db.addAccelerator(accelerator.name, coords.x, coords.y);
  });
  ast.deaccelerators.forEach((deaccelerator) => {
    const coords = toCoordinates(
      deaccelerator.x,
      deaccelerator.y,
      `Deaccelerator "${deaccelerator.name}"`
    );
    db.addDeaccelerator(deaccelerator.name, coords.x, coords.y);
  });
}, "populateDb");
var parser = {
  parser: {
    // @ts-expect-error - WardleyDB is not assignable to DiagramDB
    yy: void 0
  },
  parse: /* @__PURE__ */ __name(async (input) => {
    const ast = await parse("wardley", input);
    log.debug(ast);
    const db = parser.parser?.yy;
    if (!db || typeof db.addNode !== "function") {
      throw new Error(
        "parser.parser?.yy was not a WardleyDB. This is due to a bug within Mermaid, please report this issue at https://github.com/mermaid-js/mermaid/issues."
      );
    }
    populateDb(ast, db);
  }, "parse")
};

// src/diagrams/wardley/wardleyBuilder.ts
var WardleyBuilder = class {
  constructor() {
    this.nodes = /* @__PURE__ */ new Map();
    this.links = [];
    this.trends = /* @__PURE__ */ new Map();
    this.pipelines = /* @__PURE__ */ new Map();
    this.annotations = [];
    this.notes = [];
    this.accelerators = [];
    this.deaccelerators = [];
    this.axes = {};
  }
  static {
    __name(this, "WardleyBuilder");
  }
  addNode(node) {
    const existing = this.nodes.get(node.id) ?? { id: node.id, label: node.label };
    const merged = {
      ...existing,
      ...node,
      className: node.className ?? existing.className,
      labelOffsetX: node.labelOffsetX ?? existing.labelOffsetX,
      labelOffsetY: node.labelOffsetY ?? existing.labelOffsetY
    };
    this.nodes.set(node.id, merged);
  }
  addLink(link) {
    this.links.push(link);
  }
  addTrend(trend) {
    this.trends.set(trend.nodeId, trend);
  }
  startPipeline(nodeId) {
    this.pipelines.set(nodeId, { nodeId, componentIds: [] });
    const node = this.nodes.get(nodeId);
    if (node) {
      node.isPipelineParent = true;
    }
  }
  addPipelineComponent(pipelineNodeId, componentId) {
    const pipeline = this.pipelines.get(pipelineNodeId);
    if (pipeline) {
      pipeline.componentIds.push(componentId);
    }
    const node = this.nodes.get(componentId);
    if (node) {
      node.inPipeline = true;
    }
  }
  addAnnotation(annotation) {
    this.annotations.push(annotation);
  }
  addNote(note) {
    this.notes.push(note);
  }
  addAccelerator(accelerator) {
    this.accelerators.push(accelerator);
  }
  addDeaccelerator(deaccelerator) {
    this.deaccelerators.push(deaccelerator);
  }
  setAnnotationsBox(x, y) {
    this.annotationsBox = { x, y };
  }
  setAxes(partial) {
    this.axes = {
      ...this.axes,
      ...partial
    };
  }
  setSize(width, height) {
    this.size = { width, height };
  }
  getNode(id) {
    return this.nodes.get(id);
  }
  /**
   * Resolve a name to a node ID. Tries exact ID match first,
   * then falls back to finding a node whose label matches the name
   * (handles pipeline components which have synthetic IDs like "Parent_Child").
   */
  resolveNodeId(name) {
    if (this.nodes.has(name)) {
      return name;
    }
    for (const [id, node] of this.nodes) {
      if (node.label === name) {
        return id;
      }
    }
    return name;
  }
  build() {
    const nodes = [];
    for (const node of this.nodes.values()) {
      if (typeof node.x !== "number" || typeof node.y !== "number") {
        throw new Error(`Node "${node.label}" is missing coordinates`);
      }
      nodes.push(node);
    }
    return {
      nodes,
      links: [...this.links],
      trends: [...this.trends.values()],
      pipelines: [...this.pipelines.values()],
      annotations: [...this.annotations],
      notes: [...this.notes],
      accelerators: [...this.accelerators],
      deaccelerators: [...this.deaccelerators],
      annotationsBox: this.annotationsBox,
      axes: { ...this.axes },
      size: this.size
    };
  }
  clear() {
    this.nodes.clear();
    this.links = [];
    this.trends.clear();
    this.pipelines.clear();
    this.annotations = [];
    this.notes = [];
    this.accelerators = [];
    this.deaccelerators = [];
    this.annotationsBox = void 0;
    this.axes = {};
    this.size = void 0;
  }
};

// src/diagrams/wardley/wardleyDb.ts
var builder = new WardleyBuilder();
function getConfig3() {
  return getConfig2()["wardley-beta"];
}
__name(getConfig3, "getConfig");
function addNode(id, label, x, y, className, labelOffsetX, labelOffsetY, inertia, sourceStrategy) {
  builder.addNode({
    id,
    label,
    x,
    y,
    className,
    labelOffsetX,
    labelOffsetY,
    inertia,
    sourceStrategy
  });
}
__name(addNode, "addNode");
function addLink(sourceId, targetId, dashed = false, label, flow) {
  builder.addLink({
    source: sourceId,
    target: targetId,
    dashed,
    label,
    flow
  });
}
__name(addLink, "addLink");
function addTrend(nodeId, targetX, targetY) {
  builder.addTrend({ nodeId, targetX, targetY });
}
__name(addTrend, "addTrend");
function addAnnotation(number, coordinates, text) {
  builder.addAnnotation({
    number,
    coordinates,
    text
  });
}
__name(addAnnotation, "addAnnotation");
function addNote(text, x, y) {
  builder.addNote({
    text,
    x,
    y
  });
}
__name(addNote, "addNote");
function addAccelerator(name, x, y) {
  builder.addAccelerator({
    name,
    x,
    y
  });
}
__name(addAccelerator, "addAccelerator");
function addDeaccelerator(name, x, y) {
  builder.addDeaccelerator({
    name,
    x,
    y
  });
}
__name(addDeaccelerator, "addDeaccelerator");
function setAnnotationsBox(x, y) {
  builder.setAnnotationsBox(x, y);
}
__name(setAnnotationsBox, "setAnnotationsBox");
function setSize(width, height) {
  builder.setSize(width, height);
}
__name(setSize, "setSize");
function startPipeline(nodeId) {
  builder.startPipeline(nodeId);
}
__name(startPipeline, "startPipeline");
function addPipelineComponent(pipelineNodeId, componentId) {
  builder.addPipelineComponent(pipelineNodeId, componentId);
}
__name(addPipelineComponent, "addPipelineComponent");
function updateAxes(partial) {
  builder.setAxes(partial);
}
__name(updateAxes, "updateAxes");
function getNode(id) {
  return builder.getNode(id);
}
__name(getNode, "getNode");
function resolveNodeId(name) {
  return builder.resolveNodeId(name);
}
__name(resolveNodeId, "resolveNodeId");
function getWardleyData() {
  return builder.build();
}
__name(getWardleyData, "getWardleyData");
function clear2() {
  builder.clear();
  clear();
}
__name(clear2, "clear");
var wardleyDb_default = {
  getConfig: getConfig3,
  addNode,
  addLink,
  addTrend,
  addAnnotation,
  addNote,
  addAccelerator,
  addDeaccelerator,
  setAnnotationsBox,
  setSize,
  startPipeline,
  addPipelineComponent,
  updateAxes,
  getNode,
  resolveNodeId,
  getWardleyData,
  clear: clear2,
  setAccTitle,
  getAccTitle,
  setDiagramTitle,
  getDiagramTitle,
  getAccDescription,
  setAccDescription
};

// src/diagrams/wardley/wardleyRenderer.ts
var DEFAULT_STAGES = ["Genesis", "Custom Built", "Product", "Commodity"];
var getTheme = /* @__PURE__ */ __name(() => {
  const { themeVariables } = getConfig2();
  return {
    backgroundColor: themeVariables.wardley?.backgroundColor ?? themeVariables.background ?? "#fff",
    axisColor: themeVariables.wardley?.axisColor ?? "#000",
    axisTextColor: themeVariables.wardley?.axisTextColor ?? themeVariables.primaryTextColor ?? "#222",
    gridColor: themeVariables.wardley?.gridColor ?? "rgba(100, 100, 100, 0.2)",
    componentFill: themeVariables.wardley?.componentFill ?? "#fff",
    componentStroke: themeVariables.wardley?.componentStroke ?? "#000",
    componentLabelColor: themeVariables.wardley?.componentLabelColor ?? themeVariables.primaryTextColor ?? "#222",
    linkStroke: themeVariables.wardley?.linkStroke ?? "#000",
    evolutionStroke: themeVariables.wardley?.evolutionStroke ?? "#dc3545",
    annotationStroke: themeVariables.wardley?.annotationStroke ?? "#000",
    annotationTextColor: themeVariables.wardley?.annotationTextColor ?? themeVariables.primaryTextColor ?? "#222",
    annotationFill: themeVariables.wardley?.annotationFill ?? themeVariables.background ?? "#fff"
  };
}, "getTheme");
var getConfigValues = /* @__PURE__ */ __name(() => {
  const wardleyConfig = getConfig2()["wardley-beta"];
  return {
    width: wardleyConfig?.width ?? 900,
    height: wardleyConfig?.height ?? 600,
    padding: wardleyConfig?.padding ?? 48,
    nodeRadius: wardleyConfig?.nodeRadius ?? 6,
    nodeLabelOffset: wardleyConfig?.nodeLabelOffset ?? 8,
    axisFontSize: wardleyConfig?.axisFontSize ?? 12,
    labelFontSize: wardleyConfig?.labelFontSize ?? 10,
    showGrid: wardleyConfig?.showGrid ?? false,
    useMaxWidth: wardleyConfig?.useMaxWidth ?? true
  };
}, "getConfigValues");
var draw = /* @__PURE__ */ __name((text, id, _version, diagObj) => {
  log.debug("Rendering Wardley map\n" + text);
  const configValues = getConfigValues();
  const theme = getTheme();
  const squareSize = configValues.nodeRadius * 1.6;
  const db = diagObj.db;
  const data = db.getWardleyData();
  const title = db.getDiagramTitle();
  const width = data.size?.width ?? configValues.width;
  const height = data.size?.height ?? configValues.height;
  const svg = selectSvgElement(id);
  svg.selectAll("*").remove();
  configureSvgSize(svg, height, width, configValues.useMaxWidth);
  svg.attr("viewBox", `0 0 ${width} ${height}`);
  const root = svg.append("g").attr("class", "wardley-map");
  const defs = svg.append("defs");
  defs.append("marker").attr("id", `arrow-${id}`).attr("viewBox", "0 0 10 10").attr("refX", 9).attr("refY", 5).attr("markerWidth", 6).attr("markerHeight", 6).attr("orient", "auto-start-reverse").append("path").attr("d", "M 0 0 L 10 5 L 0 10 z").attr("fill", theme.evolutionStroke).attr("stroke", "none");
  defs.append("marker").attr("id", `link-arrow-end-${id}`).attr("viewBox", "0 0 10 10").attr("refX", 9).attr("refY", 5).attr("markerWidth", 5).attr("markerHeight", 5).attr("orient", "auto").append("path").attr("d", "M 0 0 L 10 5 L 0 10 z").attr("fill", theme.linkStroke).attr("stroke", "none");
  defs.append("marker").attr("id", `link-arrow-start-${id}`).attr("viewBox", "0 0 10 10").attr("refX", 1).attr("refY", 5).attr("markerWidth", 5).attr("markerHeight", 5).attr("orient", "auto").append("path").attr("d", "M 10 0 L 0 5 L 10 10 z").attr("fill", theme.linkStroke).attr("stroke", "none");
  root.append("rect").attr("class", "wardley-background").attr("width", width).attr("height", height).attr("fill", theme.backgroundColor);
  const chartWidth = width - configValues.padding * 2;
  const chartHeight = height - configValues.padding * 2;
  if (title) {
    root.append("text").attr("class", "wardley-title").attr("x", width / 2).attr("y", configValues.padding / 2).attr("fill", theme.axisTextColor).attr("font-size", configValues.axisFontSize * 1.05).attr("font-weight", "bold").attr("text-anchor", "middle").attr("dominant-baseline", "middle").text(title);
  }
  const projectX = /* @__PURE__ */ __name((value) => configValues.padding + value / 100 * chartWidth, "projectX");
  const projectY = /* @__PURE__ */ __name((value) => height - configValues.padding - value / 100 * chartHeight, "projectY");
  const axisGroup = root.append("g").attr("class", "wardley-axes");
  axisGroup.append("line").attr("x1", configValues.padding).attr("x2", width - configValues.padding).attr("y1", height - configValues.padding).attr("y2", height - configValues.padding).attr("stroke", theme.axisColor).attr("stroke-width", 1);
  axisGroup.append("line").attr("x1", configValues.padding).attr("x2", configValues.padding).attr("y1", configValues.padding).attr("y2", height - configValues.padding).attr("stroke", theme.axisColor).attr("stroke-width", 1);
  const xLabel = data.axes.xLabel ?? "Evolution";
  const yLabel = data.axes.yLabel ?? "Visibility";
  axisGroup.append("text").attr("class", "wardley-axis-label wardley-axis-label-x").attr("x", configValues.padding + chartWidth / 2).attr("y", height - configValues.padding / 4).attr("fill", theme.axisTextColor).attr("font-size", configValues.axisFontSize).attr("font-weight", "bold").attr("text-anchor", "middle").text(xLabel);
  axisGroup.append("text").attr("class", "wardley-axis-label wardley-axis-label-y").attr("x", configValues.padding / 3).attr("y", configValues.padding + chartHeight / 2).attr("fill", theme.axisTextColor).attr("font-size", configValues.axisFontSize).attr("font-weight", "bold").attr("text-anchor", "middle").attr(
    "transform",
    `rotate(-90 ${configValues.padding / 3} ${configValues.padding + chartHeight / 2})`
  ).text(yLabel);
  const stages = data.axes.stages && data.axes.stages.length > 0 ? data.axes.stages : DEFAULT_STAGES;
  if (stages.length > 0) {
    const stageGroup = root.append("g").attr("class", "wardley-stages");
    const boundaries = data.axes.stageBoundaries;
    const stagePositions = [];
    if (boundaries && boundaries.length === stages.length) {
      let prevBoundary = 0;
      boundaries.forEach((boundary) => {
        stagePositions.push({ start: prevBoundary, end: boundary });
        prevBoundary = boundary;
      });
    } else {
      const stageWidth = 1 / stages.length;
      stages.forEach((_, index) => {
        stagePositions.push({
          start: index * stageWidth,
          end: (index + 1) * stageWidth
        });
      });
    }
    stages.forEach((stage, index) => {
      const pos = stagePositions[index];
      const startX = configValues.padding + pos.start * chartWidth;
      const endX = configValues.padding + pos.end * chartWidth;
      const centerX = (startX + endX) / 2;
      if (index > 0) {
        stageGroup.append("line").attr("x1", startX).attr("x2", startX).attr("y1", configValues.padding).attr("y2", height - configValues.padding).attr("stroke", "#000").attr("stroke-width", 1).attr("stroke-dasharray", "5 5").attr("opacity", 0.8);
      }
      stageGroup.append("text").attr("class", "wardley-stage-label").attr("x", centerX).attr("y", height - configValues.padding / 1.5).attr("fill", theme.axisTextColor).attr("font-size", configValues.axisFontSize - 2).attr("text-anchor", "middle").text(stage);
    });
  }
  if (configValues.showGrid) {
    const gridGroup = root.append("g").attr("class", "wardley-grid");
    for (let i = 1; i < 4; i++) {
      const ratio = i / 4;
      const x = configValues.padding + chartWidth * ratio;
      gridGroup.append("line").attr("x1", x).attr("x2", x).attr("y1", configValues.padding).attr("y2", height - configValues.padding).attr("stroke", theme.gridColor).attr("stroke-dasharray", "2 6");
      gridGroup.append("line").attr("x1", configValues.padding).attr("x2", width - configValues.padding).attr("y1", height - configValues.padding - chartHeight * ratio).attr("y2", height - configValues.padding - chartHeight * ratio).attr("stroke", theme.gridColor).attr("stroke-dasharray", "2 6");
    }
  }
  const positions = /* @__PURE__ */ new Map();
  data.nodes.forEach((node) => {
    positions.set(node.id, {
      x: projectX(node.x),
      y: projectY(node.y),
      node
    });
  });
  if (data.pipelines.length > 0) {
    const pipelineGroup = root.append("g").attr("class", "wardley-pipelines");
    const pipelineLinksGroup = root.append("g").attr("class", "wardley-pipeline-links");
    data.pipelines.forEach((pipeline) => {
      if (pipeline.componentIds.length === 0) {
        return;
      }
      const sortedComponents = pipeline.componentIds.map((id2) => ({ id: id2, pos: positions.get(id2), node: data.nodes.find((n) => n.id === id2) })).filter((c) => c.pos && c.node).sort((a, b) => a.node.x - b.node.x);
      for (let i = 0; i < sortedComponents.length - 1; i++) {
        const current = sortedComponents[i];
        const next = sortedComponents[i + 1];
        pipelineLinksGroup.append("line").attr("class", "wardley-pipeline-evolution-link").attr("x1", current.pos.x).attr("y1", current.pos.y).attr("x2", next.pos.x).attr("y2", next.pos.y).attr("stroke", theme.linkStroke).attr("stroke-width", 1).attr("stroke-dasharray", "4 4");
      }
      let minX = Infinity;
      let maxX = -Infinity;
      let y = 0;
      pipeline.componentIds.forEach((componentId) => {
        const pos = positions.get(componentId);
        if (pos) {
          minX = Math.min(minX, pos.x);
          maxX = Math.max(maxX, pos.x);
          y = pos.y;
        }
      });
      if (minX !== Infinity && maxX !== -Infinity) {
        const padding = 15;
        const height2 = configValues.nodeRadius * 4;
        const boxTop = y - height2 / 2;
        const parentPos = positions.get(pipeline.nodeId);
        if (parentPos) {
          const centerX = (minX + maxX) / 2;
          parentPos.x = centerX;
          parentPos.y = boxTop - squareSize / 6;
        }
        pipelineGroup.append("rect").attr("class", "wardley-pipeline-box").attr("x", minX - padding).attr("y", boxTop).attr("width", maxX - minX + padding * 2).attr("height", height2).attr("fill", "none").attr("stroke", theme.axisColor).attr("stroke-width", 1.5).attr("rx", 4).attr("ry", 4);
      }
    });
  }
  const linksGroup = root.append("g").attr("class", "wardley-links");
  const pipelineMap = /* @__PURE__ */ new Map();
  data.pipelines.forEach((pipeline) => {
    pipelineMap.set(pipeline.nodeId, new Set(pipeline.componentIds));
  });
  const validLinks = data.links.filter((link) => {
    if (!positions.has(link.source) || !positions.has(link.target)) {
      return false;
    }
    const pipelineComponents = pipelineMap.get(link.target);
    if (pipelineComponents?.has(link.source)) {
      return false;
    }
    return true;
  });
  linksGroup.selectAll("line").data(validLinks).enter().append("line").attr("class", (link) => `wardley-link${link.dashed ? " wardley-link--dashed" : ""}`).attr("x1", (link) => {
    const sourcePos = positions.get(link.source);
    const targetPos = positions.get(link.target);
    const sourceNode = data.nodes.find((n) => n.id === link.source);
    const radius = sourceNode.isPipelineParent ? squareSize / Math.sqrt(2) : configValues.nodeRadius;
    const dx = targetPos.x - sourcePos.x;
    const dy = targetPos.y - sourcePos.y;
    const distance = Math.sqrt(dx * dx + dy * dy);
    return sourcePos.x + dx / distance * radius;
  }).attr("y1", (link) => {
    const sourcePos = positions.get(link.source);
    const targetPos = positions.get(link.target);
    const sourceNode = data.nodes.find((n) => n.id === link.source);
    const radius = sourceNode.isPipelineParent ? squareSize / Math.sqrt(2) : configValues.nodeRadius;
    const dx = targetPos.x - sourcePos.x;
    const dy = targetPos.y - sourcePos.y;
    const distance = Math.sqrt(dx * dx + dy * dy);
    return sourcePos.y + dy / distance * radius;
  }).attr("x2", (link) => {
    const sourcePos = positions.get(link.source);
    const targetPos = positions.get(link.target);
    const targetNode = data.nodes.find((n) => n.id === link.target);
    const radius = targetNode.isPipelineParent ? squareSize / Math.sqrt(2) : configValues.nodeRadius;
    const dx = sourcePos.x - targetPos.x;
    const dy = sourcePos.y - targetPos.y;
    const distance = Math.sqrt(dx * dx + dy * dy);
    return targetPos.x + dx / distance * radius;
  }).attr("y2", (link) => {
    const sourcePos = positions.get(link.source);
    const targetPos = positions.get(link.target);
    const targetNode = data.nodes.find((n) => n.id === link.target);
    const radius = targetNode.isPipelineParent ? squareSize / Math.sqrt(2) : configValues.nodeRadius;
    const dx = sourcePos.x - targetPos.x;
    const dy = sourcePos.y - targetPos.y;
    const distance = Math.sqrt(dx * dx + dy * dy);
    return targetPos.y + dy / distance * radius;
  }).attr("stroke", theme.linkStroke).attr("stroke-width", 1).attr("stroke-dasharray", (link) => link.dashed ? "6 6" : null).attr("marker-end", (link) => {
    if (link.flow === "forward" || link.flow === "bidirectional") {
      return `url(#link-arrow-end-${id})`;
    }
    return null;
  }).attr("marker-start", (link) => {
    if (link.flow === "backward" || link.flow === "bidirectional") {
      return `url(#link-arrow-start-${id})`;
    }
    return null;
  });
  linksGroup.selectAll("text").data(validLinks.filter((link) => link.label)).enter().append("text").attr("class", "wardley-link-label").attr("x", (link) => {
    const sourcePos = positions.get(link.source);
    const targetPos = positions.get(link.target);
    const midX = (sourcePos.x + targetPos.x) / 2;
    const dy = targetPos.y - sourcePos.y;
    const dx = targetPos.x - sourcePos.x;
    const distance = Math.sqrt(dx * dx + dy * dy);
    const offset = 8;
    const perpX = dy / distance;
    return midX + perpX * offset;
  }).attr("y", (link) => {
    const sourcePos = positions.get(link.source);
    const targetPos = positions.get(link.target);
    const midY = (sourcePos.y + targetPos.y) / 2;
    const dx = targetPos.x - sourcePos.x;
    const dy = targetPos.y - sourcePos.y;
    const distance = Math.sqrt(dx * dx + dy * dy);
    const offset = 8;
    const perpY = -dx / distance;
    return midY + perpY * offset;
  }).attr("fill", theme.axisTextColor).attr("font-size", configValues.labelFontSize).attr("text-anchor", "middle").attr("dominant-baseline", "middle").attr("transform", (link) => {
    const sourcePos = positions.get(link.source);
    const targetPos = positions.get(link.target);
    const midX = (sourcePos.x + targetPos.x) / 2;
    const midY = (sourcePos.y + targetPos.y) / 2;
    const dx = targetPos.x - sourcePos.x;
    const dy = targetPos.y - sourcePos.y;
    const distance = Math.sqrt(dx * dx + dy * dy);
    const offset = 8;
    const perpX = dy / distance;
    const perpY = -dx / distance;
    const labelX = midX + perpX * offset;
    const labelY = midY + perpY * offset;
    let angle = Math.atan2(dy, dx) * 180 / Math.PI;
    if (angle > 90 || angle < -90) {
      angle += 180;
    }
    return `rotate(${angle} ${labelX} ${labelY})`;
  }).text((link) => link.label);
  const trendGroup = root.append("g").attr("class", "wardley-trends");
  const trendsWithPositions = data.trends.map((trend) => {
    const origin = positions.get(trend.nodeId);
    if (!origin) {
      return null;
    }
    const targetX = projectX(trend.targetX);
    const targetY = projectY(trend.targetY);
    const dx = targetX - origin.x;
    const dy = targetY - origin.y;
    const distance = Math.sqrt(dx * dx + dy * dy);
    const shortenBy = configValues.nodeRadius + 2;
    const adjustedX2 = distance > shortenBy ? targetX - dx / distance * shortenBy : targetX;
    const adjustedY2 = distance > shortenBy ? targetY - dy / distance * shortenBy : targetY;
    return {
      origin,
      targetX,
      targetY,
      adjustedX2,
      adjustedY2
    };
  }).filter((trend) => trend !== null);
  trendGroup.selectAll("line").data(trendsWithPositions).enter().append("line").attr("class", "wardley-trend").attr("x1", (trend) => trend.origin.x).attr("y1", (trend) => trend.origin.y).attr("x2", (trend) => trend.adjustedX2).attr("y2", (trend) => trend.adjustedY2).attr("stroke", theme.evolutionStroke).attr("stroke-width", 1).attr("stroke-dasharray", "4 4").attr("marker-end", `url(#arrow-${id})`);
  const nodesGroup = root.append("g").attr("class", "wardley-nodes");
  const nodeEnter = nodesGroup.selectAll("g").data(data.nodes).enter().append("g").attr(
    "class",
    (node) => ["wardley-node", node.className ? `wardley-node--${node.className}` : ""].filter(Boolean).join(" ")
  );
  nodeEnter.filter((node) => node.sourceStrategy === "outsource").append("circle").attr("class", "wardley-outsource-overlay").attr("cx", (node) => positions.get(node.id).x).attr("cy", (node) => positions.get(node.id).y).attr("r", configValues.nodeRadius * 2).attr("fill", "#666").attr("stroke", theme.componentStroke).attr("stroke-width", 1);
  nodeEnter.filter((node) => node.sourceStrategy === "buy").append("circle").attr("class", "wardley-buy-overlay").attr("cx", (node) => positions.get(node.id).x).attr("cy", (node) => positions.get(node.id).y).attr("r", configValues.nodeRadius * 2).attr("fill", "#ccc").attr("stroke", theme.componentStroke).attr("stroke-width", 1);
  nodeEnter.filter((node) => node.sourceStrategy === "build").append("circle").attr("class", "wardley-build-overlay").attr("cx", (node) => positions.get(node.id).x).attr("cy", (node) => positions.get(node.id).y).attr("r", configValues.nodeRadius * 2).attr("fill", "#eee").attr("stroke", "#000").attr("stroke-width", 1);
  const marketNodes = nodeEnter.filter((node) => node.sourceStrategy === "market");
  marketNodes.append("circle").attr("class", "wardley-market-overlay").attr("cx", (node) => positions.get(node.id).x).attr("cy", (node) => positions.get(node.id).y).attr("r", configValues.nodeRadius * 2).attr("fill", "white").attr("stroke", theme.componentStroke).attr("stroke-width", 1);
  nodeEnter.filter(
    (node) => !node.isPipelineParent && node.sourceStrategy !== "market" && node.className !== "anchor"
  ).append("circle").attr("cx", (node) => positions.get(node.id).x).attr("cy", (node) => positions.get(node.id).y).attr("r", configValues.nodeRadius).attr("fill", theme.componentFill).attr("stroke", theme.componentStroke).attr("stroke-width", 1);
  const smallCircleRadius = configValues.nodeRadius * 0.7;
  const triangleRadius = configValues.nodeRadius * 1.2;
  marketNodes.append("line").attr("class", "wardley-market-line").attr("x1", (node) => positions.get(node.id).x).attr("y1", (node) => positions.get(node.id).y - triangleRadius).attr("x2", (node) => positions.get(node.id).x - triangleRadius * Math.cos(Math.PI / 6)).attr("y2", (node) => positions.get(node.id).y + triangleRadius * Math.sin(Math.PI / 6)).attr("stroke", theme.componentStroke).attr("stroke-width", 1);
  marketNodes.append("line").attr("class", "wardley-market-line").attr("x1", (node) => positions.get(node.id).x - triangleRadius * Math.cos(Math.PI / 6)).attr("y1", (node) => positions.get(node.id).y + triangleRadius * Math.sin(Math.PI / 6)).attr("x2", (node) => positions.get(node.id).x + triangleRadius * Math.cos(Math.PI / 6)).attr("y2", (node) => positions.get(node.id).y + triangleRadius * Math.sin(Math.PI / 6)).attr("stroke", theme.componentStroke).attr("stroke-width", 1);
  marketNodes.append("line").attr("class", "wardley-market-line").attr("x1", (node) => positions.get(node.id).x + triangleRadius * Math.cos(Math.PI / 6)).attr("y1", (node) => positions.get(node.id).y + triangleRadius * Math.sin(Math.PI / 6)).attr("x2", (node) => positions.get(node.id).x).attr("y2", (node) => positions.get(node.id).y - triangleRadius).attr("stroke", theme.componentStroke).attr("stroke-width", 1);
  marketNodes.append("circle").attr("class", "wardley-market-dot").attr("cx", (node) => positions.get(node.id).x).attr("cy", (node) => positions.get(node.id).y - triangleRadius).attr("r", smallCircleRadius).attr("fill", "white").attr("stroke", theme.componentStroke).attr("stroke-width", 2);
  marketNodes.append("circle").attr("class", "wardley-market-dot").attr("cx", (node) => positions.get(node.id).x - triangleRadius * Math.cos(Math.PI / 6)).attr("cy", (node) => positions.get(node.id).y + triangleRadius * Math.sin(Math.PI / 6)).attr("r", smallCircleRadius).attr("fill", "white").attr("stroke", theme.componentStroke).attr("stroke-width", 2);
  marketNodes.append("circle").attr("class", "wardley-market-dot").attr("cx", (node) => positions.get(node.id).x + triangleRadius * Math.cos(Math.PI / 6)).attr("cy", (node) => positions.get(node.id).y + triangleRadius * Math.sin(Math.PI / 6)).attr("r", smallCircleRadius).attr("fill", "white").attr("stroke", theme.componentStroke).attr("stroke-width", 2);
  nodeEnter.filter((node) => node.isPipelineParent === true).append("rect").attr("x", (node) => positions.get(node.id).x - squareSize / 2).attr("y", (node) => positions.get(node.id).y - squareSize / 2).attr("width", squareSize).attr("height", squareSize).attr("fill", theme.componentFill).attr("stroke", theme.componentStroke).attr("stroke-width", 1);
  nodeEnter.filter((node) => node.inertia === true).append("line").attr("class", "wardley-inertia").attr("x1", (node) => {
    const pos = positions.get(node.id);
    let offset = node.isPipelineParent ? squareSize / 2 + 15 : configValues.nodeRadius + 15;
    if (node.sourceStrategy) {
      offset += configValues.nodeRadius + 10;
    }
    return pos.x + offset;
  }).attr("y1", (node) => {
    const pos = positions.get(node.id);
    const lineHeight = node.isPipelineParent ? squareSize : configValues.nodeRadius * 2;
    return pos.y - lineHeight / 2;
  }).attr("x2", (node) => {
    const pos = positions.get(node.id);
    let offset = node.isPipelineParent ? squareSize / 2 + 15 : configValues.nodeRadius + 15;
    if (node.sourceStrategy) {
      offset += configValues.nodeRadius + 10;
    }
    return pos.x + offset;
  }).attr("y2", (node) => {
    const pos = positions.get(node.id);
    const lineHeight = node.isPipelineParent ? squareSize : configValues.nodeRadius * 2;
    return pos.y + lineHeight / 2;
  }).attr("stroke", theme.componentStroke).attr("stroke-width", 6);
  nodeEnter.append("text").attr("x", (node) => {
    const pos = positions.get(node.id);
    if (node.className === "anchor") {
      return node.labelOffsetX !== void 0 ? pos.x + node.labelOffsetX : pos.x;
    }
    let defaultOffset = configValues.nodeLabelOffset;
    if (node.sourceStrategy && node.labelOffsetX === void 0) {
      defaultOffset += 10;
    }
    const customOffset = node.labelOffsetX ?? defaultOffset;
    return pos.x + customOffset;
  }).attr("y", (node) => {
    const pos = positions.get(node.id);
    if (node.className === "anchor") {
      return node.labelOffsetY !== void 0 ? pos.y + node.labelOffsetY : pos.y - 3;
    }
    let defaultOffset = -configValues.nodeLabelOffset;
    if (node.sourceStrategy && node.labelOffsetY === void 0) {
      defaultOffset -= 10;
    }
    const customOffset = node.labelOffsetY ?? defaultOffset;
    return pos.y + customOffset;
  }).attr("class", "wardley-node-label").attr("fill", (node) => {
    if (node.className === "evolved") {
      return theme.evolutionStroke;
    }
    if (node.className === "anchor") {
      return "#000";
    }
    return theme.componentLabelColor;
  }).attr("font-size", configValues.labelFontSize).attr("font-weight", (node) => node.className === "anchor" ? "bold" : "normal").attr("text-anchor", (node) => node.className === "anchor" ? "middle" : "start").attr("dominant-baseline", (node) => node.className === "anchor" ? "middle" : "auto").text((node) => node.label);
  if (data.annotations.length > 0) {
    const annotationsGroup = root.append("g").attr("class", "wardley-annotations");
    data.annotations.forEach((annotation) => {
      const projectedCoords = annotation.coordinates.map((coord) => ({
        x: projectX(coord.x),
        y: projectY(coord.y)
      }));
      if (projectedCoords.length > 1) {
        for (let i = 0; i < projectedCoords.length - 1; i++) {
          annotationsGroup.append("line").attr("class", "wardley-annotation-line").attr("x1", projectedCoords[i].x).attr("y1", projectedCoords[i].y).attr("x2", projectedCoords[i + 1].x).attr("y2", projectedCoords[i + 1].y).attr("stroke", theme.axisColor).attr("stroke-width", 1.5).attr("stroke-dasharray", "4 4");
        }
      }
      projectedCoords.forEach((coord) => {
        const annotationNode = annotationsGroup.append("g").attr("class", "wardley-annotation");
        annotationNode.append("circle").attr("cx", coord.x).attr("cy", coord.y).attr("r", 10).attr("fill", "white").attr("stroke", theme.axisColor).attr("stroke-width", 1.5);
        annotationNode.append("text").attr("x", coord.x).attr("y", coord.y).attr("text-anchor", "middle").attr("dominant-baseline", "central").attr("font-size", 10).attr("fill", theme.axisTextColor).attr("font-weight", "bold").text(annotation.number);
      });
    });
    if (data.annotationsBox) {
      let boxX = projectX(data.annotationsBox.x);
      let boxY = projectY(data.annotationsBox.y);
      const padding = 10;
      const lineHeight = 16;
      const fontSize = 11;
      const textBoxGroup = annotationsGroup.append("g").attr("class", "wardley-annotations-box");
      const sortedAnnotations = [...data.annotations].filter((a) => a.text).sort((a, b) => a.number - b.number);
      const textElements = [];
      sortedAnnotations.forEach((annotation, idx) => {
        const text2 = textBoxGroup.append("text").attr("x", boxX + padding).attr("y", boxY + padding + (idx + 1) * lineHeight).attr("font-size", fontSize).attr("fill", theme.axisTextColor).attr("text-anchor", "start").attr("dominant-baseline", "middle").text(`${annotation.number}. ${annotation.text}`);
        textElements.push(text2);
      });
      if (textElements.length > 0) {
        let maxWidth = 0;
        let maxHeight = 0;
        textElements.forEach((text2) => {
          const textNode = text2.node();
          const textWidth = textNode.getComputedTextLength();
          maxWidth = Math.max(maxWidth, textWidth);
          const bbox = textNode.getBBox();
          maxHeight = Math.max(maxHeight, bbox.height);
        });
        const boxWidth = maxWidth + padding * 2 + 105;
        const boxHeight = sortedAnnotations.length * lineHeight + padding * 2 + maxHeight / 2;
        const minX = configValues.padding;
        const maxX = width - configValues.padding - boxWidth;
        const minY = configValues.padding;
        const maxY = height - configValues.padding - boxHeight;
        boxX = Math.max(minX, Math.min(boxX, maxX));
        boxY = Math.max(minY, Math.min(boxY, maxY));
        textElements.forEach((text2, idx) => {
          text2.attr("x", boxX + padding).attr("y", boxY + padding + (idx + 1) * lineHeight);
        });
        textBoxGroup.insert("rect", "text").attr("x", boxX).attr("y", boxY).attr("width", boxWidth).attr("height", boxHeight).attr("fill", "white").attr("stroke", theme.axisColor).attr("stroke-width", 1.5).attr("rx", 4).attr("ry", 4);
      }
    }
  }
  if (data.notes.length > 0) {
    const notesGroup = root.append("g").attr("class", "wardley-notes");
    data.notes.forEach((note) => {
      const noteX = projectX(note.x);
      const noteY = projectY(note.y);
      notesGroup.append("text").attr("x", noteX).attr("y", noteY).attr("text-anchor", "start").attr("font-size", 11).attr("fill", theme.axisTextColor).attr("font-weight", "bold").text(note.text);
    });
  }
  if (data.accelerators.length > 0) {
    const acceleratorsGroup = root.append("g").attr("class", "wardley-accelerators");
    data.accelerators.forEach((accelerator) => {
      const accX = projectX(accelerator.x);
      const accY = projectY(accelerator.y);
      const arrowWidth = 60;
      const arrowHeight = 30;
      const arrowHeadWidth = 20;
      const arrowPath = `
        M ${accX} ${accY - arrowHeight / 2}
        L ${accX + arrowWidth - arrowHeadWidth} ${accY - arrowHeight / 2}
        L ${accX + arrowWidth - arrowHeadWidth} ${accY - arrowHeight / 2 - 8}
        L ${accX + arrowWidth} ${accY}
        L ${accX + arrowWidth - arrowHeadWidth} ${accY + arrowHeight / 2 + 8}
        L ${accX + arrowWidth - arrowHeadWidth} ${accY + arrowHeight / 2}
        L ${accX} ${accY + arrowHeight / 2}
        Z
      `;
      acceleratorsGroup.append("path").attr("d", arrowPath).attr("fill", "white").attr("stroke", theme.componentStroke).attr("stroke-width", 1);
      acceleratorsGroup.append("text").attr("x", accX + arrowWidth / 2).attr("y", accY + arrowHeight / 2 + 15).attr("text-anchor", "middle").attr("font-size", 10).attr("fill", theme.axisTextColor).attr("font-weight", "bold").text(accelerator.name);
    });
  }
  if (data.deaccelerators.length > 0) {
    const deacceleratorsGroup = root.append("g").attr("class", "wardley-deaccelerators");
    data.deaccelerators.forEach((deaccelerator) => {
      const decX = projectX(deaccelerator.x);
      const decY = projectY(deaccelerator.y);
      const arrowWidth = 60;
      const arrowHeight = 30;
      const arrowHeadWidth = 20;
      const arrowPath = `
        M ${decX + arrowWidth} ${decY - arrowHeight / 2}
        L ${decX + arrowHeadWidth} ${decY - arrowHeight / 2}
        L ${decX + arrowHeadWidth} ${decY - arrowHeight / 2 - 8}
        L ${decX} ${decY}
        L ${decX + arrowHeadWidth} ${decY + arrowHeight / 2 + 8}
        L ${decX + arrowHeadWidth} ${decY + arrowHeight / 2}
        L ${decX + arrowWidth} ${decY + arrowHeight / 2}
        Z
      `;
      deacceleratorsGroup.append("path").attr("d", arrowPath).attr("fill", "white").attr("stroke", theme.componentStroke).attr("stroke-width", 1);
      deacceleratorsGroup.append("text").attr("x", decX + arrowWidth / 2).attr("y", decY + arrowHeight / 2 + 15).attr("text-anchor", "middle").attr("font-size", 10).attr("fill", theme.axisTextColor).attr("font-weight", "bold").text(deaccelerator.name);
    });
  }
}, "draw");
var wardleyRenderer_default = {
  draw
};

// src/diagrams/wardley/styles.ts
var styles = /* @__PURE__ */ __name(({
  wardley
} = {}) => {
  const defaultThemeVariables = getThemeVariables();
  const currentConfig = getConfig();
  const themeVariables = cleanAndMerge(defaultThemeVariables, currentConfig.themeVariables);
  const w = cleanAndMerge(themeVariables.wardley, wardley);
  return `
  .wardley-background {
    fill: ${w.backgroundColor};
  }
  .wardley-axes line, .wardley-axes path {
    stroke: ${w.axisColor};
  }
  .wardley-axis-label {
    fill: ${w.axisTextColor};
  }
  .wardley-stage-label {
    fill: ${w.axisTextColor};
  }
  .wardley-grid line {
    stroke: ${w.gridColor};
  }
  .wardley-node circle {
    fill: ${w.componentFill};
    stroke: ${w.componentStroke};
  }
  .wardley-node-label {
    fill: ${w.componentLabelColor};
  }
  .wardley-link {
    stroke: ${w.linkStroke};
  }
  .wardley-link--dashed {
    stroke-dasharray: 4 4;
  }
  .wardley-link-label {
    fill: ${w.axisTextColor};
  }
  .wardley-trend line {
    stroke: ${w.evolutionStroke};
  }
  .wardley-annotation-line {
    stroke: ${w.annotationStroke};
  }
  .wardley-annotation circle {
    fill: ${w.annotationFill};
    stroke: ${w.annotationStroke};
  }
  .wardley-annotation text {
    fill: ${w.annotationTextColor};
  }
  .wardley-annotations-box rect {
    fill: ${w.annotationFill};
    stroke: ${w.annotationStroke};
  }
  .wardley-annotations-box text {
    fill: ${w.annotationTextColor};
  }
  .wardley-pipeline-box {
    stroke: ${w.componentStroke};
  }
  .wardley-notes text {
    fill: ${w.axisTextColor};
  }
  `;
}, "styles");

// src/diagrams/wardley/wardleyDiagram.ts
var diagram = {
  parser,
  db: wardleyDb_default,
  renderer: wardleyRenderer_default,
  styles
};
export {
  diagram
};
