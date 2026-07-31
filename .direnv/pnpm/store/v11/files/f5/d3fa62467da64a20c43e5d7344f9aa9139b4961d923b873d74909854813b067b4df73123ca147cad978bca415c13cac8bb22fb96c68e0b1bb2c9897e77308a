import {
  selectSvgElement
} from "./chunk-VAUOI2AC.mjs";
import {
  clear,
  configureSvgSize,
  getConfig,
  getConfig2,
  getThemeVariables,
  sanitizeText
} from "./chunk-WYO6CB5R.mjs";
import {
  log
} from "./chunk-X3CZISLH.mjs";
import {
  __name
} from "./chunk-Y2CYZVJY.mjs";

// src/diagrams/railroad/railroadDb.ts
var diagramTitle = "";
var accTitle = "";
var accDescription = "";
var rules = [];
var ruleMap = /* @__PURE__ */ new Map();
var sanitizeText2 = /* @__PURE__ */ __name((text) => {
  return sanitizeText(text, getConfig2());
}, "sanitizeText");
var sanitizeAstNode = /* @__PURE__ */ __name((node) => {
  switch (node.type) {
    case "terminal":
      return { ...node, value: sanitizeText2(node.value) };
    case "nonterminal":
      return { ...node, name: sanitizeText2(node.name) };
    case "sequence":
      return { ...node, elements: node.elements.map(sanitizeAstNode) };
    case "choice":
      return { ...node, alternatives: node.alternatives.map(sanitizeAstNode) };
    case "optional":
      return { ...node, element: sanitizeAstNode(node.element) };
    case "repetition":
      return {
        ...node,
        element: sanitizeAstNode(node.element),
        separator: node.separator ? sanitizeAstNode(node.separator) : void 0
      };
    case "special":
      return { ...node, text: sanitizeText2(node.text) };
  }
}, "sanitizeAstNode");
var clear2 = /* @__PURE__ */ __name(() => {
  diagramTitle = "";
  accTitle = "";
  accDescription = "";
  rules.length = 0;
  ruleMap.clear();
  clear();
  log.debug("[Railroad] Database cleared");
}, "clear");
var setTitle = /* @__PURE__ */ __name((text) => {
  diagramTitle = sanitizeText2(text);
  log.debug("[Railroad] Title set:", text);
}, "setTitle");
var getTitle = /* @__PURE__ */ __name(() => {
  return diagramTitle;
}, "getTitle");
var addRule = /* @__PURE__ */ __name((rule) => {
  const sanitizedRule = {
    ...rule,
    name: sanitizeText2(rule.name),
    definition: sanitizeAstNode(rule.definition),
    comment: rule.comment ? sanitizeText2(rule.comment) : void 0
  };
  log.debug("[Railroad] Adding rule:", sanitizedRule.name);
  if (ruleMap.has(sanitizedRule.name)) {
    log.warn(`[Railroad] Rule '${sanitizedRule.name}' is already defined. Overwriting.`);
  }
  rules.push(sanitizedRule);
  ruleMap.set(sanitizedRule.name, sanitizedRule);
}, "addRule");
var getRules = /* @__PURE__ */ __name(() => {
  return rules;
}, "getRules");
var getRule = /* @__PURE__ */ __name((name) => {
  return ruleMap.get(name);
}, "getRule");
var setAccTitle = /* @__PURE__ */ __name((text) => {
  accTitle = sanitizeText2(text).replace(/^\s+/g, "");
  log.debug("[Railroad] Accessibility title set:", text);
}, "setAccTitle");
var getAccTitle = /* @__PURE__ */ __name(() => {
  return accTitle;
}, "getAccTitle");
var setAccDescription = /* @__PURE__ */ __name((text) => {
  accDescription = sanitizeText2(text).replace(/\n\s+/g, "\n");
  log.debug("[Railroad] Accessibility description set:", text);
}, "setAccDescription");
var getAccDescription = /* @__PURE__ */ __name(() => {
  return accDescription;
}, "getAccDescription");
var setDiagramTitle = setTitle;
var getDiagramTitle = getTitle;
var db = {
  clear: clear2,
  setTitle,
  getTitle,
  addRule,
  getRules,
  getRule,
  setAccTitle,
  getAccTitle,
  setAccDescription,
  getAccDescription,
  setDiagramTitle,
  getDiagramTitle
};

// src/diagrams/railroad/railroadTypes.ts
var DEFAULT_RAILROAD_CONFIG = {
  // Layout
  compactMode: false,
  // Spacing
  padding: 10,
  verticalSeparation: 8,
  horizontalSeparation: 10,
  arcRadius: 10,
  // Typography
  fontSize: 14,
  fontFamily: "monospace",
  // Colors - Terminal
  terminalFill: "#FFFFC0",
  terminalStroke: "#000000",
  terminalTextColor: "#000000",
  // Colors - Non-terminal
  nonTerminalFill: "#FFFFFF",
  nonTerminalStroke: "#000000",
  nonTerminalTextColor: "#000000",
  // Colors - Lines
  lineColor: "#000000",
  strokeWidth: 2,
  // Colors - Markers
  markerFill: "#000000",
  // Colors - Comment
  commentFill: "#E8E8E8",
  commentStroke: "#888888",
  commentTextColor: "#666666",
  // Colors - Special
  specialFill: "#F0E0FF",
  specialStroke: "#8800CC",
  // Other
  ruleNameColor: "#000066",
  showMarkers: true,
  markerRadius: 5
};

// src/diagrams/railroad/styles.ts
var COLOR_VALUE_PATTERN = /^#(?:[\da-f]{3,4}|[\da-f]{6}|[\da-f]{8})$|^(?:rgb|rgba|hsl|hsla|hwb|lab|lch|oklab|oklch)\([\d\s%+,./-]+\)$|^[a-z]+$/i;
var FONT_FAMILY_PATTERN = /^[\w "',.-]+$/;
var RAILROAD_STYLE_OPTION_KEYS = /* @__PURE__ */ new Set([
  "compactMode",
  "padding",
  "verticalSeparation",
  "horizontalSeparation",
  "arcRadius",
  "fontSize",
  "fontFamily",
  "terminalFill",
  "terminalStroke",
  "terminalTextColor",
  "nonTerminalFill",
  "nonTerminalStroke",
  "nonTerminalTextColor",
  "lineColor",
  "strokeWidth",
  "markerFill",
  "commentFill",
  "commentStroke",
  "commentTextColor",
  "specialFill",
  "specialStroke",
  "ruleNameColor",
  "showMarkers",
  "markerRadius"
]);
var isRailroadStyleOptions = /* @__PURE__ */ __name((options) => {
  if (!options) {
    return false;
  }
  return Object.keys(options).every(
    (key) => key === "railroad" || RAILROAD_STYLE_OPTION_KEYS.has(key)
  );
}, "isRailroadStyleOptions");
var extractRailroadOverrides = /* @__PURE__ */ __name((options) => {
  if (!options) {
    return {};
  }
  if ("railroad" in options && options.railroad) {
    return options.railroad;
  }
  return isRailroadStyleOptions(options) ? options : {};
}, "extractRailroadOverrides");
var extractThemeOverrides = /* @__PURE__ */ __name((options) => {
  if (!options || isRailroadStyleOptions(options)) {
    return {};
  }
  const {
    railroad: _railroad,
    svgId: _svgId,
    theme: _theme,
    look: _look,
    ...themeOverrides
  } = options;
  return themeOverrides;
}, "extractThemeOverrides");
var sanitizeColorValue = /* @__PURE__ */ __name((value, fallback) => {
  if (typeof value !== "string") {
    return fallback;
  }
  const normalized = value.trim();
  return COLOR_VALUE_PATTERN.test(normalized) ? normalized : fallback;
}, "sanitizeColorValue");
var sanitizeFontFamilyValue = /* @__PURE__ */ __name((value, fallback) => {
  if (typeof value !== "string") {
    return fallback;
  }
  const normalized = value.trim();
  return FONT_FAMILY_PATTERN.test(normalized) ? normalized : fallback;
}, "sanitizeFontFamilyValue");
var sanitizeNumberValue = /* @__PURE__ */ __name((value, fallback) => {
  const parsed = typeof value === "number" ? value : typeof value === "string" ? Number.parseFloat(value) : Number.NaN;
  return Number.isFinite(parsed) && parsed >= 0 ? parsed : fallback;
}, "sanitizeNumberValue");
var parseThemeFontSize = /* @__PURE__ */ __name((value) => {
  const parsed = typeof value === "number" ? value : typeof value === "string" ? Number.parseFloat(value) : Number.NaN;
  return Number.isFinite(parsed) && parsed > 0 ? parsed : void 0;
}, "parseThemeFontSize");
var buildThemeDefaults = /* @__PURE__ */ __name((themeVariables) => {
  const fontFamily = sanitizeFontFamilyValue(
    themeVariables.fontFamily,
    DEFAULT_RAILROAD_CONFIG.fontFamily
  );
  const fontSize = parseThemeFontSize(themeVariables.fontSize) ?? DEFAULT_RAILROAD_CONFIG.fontSize;
  return {
    ...DEFAULT_RAILROAD_CONFIG,
    fontFamily,
    fontSize,
    terminalFill: sanitizeColorValue(
      themeVariables.secondBkg ?? themeVariables.secondaryColor,
      DEFAULT_RAILROAD_CONFIG.terminalFill
    ),
    terminalStroke: sanitizeColorValue(
      themeVariables.secondaryBorderColor ?? themeVariables.lineColor,
      DEFAULT_RAILROAD_CONFIG.terminalStroke
    ),
    terminalTextColor: sanitizeColorValue(
      themeVariables.secondaryTextColor ?? themeVariables.textColor,
      DEFAULT_RAILROAD_CONFIG.terminalTextColor
    ),
    nonTerminalFill: sanitizeColorValue(
      themeVariables.mainBkg ?? themeVariables.background,
      DEFAULT_RAILROAD_CONFIG.nonTerminalFill
    ),
    nonTerminalStroke: sanitizeColorValue(
      themeVariables.primaryBorderColor ?? themeVariables.lineColor,
      DEFAULT_RAILROAD_CONFIG.nonTerminalStroke
    ),
    nonTerminalTextColor: sanitizeColorValue(
      themeVariables.primaryTextColor ?? themeVariables.textColor,
      DEFAULT_RAILROAD_CONFIG.nonTerminalTextColor
    ),
    lineColor: sanitizeColorValue(themeVariables.lineColor, DEFAULT_RAILROAD_CONFIG.lineColor),
    markerFill: sanitizeColorValue(themeVariables.lineColor, DEFAULT_RAILROAD_CONFIG.markerFill),
    commentFill: sanitizeColorValue(
      themeVariables.labelBackground ?? themeVariables.tertiaryColor,
      DEFAULT_RAILROAD_CONFIG.commentFill
    ),
    commentStroke: sanitizeColorValue(
      themeVariables.tertiaryBorderColor ?? themeVariables.lineColor,
      DEFAULT_RAILROAD_CONFIG.commentStroke
    ),
    commentTextColor: sanitizeColorValue(
      themeVariables.tertiaryTextColor ?? themeVariables.textColor,
      DEFAULT_RAILROAD_CONFIG.commentTextColor
    ),
    specialFill: sanitizeColorValue(
      themeVariables.tertiaryColor ?? themeVariables.secondaryColor,
      DEFAULT_RAILROAD_CONFIG.specialFill
    ),
    specialStroke: sanitizeColorValue(
      themeVariables.tertiaryBorderColor ?? themeVariables.secondaryBorderColor,
      DEFAULT_RAILROAD_CONFIG.specialStroke
    ),
    ruleNameColor: sanitizeColorValue(
      themeVariables.titleColor ?? themeVariables.textColor,
      DEFAULT_RAILROAD_CONFIG.ruleNameColor
    )
  };
}, "buildThemeDefaults");
var buildRailroadStyleOptions = /* @__PURE__ */ __name((options) => {
  const currentConfig = getConfig();
  const themeVariables = {
    ...getThemeVariables(),
    ...currentConfig.themeVariables ?? {},
    ...extractThemeOverrides(options)
  };
  const themeDefaults = buildThemeDefaults(themeVariables);
  const railroadOptions = {
    ...currentConfig.railroad ?? {},
    ...extractRailroadOverrides(options)
  };
  return {
    compactMode: railroadOptions.compactMode ?? themeDefaults.compactMode,
    padding: sanitizeNumberValue(railroadOptions.padding, themeDefaults.padding),
    verticalSeparation: sanitizeNumberValue(
      railroadOptions.verticalSeparation,
      themeDefaults.verticalSeparation
    ),
    horizontalSeparation: sanitizeNumberValue(
      railroadOptions.horizontalSeparation,
      themeDefaults.horizontalSeparation
    ),
    arcRadius: sanitizeNumberValue(railroadOptions.arcRadius, themeDefaults.arcRadius),
    fontSize: sanitizeNumberValue(railroadOptions.fontSize, themeDefaults.fontSize),
    fontFamily: sanitizeFontFamilyValue(railroadOptions.fontFamily, themeDefaults.fontFamily),
    terminalFill: sanitizeColorValue(railroadOptions.terminalFill, themeDefaults.terminalFill),
    terminalStroke: sanitizeColorValue(
      railroadOptions.terminalStroke,
      themeDefaults.terminalStroke
    ),
    terminalTextColor: sanitizeColorValue(
      railroadOptions.terminalTextColor,
      themeDefaults.terminalTextColor
    ),
    nonTerminalFill: sanitizeColorValue(
      railroadOptions.nonTerminalFill,
      themeDefaults.nonTerminalFill
    ),
    nonTerminalStroke: sanitizeColorValue(
      railroadOptions.nonTerminalStroke,
      themeDefaults.nonTerminalStroke
    ),
    nonTerminalTextColor: sanitizeColorValue(
      railroadOptions.nonTerminalTextColor,
      themeDefaults.nonTerminalTextColor
    ),
    lineColor: sanitizeColorValue(railroadOptions.lineColor, themeDefaults.lineColor),
    strokeWidth: sanitizeNumberValue(railroadOptions.strokeWidth, themeDefaults.strokeWidth),
    markerFill: sanitizeColorValue(railroadOptions.markerFill, themeDefaults.markerFill),
    commentFill: sanitizeColorValue(railroadOptions.commentFill, themeDefaults.commentFill),
    commentStroke: sanitizeColorValue(railroadOptions.commentStroke, themeDefaults.commentStroke),
    commentTextColor: sanitizeColorValue(
      railroadOptions.commentTextColor,
      themeDefaults.commentTextColor
    ),
    specialFill: sanitizeColorValue(railroadOptions.specialFill, themeDefaults.specialFill),
    specialStroke: sanitizeColorValue(railroadOptions.specialStroke, themeDefaults.specialStroke),
    ruleNameColor: sanitizeColorValue(railroadOptions.ruleNameColor, themeDefaults.ruleNameColor),
    showMarkers: railroadOptions.showMarkers ?? themeDefaults.showMarkers,
    markerRadius: sanitizeNumberValue(railroadOptions.markerRadius, themeDefaults.markerRadius)
  };
}, "buildRailroadStyleOptions");
var getStyles = /* @__PURE__ */ __name((options) => {
  const {
    fontFamily,
    fontSize,
    terminalFill,
    terminalStroke,
    terminalTextColor,
    nonTerminalFill,
    nonTerminalStroke,
    nonTerminalTextColor,
    lineColor,
    strokeWidth,
    markerFill,
    commentFill,
    commentStroke,
    commentTextColor,
    specialFill,
    specialStroke,
    ruleNameColor
  } = buildRailroadStyleOptions(options);
  return `
  .railroad-diagram {
    font-family: ${fontFamily};
    font-size: ${fontSize}px;
  }

  .railroad-terminal rect {
    fill: ${terminalFill};
    stroke: ${terminalStroke};
    stroke-width: ${strokeWidth}px;
  }

  .railroad-terminal text {
    fill: ${terminalTextColor};
    font-family: ${fontFamily};
    font-size: ${fontSize}px;
    text-anchor: middle;
    dominant-baseline: middle;
  }

  .railroad-nonterminal rect {
    fill: ${nonTerminalFill};
    stroke: ${nonTerminalStroke};
    stroke-width: ${strokeWidth}px;
  }

  .railroad-nonterminal text {
    fill: ${nonTerminalTextColor};
    font-family: ${fontFamily};
    font-size: ${fontSize}px;
    text-anchor: middle;
    dominant-baseline: middle;
  }

  .railroad-line {
    stroke: ${lineColor};
    stroke-width: ${strokeWidth}px;
    fill: none;
  }

  .railroad-start circle,
  .railroad-end circle {
    fill: ${markerFill};
  }

  .railroad-comment ellipse {
    fill: ${commentFill};
    stroke: ${commentStroke};
    stroke-width: ${strokeWidth}px;
  }

  .railroad-comment text {
    fill: ${commentTextColor};
    font-style: italic;
    font-family: ${fontFamily};
    font-size: ${fontSize}px;
    text-anchor: middle;
    dominant-baseline: middle;
  }

  .railroad-special rect {
    fill: ${specialFill};
    stroke: ${specialStroke};
    stroke-width: ${strokeWidth}px;
    stroke-dasharray: 5,3;
  }

  .railroad-special text {
    fill: ${nonTerminalTextColor};
    font-family: ${fontFamily};
    font-size: ${fontSize}px;
    text-anchor: middle;
    dominant-baseline: middle;
  }

  .railroad-rule-name {
    font-weight: bold;
    fill: ${ruleNameColor};
    font-family: ${fontFamily};
    font-size: ${fontSize}px;
  }

  .railroad-group {
    /* Grouping container, no specific styles */
  }
`;
}, "getStyles");

// src/diagrams/railroad/railroadRenderer.ts
var PathBuilder = class {
  constructor() {
    this.d = "";
  }
  static {
    __name(this, "PathBuilder");
  }
  moveTo(x, y) {
    this.d += `M ${x} ${y} `;
    return this;
  }
  lineTo(x, y) {
    this.d += `L ${x} ${y} `;
    return this;
  }
  horizontalTo(x) {
    this.d += `H ${x} `;
    return this;
  }
  verticalTo(y) {
    this.d += `V ${y} `;
    return this;
  }
  arcTo(rx, ry, rotation, largeArc, sweep, x, y) {
    this.d += `A ${rx} ${ry} ${rotation} ${largeArc ? 1 : 0} ${sweep ? 1 : 0} ${x} ${y} `;
    return this;
  }
  build() {
    return this.d.trim();
  }
};
var RailroadRenderer = class {
  constructor(svg, config = buildRailroadStyleOptions()) {
    this.textCache = /* @__PURE__ */ new Map();
    this.svg = svg;
    this.config = config;
  }
  static {
    __name(this, "RailroadRenderer");
  }
  /**
   * Measure text dimensions
   */
  measureText(text) {
    if (this.textCache.has(text)) {
      return this.textCache.get(text);
    }
    const tempText = this.svg.append("text").attr("font-family", this.config.fontFamily).attr("font-size", this.config.fontSize).text(text);
    const bbox = tempText.node().getBBox();
    const dimensions = { width: bbox.width, height: bbox.height };
    tempText.remove();
    this.textCache.set(text, dimensions);
    return dimensions;
  }
  /**
   * Render terminal symbol (rounded rectangle)
   */
  renderTerminal(parent, value) {
    const textDim = this.measureText(value);
    const width = textDim.width + this.config.padding * 2;
    const height = textDim.height + this.config.padding * 2;
    const group = parent.append("g").attr("class", "railroad-terminal");
    group.append("rect").attr("x", 0).attr("y", 0).attr("width", width).attr("height", height).attr("rx", 10).attr("ry", 10);
    group.append("text").attr("x", width / 2).attr("y", height / 2).text(value);
    return {
      element: group.node(),
      dimensions: {
        width,
        height,
        up: height / 2,
        down: height / 2
      }
    };
  }
  /**
   * Render non-terminal symbol (rectangle)
   */
  renderNonTerminal(parent, name) {
    const textDim = this.measureText(name);
    const width = textDim.width + this.config.padding * 2;
    const height = textDim.height + this.config.padding * 2;
    const group = parent.append("g").attr("class", "railroad-nonterminal");
    group.append("rect").attr("x", 0).attr("y", 0).attr("width", width).attr("height", height);
    group.append("text").attr("x", width / 2).attr("y", height / 2).text(name);
    return {
      element: group.node(),
      dimensions: {
        width,
        height,
        up: height / 2,
        down: height / 2
      }
    };
  }
  /**
   * Render sequence (horizontal concatenation)
   */
  renderSequence(parent, elements) {
    const rendered = elements.map((e) => this.renderExpression(parent, e));
    let totalWidth = 0;
    let maxUp = 0;
    let maxDown = 0;
    for (const r of rendered) {
      totalWidth += r.dimensions.width;
      maxUp = Math.max(maxUp, r.dimensions.up);
      maxDown = Math.max(maxDown, r.dimensions.down);
    }
    totalWidth += (rendered.length - 1) * this.config.horizontalSeparation;
    const group = parent.append("g").attr("class", "railroad-sequence");
    let x = 0;
    for (let i = 0; i < rendered.length; i++) {
      const r = rendered[i];
      const y = maxUp - r.dimensions.up;
      const elem = group.node().appendChild(r.element);
      elem.setAttribute("transform", `translate(${x}, ${y})`);
      if (i < rendered.length - 1) {
        const lineX1 = x + r.dimensions.width;
        const lineX2 = lineX1 + this.config.horizontalSeparation;
        const lineY = maxUp;
        group.append("path").attr("class", "railroad-line").attr("d", new PathBuilder().moveTo(lineX1, lineY).lineTo(lineX2, lineY).build());
      }
      x += r.dimensions.width + this.config.horizontalSeparation;
    }
    return {
      element: group.node(),
      dimensions: {
        width: totalWidth,
        height: maxUp + maxDown,
        up: maxUp,
        down: maxDown
      }
    };
  }
  /**
   * Render choice (vertical alternatives)
   */
  renderChoice(parent, alternatives) {
    const rendered = alternatives.map((a) => this.renderExpression(parent, a));
    let maxWidth = 0;
    let totalHeight = 0;
    for (const r of rendered) {
      maxWidth = Math.max(maxWidth, r.dimensions.width);
      totalHeight += r.dimensions.height;
    }
    totalHeight += (rendered.length - 1) * this.config.verticalSeparation;
    const arcRadius = this.config.arcRadius;
    const arcWidth = arcRadius * 4;
    const totalWidth = maxWidth + arcWidth;
    const group = parent.append("g").attr("class", "railroad-choice");
    let y = 0;
    const centerY = totalHeight / 2;
    for (const r of rendered) {
      const elemY = y;
      const elemCenterY = elemY + r.dimensions.up;
      const elemX = arcRadius * 2 + (maxWidth - r.dimensions.width) / 2;
      const elem = group.node().appendChild(r.element);
      elem.setAttribute("transform", `translate(${elemX}, ${elemY})`);
      const leftPath = new PathBuilder();
      const isBelowCenter = elemCenterY > centerY;
      if (elemCenterY === centerY) {
        leftPath.moveTo(0, centerY).lineTo(elemX, elemCenterY);
      } else {
        leftPath.moveTo(0, centerY).arcTo(
          arcRadius,
          arcRadius,
          0,
          false,
          isBelowCenter,
          arcRadius,
          centerY + (isBelowCenter ? arcRadius : -arcRadius)
        ).lineTo(arcRadius, elemCenterY - (isBelowCenter ? arcRadius : -arcRadius)).arcTo(arcRadius, arcRadius, 0, false, !isBelowCenter, arcRadius * 2, elemCenterY).lineTo(elemX, elemCenterY);
      }
      group.append("path").attr("class", "railroad-line").attr("d", leftPath.build());
      const rightPath = new PathBuilder();
      const rightStart = elemX + r.dimensions.width;
      const rightLaneX = totalWidth - arcRadius * 2;
      if (elemCenterY === centerY) {
        rightPath.moveTo(rightStart, elemCenterY).lineTo(totalWidth, centerY);
      } else {
        rightPath.moveTo(rightStart, elemCenterY).lineTo(rightLaneX, elemCenterY).arcTo(
          arcRadius,
          arcRadius,
          0,
          false,
          !isBelowCenter,
          totalWidth - arcRadius,
          elemCenterY + (isBelowCenter ? -arcRadius : arcRadius)
        ).lineTo(totalWidth - arcRadius, centerY + (isBelowCenter ? arcRadius : -arcRadius)).arcTo(arcRadius, arcRadius, 0, false, isBelowCenter, totalWidth, centerY);
      }
      group.append("path").attr("class", "railroad-line").attr("d", rightPath.build());
      y += r.dimensions.height + this.config.verticalSeparation;
    }
    return {
      element: group.node(),
      dimensions: {
        width: totalWidth,
        height: totalHeight,
        up: centerY,
        down: totalHeight - centerY
      }
    };
  }
  /**
   * Render optional (bypass path above)
   */
  renderOptional(parent, element) {
    const inner = this.renderExpression(parent, element);
    const arcRadius = this.config.arcRadius;
    const arcHeight = arcRadius * 2;
    const totalWidth = inner.dimensions.width + arcRadius * 4;
    const totalHeight = inner.dimensions.height + arcHeight;
    const group = parent.append("g").attr("class", "railroad-optional");
    const elemX = arcRadius * 2;
    const elemY = arcHeight;
    const elem = group.node().appendChild(inner.element);
    elem.setAttribute("transform", `translate(${elemX}, ${elemY})`);
    const centerY = elemY + inner.dimensions.up;
    const lowerPath = new PathBuilder().moveTo(0, centerY).lineTo(arcRadius * 2, centerY);
    group.append("path").attr("class", "railroad-line").attr("d", lowerPath.build());
    const lowerPath2 = new PathBuilder().moveTo(elemX + inner.dimensions.width, centerY).lineTo(totalWidth, centerY);
    group.append("path").attr("class", "railroad-line").attr("d", lowerPath2.build());
    const bypassPath = new PathBuilder().moveTo(0, centerY).arcTo(arcRadius, arcRadius, 0, false, false, arcRadius, centerY - arcRadius).lineTo(arcRadius, arcRadius).arcTo(arcRadius, arcRadius, 0, false, true, arcRadius * 2, 0).lineTo(totalWidth - arcRadius * 2, 0).arcTo(arcRadius, arcRadius, 0, false, true, totalWidth - arcRadius, arcRadius).lineTo(totalWidth - arcRadius, centerY - arcRadius).arcTo(arcRadius, arcRadius, 0, false, false, totalWidth, centerY);
    group.append("path").attr("class", "railroad-line").attr("d", bypassPath.build());
    return {
      element: group.node(),
      dimensions: {
        width: totalWidth,
        height: totalHeight,
        up: centerY,
        down: totalHeight - centerY
      }
    };
  }
  /**
   * Render repetition (loop back path)
   */
  renderRepetition(parent, element, min) {
    const inner = this.renderExpression(parent, element);
    const arcRadius = this.config.arcRadius;
    const arcHeight = arcRadius * 2;
    const totalWidth = inner.dimensions.width + arcRadius * 4;
    const hasBypass = min === 0;
    const totalHeight = inner.dimensions.height + arcHeight + (hasBypass ? arcHeight : 0);
    const group = parent.append("g").attr("class", "railroad-repetition");
    const elemX = arcRadius * 2;
    const elemY = hasBypass ? arcHeight : 0;
    const elem = group.node().appendChild(inner.element);
    elem.setAttribute("transform", `translate(${elemX}, ${elemY})`);
    const centerY = elemY + inner.dimensions.up;
    group.append("path").attr("class", "railroad-line").attr(
      "d",
      new PathBuilder().moveTo(0, centerY).lineTo(arcRadius * 2, centerY).build()
    );
    group.append("path").attr("class", "railroad-line").attr(
      "d",
      new PathBuilder().moveTo(elemX + inner.dimensions.width, centerY).lineTo(totalWidth, centerY).build()
    );
    const loopY = elemY + inner.dimensions.height + arcRadius;
    const loopPath = new PathBuilder().moveTo(elemX + inner.dimensions.width, centerY).arcTo(
      arcRadius,
      arcRadius,
      0,
      false,
      true,
      elemX + inner.dimensions.width + arcRadius,
      centerY + arcRadius
    ).lineTo(elemX + inner.dimensions.width + arcRadius, loopY).arcTo(
      arcRadius,
      arcRadius,
      0,
      false,
      true,
      elemX + inner.dimensions.width,
      loopY + arcRadius
    ).lineTo(arcRadius * 2, loopY + arcRadius).arcTo(arcRadius, arcRadius, 0, false, true, arcRadius, loopY).lineTo(arcRadius, centerY + arcRadius).arcTo(arcRadius, arcRadius, 0, false, true, arcRadius * 2, centerY);
    group.append("path").attr("class", "railroad-line").attr("d", loopPath.build());
    if (hasBypass) {
      const bypassPath = new PathBuilder().moveTo(0, centerY).arcTo(arcRadius, arcRadius, 0, false, false, arcRadius, centerY - arcRadius).lineTo(arcRadius, arcRadius).arcTo(arcRadius, arcRadius, 0, false, true, arcRadius * 2, 0).lineTo(totalWidth - arcRadius * 2, 0).arcTo(arcRadius, arcRadius, 0, false, true, totalWidth - arcRadius, arcRadius).lineTo(totalWidth - arcRadius, centerY - arcRadius).arcTo(arcRadius, arcRadius, 0, false, false, totalWidth, centerY);
      group.append("path").attr("class", "railroad-line").attr("d", bypassPath.build());
    }
    return {
      element: group.node(),
      dimensions: {
        width: totalWidth,
        height: totalHeight,
        up: centerY,
        down: totalHeight - centerY
      }
    };
  }
  /**
   * Render special sequence
   */
  renderSpecial(parent, text) {
    const textDim = this.measureText("? " + text + " ?");
    const width = textDim.width + this.config.padding * 2;
    const height = textDim.height + this.config.padding * 2;
    const group = parent.append("g").attr("class", "railroad-special");
    group.append("rect").attr("x", 0).attr("y", 0).attr("width", width).attr("height", height);
    group.append("text").attr("x", width / 2).attr("y", height / 2).text("? " + text + " ?");
    return {
      element: group.node(),
      dimensions: {
        width,
        height,
        up: height / 2,
        down: height / 2
      }
    };
  }
  /**
   * Render an expression (recursive)
   */
  renderExpression(parent, node) {
    switch (node.type) {
      case "terminal":
        return this.renderTerminal(parent, node.value);
      case "nonterminal":
        return this.renderNonTerminal(parent, node.name);
      case "sequence":
        return this.renderSequence(parent, node.elements);
      case "choice":
        return this.renderChoice(parent, node.alternatives);
      case "optional":
        return this.renderOptional(parent, node.element);
      case "repetition":
        return this.renderRepetition(parent, node.element, node.min);
      case "special":
        return this.renderSpecial(parent, node.text);
      default:
        throw new Error(`Unknown node type: ${node.type}`);
    }
  }
  /**
   * Render a single rule
   */
  renderRule(rule, y) {
    const group = this.svg.append("g").attr("class", "railroad-rule").attr("transform", `translate(0, ${y})`);
    const ruleName = rule.name + " =";
    const nameWidth = this.measureText(ruleName).width + 20;
    const definitionX = nameWidth + 20;
    const defGroup = group.append("g");
    const result = this.renderExpression(defGroup, rule.definition);
    const baselineY = Math.max(20, result.dimensions.up);
    const definitionY = baselineY - result.dimensions.up;
    defGroup.attr("transform", `translate(${definitionX}, ${definitionY})`);
    const nameGroup = group.append("g").attr("class", "railroad-rule-name-group");
    nameGroup.append("text").attr("class", "railroad-rule-name").attr("x", 0).attr("y", baselineY).text(ruleName);
    const startMarker = group.append("g").attr("class", "railroad-start");
    startMarker.append("circle").attr("cx", nameWidth).attr("cy", baselineY).attr("r", this.config.markerRadius);
    const endMarker = group.append("g").attr("class", "railroad-end");
    endMarker.append("circle").attr("cx", definitionX + result.dimensions.width + 10).attr("cy", baselineY).attr("r", this.config.markerRadius);
    group.append("path").attr("class", "railroad-line").attr(
      "d",
      new PathBuilder().moveTo(nameWidth + this.config.markerRadius, baselineY).lineTo(definitionX, baselineY).build()
    );
    group.append("path").attr("class", "railroad-line").attr(
      "d",
      new PathBuilder().moveTo(definitionX + result.dimensions.width, baselineY).lineTo(definitionX + result.dimensions.width + 10 - this.config.markerRadius, baselineY).build()
    );
    return {
      height: Math.max(40, definitionY + result.dimensions.height + this.config.padding * 2),
      width: definitionX + result.dimensions.width + 10 + this.config.markerRadius
    };
  }
  /**
   * Render all rules
   */
  renderDiagram(rules2) {
    let y = this.config.padding;
    let maxWidth = 0;
    for (const rule of rules2) {
      const result = this.renderRule(rule, y);
      y += result.height + this.config.verticalSeparation;
      maxWidth = Math.max(maxWidth, result.width);
    }
    return {
      width: maxWidth + this.config.padding * 2,
      height: y + this.config.padding
    };
  }
};
var configureRailroadSvgSize = /* @__PURE__ */ __name((svg, dimensions, useMaxWidth) => {
  configureSvgSize(svg, dimensions.height, dimensions.width, useMaxWidth);
  svg.attr("viewBox", `0 0 ${dimensions.width} ${dimensions.height}`);
}, "configureRailroadSvgSize");
var draw = /* @__PURE__ */ __name((text, id, _version) => {
  log.debug("[Railroad] Rendering diagram\n" + text);
  try {
    const svg = selectSvgElement(id);
    svg.attr("class", "railroad-diagram");
    const railroadConfig = getConfig().railroad;
    const useMaxWidth = railroadConfig?.useMaxWidth ?? true;
    const rules2 = db.getRules();
    log.debug(`[Railroad] Rendering ${rules2.length} rules`);
    if (rules2.length === 0) {
      log.warn("[Railroad] No rules to render");
      configureRailroadSvgSize(svg, { height: 100, width: 200 }, useMaxWidth);
      return;
    }
    const renderer2 = new RailroadRenderer(svg, buildRailroadStyleOptions());
    const dimensions = renderer2.renderDiagram(rules2);
    configureRailroadSvgSize(svg, dimensions, useMaxWidth);
    log.debug("[Railroad] Render complete");
  } catch (error) {
    log.error("[Railroad] Render error:", error);
    throw error;
  }
}, "draw");
var renderer = { draw };

export {
  db,
  getStyles,
  renderer
};
