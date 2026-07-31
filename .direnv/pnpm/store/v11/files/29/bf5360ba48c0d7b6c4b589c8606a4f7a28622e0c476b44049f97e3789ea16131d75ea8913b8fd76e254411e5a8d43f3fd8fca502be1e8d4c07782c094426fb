import {
  createLabel_default
} from "./chunk-ZGVPDNZ5.mjs";
import {
  isLabelStyle,
  styles2String
} from "./chunk-C7G6YPKG.mjs";
import {
  computeLabelTransform,
  getLineFunctionsWithOffset,
  markerOffsets,
  markerOffsets2
} from "./chunk-7BUUIJ7U.mjs";
import {
  getSubGraphTitleMargins
} from "./chunk-OGEWGWER.mjs";
import {
  createText
} from "./chunk-Q4XR5HBZ.mjs";
import {
  handleUndefinedAttr,
  utils_default
} from "./chunk-ICXQ74PX.mjs";
import {
  getConfig,
  getConfig2,
  getEffectiveHtmlLabels
} from "./chunk-WYO6CB5R.mjs";
import {
  log
} from "./chunk-X3CZISLH.mjs";
import {
  __name
} from "./chunk-Y2CYZVJY.mjs";

// src/rendering-util/rendering-elements/edges.js
import {
  curveBasis,
  curveLinear,
  curveCardinal,
  curveBumpX,
  curveBumpY,
  curveCatmullRom,
  curveMonotoneX,
  curveMonotoneY,
  curveNatural,
  curveStep,
  curveStepAfter,
  curveStepBefore,
  line,
  select
} from "d3";
import rough from "roughjs";

// src/rendering-util/rendering-elements/edgeMarker.ts
var addEdgeMarkers = /* @__PURE__ */ __name((svgPath, edge, url, id, diagramType, useMargin = false, strokeColor) => {
  if (edge.arrowTypeStart) {
    addEdgeMarker(
      svgPath,
      "start",
      edge.arrowTypeStart,
      url,
      id,
      diagramType,
      useMargin,
      strokeColor
    );
  }
  if (edge.arrowTypeEnd) {
    addEdgeMarker(svgPath, "end", edge.arrowTypeEnd, url, id, diagramType, useMargin, strokeColor);
  }
}, "addEdgeMarkers");
var arrowTypesMap = {
  arrow_cross: { type: "cross", fill: false },
  arrow_point: { type: "point", fill: true },
  arrow_barb: { type: "barb", fill: true },
  arrow_barb_neo: { type: "barb", fill: true },
  arrow_circle: { type: "circle", fill: false },
  aggregation: { type: "aggregation", fill: false },
  extension: { type: "extension", fill: false },
  composition: { type: "composition", fill: true },
  dependency: { type: "dependency", fill: true },
  lollipop: { type: "lollipop", fill: false },
  only_one: { type: "onlyOne", fill: false },
  zero_or_one: { type: "zeroOrOne", fill: false },
  one_or_more: { type: "oneOrMore", fill: false },
  zero_or_more: { type: "zeroOrMore", fill: false },
  requirement_arrow: { type: "requirement_arrow", fill: false },
  requirement_contains: { type: "requirement_contains", fill: false }
};
var arrowTypesWithMarginSupport = [
  "cross",
  "point",
  "circle",
  "lollipop",
  "aggregation",
  "extension",
  "composition",
  "dependency",
  "barb"
];
var addEdgeMarker = /* @__PURE__ */ __name((svgPath, position, arrowType, url, id, diagramType, useMargin = false, strokeColor) => {
  const arrowTypeInfo = arrowTypesMap[arrowType];
  const marginSupport = arrowTypeInfo && arrowTypesWithMarginSupport.includes(arrowTypeInfo.type);
  if (!arrowTypeInfo) {
    log.warn(`Unknown arrow type: ${arrowType}`);
    return;
  }
  const endMarkerType = arrowTypeInfo.type;
  const suffix = position === "start" ? "Start" : "End";
  const offset = useMargin && marginSupport ? "-margin" : "";
  const originalMarkerId = `${id}_${diagramType}-${endMarkerType}${suffix}${offset}`;
  if (strokeColor && strokeColor.trim() !== "") {
    const colorId = strokeColor.replace(/[^\dA-Za-z]/g, "_");
    const coloredMarkerId = `${originalMarkerId}_${colorId}`;
    if (!document.getElementById(coloredMarkerId)) {
      const originalMarker = document.getElementById(originalMarkerId);
      if (originalMarker) {
        const coloredMarker = originalMarker.cloneNode(true);
        coloredMarker.id = coloredMarkerId;
        const paths = coloredMarker.querySelectorAll("path, circle, line");
        paths.forEach((path) => {
          path.setAttribute("stroke", strokeColor);
          if (arrowTypeInfo.fill) {
            path.setAttribute("fill", strokeColor);
          }
        });
        originalMarker.parentNode?.appendChild(coloredMarker);
      }
    }
    svgPath.attr(`marker-${position}`, `url(${url}#${coloredMarkerId})`);
  } else {
    svgPath.attr(`marker-${position}`, `url(${url}#${originalMarkerId})`);
  }
}, "addEdgeMarker");

// src/rendering-util/rendering-elements/edges.js
var resolveEdgeCurveType = /* @__PURE__ */ __name((edgeCurve) => {
  return typeof edgeCurve === "string" ? edgeCurve : getConfig2()?.flowchart?.curve;
}, "resolveEdgeCurveType");
var edgeLabels = /* @__PURE__ */ new Map();
var terminalLabels = /* @__PURE__ */ new Map();
var clear = /* @__PURE__ */ __name(() => {
  edgeLabels.clear();
  terminalLabels.clear();
}, "clear");
var getLabelStyles = /* @__PURE__ */ __name((styleArray) => {
  if (!styleArray) {
    return "";
  }
  if (typeof styleArray === "string") {
    return styleArray;
  }
  return styleArray.reduce((acc, style) => acc + ";" + style, "");
}, "getLabelStyles");
var insertEdgeLabel = /* @__PURE__ */ __name(async (elem, edge) => {
  const config = getConfig2();
  let useHtmlLabels = getEffectiveHtmlLabels(config);
  const { labelStyles } = styles2String(edge);
  edge.labelStyle = labelStyles;
  const edgeLabel = elem.insert("g").attr("class", "edgeLabel");
  const label = edgeLabel.insert("g").attr("class", "label").attr("data-id", edge.id);
  const isMarkdown = edge.labelType === "markdown";
  const markdownWidth = void 0;
  const labelElement = await createText(
    elem,
    edge.label,
    {
      style: getLabelStyles(edge.labelStyle),
      useHtmlLabels,
      addSvgBackground: true,
      isNode: false,
      markdown: isMarkdown,
      // Plain text edge labels should auto-wrap, markdown edge labels respect markdownAutoWrap config
      width: isMarkdown ? markdownWidth : void 0
    },
    config
  );
  label.node().appendChild(labelElement);
  log.info("abc82", edge, edge.labelType);
  let bbox = labelElement.getBBox();
  let transformBbox = bbox;
  if (useHtmlLabels) {
    const div = labelElement.children[0];
    const dv = select(labelElement);
    bbox = div.getBoundingClientRect();
    transformBbox = bbox;
    dv.attr("width", bbox.width);
    dv.attr("height", bbox.height);
  } else {
    const textEl = select(labelElement).select("text").node();
    if (textEl && typeof textEl.getBBox === "function") {
      transformBbox = textEl.getBBox();
    }
  }
  label.attr("transform", computeLabelTransform(transformBbox, useHtmlLabels));
  edgeLabels.set(edge.id, edgeLabel);
  edge.width = bbox.width;
  edge.height = bbox.height;
  let fo;
  if (edge.startLabelLeft) {
    const startEdgeLabelLeft = elem.insert("g").attr("class", "edgeTerminals");
    const inner = startEdgeLabelLeft.insert("g").attr("class", "inner");
    const startLabelElement = await createLabel_default(
      inner,
      edge.startLabelLeft,
      getLabelStyles(edge.labelStyle) || "",
      false,
      false
    );
    fo = startLabelElement;
    let slBox = startLabelElement.getBBox();
    if (useHtmlLabels) {
      const div = startLabelElement.children[0];
      const dv = select(startLabelElement);
      slBox = div.getBoundingClientRect();
      dv.attr("width", slBox.width);
      dv.attr("height", slBox.height);
    }
    inner.attr("transform", computeLabelTransform(slBox, useHtmlLabels));
    if (!terminalLabels.get(edge.id)) {
      terminalLabels.set(edge.id, {});
    }
    terminalLabels.get(edge.id).startLeft = startEdgeLabelLeft;
    setTerminalWidth(fo, edge.startLabelLeft);
  }
  if (edge.startLabelRight) {
    const startEdgeLabelRight = elem.insert("g").attr("class", "edgeTerminals");
    const inner = startEdgeLabelRight.insert("g").attr("class", "inner");
    const startLabelElement = await createLabel_default(
      inner,
      edge.startLabelRight,
      getLabelStyles(edge.labelStyle) || "",
      false,
      false
    );
    fo = startLabelElement;
    let slBox = startLabelElement.getBBox();
    if (useHtmlLabels) {
      const div = startLabelElement.children[0];
      const dv = select(startLabelElement);
      slBox = div.getBoundingClientRect();
      dv.attr("width", slBox.width);
      dv.attr("height", slBox.height);
    }
    inner.attr("transform", computeLabelTransform(slBox, useHtmlLabels));
    if (!terminalLabels.get(edge.id)) {
      terminalLabels.set(edge.id, {});
    }
    terminalLabels.get(edge.id).startRight = startEdgeLabelRight;
    setTerminalWidth(fo, edge.startLabelRight);
  }
  if (edge.endLabelLeft) {
    const endEdgeLabelLeft = elem.insert("g").attr("class", "edgeTerminals");
    const inner = endEdgeLabelLeft.insert("g").attr("class", "inner");
    const endLabelElement = await createLabel_default(
      endEdgeLabelLeft,
      edge.endLabelLeft,
      getLabelStyles(edge.labelStyle) || "",
      false,
      false
    );
    fo = endLabelElement;
    let slBox = endLabelElement.getBBox();
    if (useHtmlLabels) {
      const div = endLabelElement.children[0];
      const dv = select(endLabelElement);
      slBox = div.getBoundingClientRect();
      dv.attr("width", slBox.width);
      dv.attr("height", slBox.height);
    }
    inner.attr("transform", computeLabelTransform(slBox, useHtmlLabels));
    if (!terminalLabels.get(edge.id)) {
      terminalLabels.set(edge.id, {});
    }
    terminalLabels.get(edge.id).endLeft = endEdgeLabelLeft;
    setTerminalWidth(fo, edge.endLabelLeft);
  }
  if (edge.endLabelRight) {
    const endEdgeLabelRight = elem.insert("g").attr("class", "edgeTerminals");
    const inner = endEdgeLabelRight.insert("g").attr("class", "inner");
    const endLabelElement = await createLabel_default(
      endEdgeLabelRight,
      edge.endLabelRight,
      getLabelStyles(edge.labelStyle) || "",
      false,
      false
    );
    fo = endLabelElement;
    let slBox = endLabelElement.getBBox();
    if (useHtmlLabels) {
      const div = endLabelElement.children[0];
      const dv = select(endLabelElement);
      slBox = div.getBoundingClientRect();
      dv.attr("width", slBox.width);
      dv.attr("height", slBox.height);
    }
    inner.attr("transform", computeLabelTransform(slBox, useHtmlLabels));
    if (!terminalLabels.get(edge.id)) {
      terminalLabels.set(edge.id, {});
    }
    terminalLabels.get(edge.id).endRight = endEdgeLabelRight;
    setTerminalWidth(fo, edge.endLabelRight);
  }
  return labelElement;
}, "insertEdgeLabel");
function setTerminalWidth(fo, value) {
  if (getEffectiveHtmlLabels(getConfig2()) && fo) {
    fo.style.width = value.length * 9 + "px";
    fo.style.height = "12px";
  }
}
__name(setTerminalWidth, "setTerminalWidth");
var positionEdgeLabel = /* @__PURE__ */ __name((edge, paths) => {
  log.debug("Moving label abc88 ", edge.id, edge.label, edgeLabels.get(edge.id), paths);
  let path = paths.updatedPath ? paths.updatedPath : paths.originalPath;
  const siteConfig = getConfig2();
  const { subGraphTitleTotalMargin } = getSubGraphTitleMargins(siteConfig);
  if (edge.label) {
    const el = edgeLabels.get(edge.id);
    let x = edge.x;
    let y = edge.y;
    if (path) {
      const pos = utils_default.calcLabelPosition(path);
      log.debug(
        "Moving label " + edge.label + " from (",
        x,
        ",",
        y,
        ") to (",
        pos.x,
        ",",
        pos.y,
        ") abc88"
      );
      if (paths.updatedPath) {
        x = pos.x;
        y = pos.y;
      }
    }
    el.attr("transform", `translate(${x}, ${y + subGraphTitleTotalMargin / 2})`);
  }
  if (edge.startLabelLeft) {
    const el = terminalLabels.get(edge.id).startLeft;
    let x = edge.x;
    let y = edge.y;
    if (path) {
      const pos = utils_default.calcTerminalLabelPosition(edge.arrowTypeStart ? 10 : 0, "start_left", path);
      x = pos.x;
      y = pos.y;
    }
    el.attr("transform", `translate(${x}, ${y})`);
  }
  if (edge.startLabelRight) {
    const el = terminalLabels.get(edge.id).startRight;
    let x = edge.x;
    let y = edge.y;
    if (path) {
      const pos = utils_default.calcTerminalLabelPosition(
        edge.arrowTypeStart ? 10 : 0,
        "start_right",
        path
      );
      x = pos.x;
      y = pos.y;
    }
    el.attr("transform", `translate(${x}, ${y})`);
  }
  if (edge.endLabelLeft) {
    const el = terminalLabels.get(edge.id).endLeft;
    let x = edge.x;
    let y = edge.y;
    if (path) {
      const pos = utils_default.calcTerminalLabelPosition(edge.arrowTypeEnd ? 10 : 0, "end_left", path);
      x = pos.x;
      y = pos.y;
    }
    el.attr("transform", `translate(${x}, ${y})`);
  }
  if (edge.endLabelRight) {
    const el = terminalLabels.get(edge.id).endRight;
    let x = edge.x;
    let y = edge.y;
    if (path) {
      const pos = utils_default.calcTerminalLabelPosition(edge.arrowTypeEnd ? 10 : 0, "end_right", path);
      x = pos.x;
      y = pos.y;
    }
    el.attr("transform", `translate(${x}, ${y})`);
  }
}, "positionEdgeLabel");
var orthogonalizeToLabelClippedPoints = /* @__PURE__ */ __name((edge, points) => {
  if (!edge?.isLabelEdge || !edge?.id?.endsWith("-to-label") || !Array.isArray(points)) {
    return points;
  }
  if (points.length !== 2) {
    return points;
  }
  const [start, end] = points;
  const dx = Math.abs(end.x - start.x);
  const dy = Math.abs(end.y - start.y);
  if (dx < 1e-3 || dy < 1e-3) {
    return points;
  }
  if (dy >= dx) {
    return [start, { x: start.x, y: end.y }, end];
  }
  return [start, { x: end.x, y: start.y }, end];
}, "orthogonalizeToLabelClippedPoints");
var outsideNode = /* @__PURE__ */ __name((node, point2) => {
  const x = node.x;
  const y = node.y;
  const dx = Math.abs(point2.x - x);
  const dy = Math.abs(point2.y - y);
  const w = node.width / 2;
  const h = node.height / 2;
  return dx >= w || dy >= h;
}, "outsideNode");
var intersection = /* @__PURE__ */ __name((node, outsidePoint, insidePoint) => {
  log.debug(`intersection calc abc89:
  outsidePoint: ${JSON.stringify(outsidePoint)}
  insidePoint : ${JSON.stringify(insidePoint)}
  node        : x:${node.x} y:${node.y} w:${node.width} h:${node.height}`);
  const x = node.x;
  const y = node.y;
  const dx = Math.abs(x - insidePoint.x);
  const w = node.width / 2;
  let r = insidePoint.x < outsidePoint.x ? w - dx : w + dx;
  const h = node.height / 2;
  const Q = Math.abs(outsidePoint.y - insidePoint.y);
  const R = Math.abs(outsidePoint.x - insidePoint.x);
  if (Math.abs(y - outsidePoint.y) * w > Math.abs(x - outsidePoint.x) * h) {
    let q = insidePoint.y < outsidePoint.y ? outsidePoint.y - h - y : y - h - outsidePoint.y;
    r = R * q / Q;
    const res = {
      x: insidePoint.x < outsidePoint.x ? insidePoint.x + r : insidePoint.x - R + r,
      y: insidePoint.y < outsidePoint.y ? insidePoint.y + Q - q : insidePoint.y - Q + q
    };
    if (r === 0) {
      res.x = outsidePoint.x;
      res.y = outsidePoint.y;
    }
    if (R === 0) {
      res.x = outsidePoint.x;
    }
    if (Q === 0) {
      res.y = outsidePoint.y;
    }
    log.debug(`abc89 top/bottom calc, Q ${Q}, q ${q}, R ${R}, r ${r}`, res);
    return res;
  } else {
    if (insidePoint.x < outsidePoint.x) {
      r = outsidePoint.x - w - x;
    } else {
      r = x - w - outsidePoint.x;
    }
    let q = Q * r / R;
    let _x = insidePoint.x < outsidePoint.x ? insidePoint.x + R - r : insidePoint.x - R + r;
    let _y = insidePoint.y < outsidePoint.y ? insidePoint.y + q : insidePoint.y - q;
    log.debug(`sides calc abc89, Q ${Q}, q ${q}, R ${R}, r ${r}`, { _x, _y });
    if (r === 0) {
      _x = outsidePoint.x;
      _y = outsidePoint.y;
    }
    if (R === 0) {
      _x = outsidePoint.x;
    }
    if (Q === 0) {
      _y = outsidePoint.y;
    }
    return { x: _x, y: _y };
  }
}, "intersection");
var cutPathAtIntersect = /* @__PURE__ */ __name((_points, boundaryNode) => {
  log.warn("abc88 cutPathAtIntersect", _points, boundaryNode);
  let points = [];
  let lastPointOutside = _points[0];
  let isInside = false;
  _points.forEach((point2) => {
    log.info("abc88 checking point", point2, boundaryNode);
    if (!outsideNode(boundaryNode, point2) && !isInside) {
      const inter = intersection(boundaryNode, lastPointOutside, point2);
      log.debug("abc88 inside", point2, lastPointOutside, inter);
      log.debug("abc88 intersection", inter, boundaryNode);
      let pointPresent = false;
      points.forEach((p) => {
        pointPresent = pointPresent || p.x === inter.x && p.y === inter.y;
      });
      if (!points.some((e) => e.x === inter.x && e.y === inter.y)) {
        points.push(inter);
      } else {
        log.warn("abc88 no intersect", inter, points);
      }
      isInside = true;
    } else {
      log.warn("abc88 outside", point2, lastPointOutside);
      lastPointOutside = point2;
      if (!isInside) {
        points.push(point2);
      }
    }
  });
  log.debug("returning points", points);
  return points;
}, "cutPathAtIntersect");
function extractCornerPoints(points) {
  const cornerPoints = [];
  const cornerPointPositions = [];
  for (let i = 1; i < points.length - 1; i++) {
    const prev = points[i - 1];
    const curr = points[i];
    const next = points[i + 1];
    if (prev.x === curr.x && curr.y === next.y && Math.abs(curr.x - next.x) > 5 && Math.abs(curr.y - prev.y) > 5) {
      cornerPoints.push(curr);
      cornerPointPositions.push(i);
    } else if (prev.y === curr.y && curr.x === next.x && Math.abs(curr.x - prev.x) > 5 && Math.abs(curr.y - next.y) > 5) {
      cornerPoints.push(curr);
      cornerPointPositions.push(i);
    }
  }
  return { cornerPoints, cornerPointPositions };
}
__name(extractCornerPoints, "extractCornerPoints");
var findAdjacentPoint = /* @__PURE__ */ __name(function(pointA, pointB, distance) {
  const xDiff = pointB.x - pointA.x;
  const yDiff = pointB.y - pointA.y;
  const length = Math.sqrt(xDiff * xDiff + yDiff * yDiff);
  const ratio = distance / length;
  return { x: pointB.x - ratio * xDiff, y: pointB.y - ratio * yDiff };
}, "findAdjacentPoint");
var fixCorners = /* @__PURE__ */ __name(function(lineData) {
  const { cornerPointPositions } = extractCornerPoints(lineData);
  const newLineData = [];
  for (let i = 0; i < lineData.length; i++) {
    if (cornerPointPositions.includes(i)) {
      const prevPoint = lineData[i - 1];
      const nextPoint = lineData[i + 1];
      const cornerPoint = lineData[i];
      const newPrevPoint = findAdjacentPoint(prevPoint, cornerPoint, 5);
      const newNextPoint = findAdjacentPoint(nextPoint, cornerPoint, 5);
      const xDiff = newNextPoint.x - newPrevPoint.x;
      const yDiff = newNextPoint.y - newPrevPoint.y;
      newLineData.push(newPrevPoint);
      const a = Math.sqrt(2) * 2;
      let newCornerPoint = { x: cornerPoint.x, y: cornerPoint.y };
      if (Math.abs(nextPoint.x - prevPoint.x) > 10 && Math.abs(nextPoint.y - prevPoint.y) >= 10) {
        log.debug(
          "Corner point fixing",
          Math.abs(nextPoint.x - prevPoint.x),
          Math.abs(nextPoint.y - prevPoint.y)
        );
        const r = 5;
        if (cornerPoint.x === newPrevPoint.x) {
          newCornerPoint = {
            x: xDiff < 0 ? newPrevPoint.x - r + a : newPrevPoint.x + r - a,
            y: yDiff < 0 ? newPrevPoint.y - a : newPrevPoint.y + a
          };
        } else {
          newCornerPoint = {
            x: xDiff < 0 ? newPrevPoint.x - a : newPrevPoint.x + a,
            y: yDiff < 0 ? newPrevPoint.y - r + a : newPrevPoint.y + r - a
          };
        }
      } else {
        log.debug(
          "Corner point skipping fixing",
          Math.abs(nextPoint.x - prevPoint.x),
          Math.abs(nextPoint.y - prevPoint.y)
        );
      }
      newLineData.push(newCornerPoint, newNextPoint);
    } else {
      newLineData.push(lineData[i]);
    }
  }
  return newLineData;
}, "fixCorners");
var generateDashArray = /* @__PURE__ */ __name((len, oValueS, oValueE) => {
  const middleLength = len - oValueS - oValueE;
  const dashLength = 2;
  const gapLength = 2;
  const dashGapPairLength = dashLength + gapLength;
  const numberOfPairs = Math.floor(middleLength / dashGapPairLength);
  const middlePattern = Array(numberOfPairs).fill(`${dashLength} ${gapLength}`).join(" ");
  const dashArray = `0 ${oValueS} ${middlePattern} ${oValueE}`;
  return dashArray;
}, "generateDashArray");
var insertEdge = /* @__PURE__ */ __name(function(elem, edge, clusterDb, diagramType, startNode, endNode, diagramId, skipIntersect = false) {
  if (!diagramId) {
    throw new Error(
      `insertEdge: missing diagramId for edge "${edge.id}" \u2014 edge IDs require a diagram prefix for uniqueness`
    );
  }
  const { handDrawnSeed, layout } = getConfig2();
  let points = edge.points;
  let pointsHasChanged = false;
  const tail = startNode;
  var head = endNode;
  const edgeClassStyles = [];
  for (const key in edge.cssCompiledStyles) {
    if (isLabelStyle(key)) {
      continue;
    }
    edgeClassStyles.push(edge.cssCompiledStyles[key]);
  }
  if (layout === "swimlane") {
    if (head.intersect && tail.intersect && Array.isArray(points) && points.length >= 2) {
      if (points.length === 2) {
        points = [tail.intersect(points[0]), head.intersect(points[1])];
      } else {
        const innerPoints = points.slice(1, -1);
        const firstInner = innerPoints[0];
        const lastInner = innerPoints[innerPoints.length - 1];
        const TOLERANCE = 0.5;
        const lastIsPinned = Math.abs(points[points.length - 1].x - lastInner.x) < TOLERANCE && Math.abs(points[points.length - 1].y - lastInner.y) < TOLERANCE;
        const newFirst = tail.intersect(firstInner);
        const newLast = lastIsPinned ? lastInner : head.intersect(lastInner);
        const lastIsDuplicate = Math.abs(newLast.x - lastInner.x) < TOLERANCE && Math.abs(newLast.y - lastInner.y) < TOLERANCE;
        const firstIsDuplicate = Math.abs(newFirst.x - firstInner.x) < TOLERANCE && Math.abs(newFirst.y - firstInner.y) < TOLERANCE;
        const startPoints = firstIsDuplicate ? [] : [newFirst];
        const endPoints = lastIsDuplicate ? [] : [newLast];
        points = [...startPoints, ...innerPoints, ...endPoints];
      }
    }
    points = orthogonalizeToLabelClippedPoints(edge, points);
  } else if (head.intersect && tail.intersect && !skipIntersect) {
    points = points.slice(1, edge.points.length - 1);
    points.unshift(tail.intersect(points[0]));
    points.push(head.intersect(points[points.length - 1]));
  }
  const pointsStr = btoa(JSON.stringify(points));
  if (edge.toCluster) {
    log.info("to cluster abc88", clusterDb.get(edge.toCluster));
    points = cutPathAtIntersect(edge.points, clusterDb.get(edge.toCluster).node);
    pointsHasChanged = true;
  }
  if (edge.fromCluster) {
    log.debug(
      "from cluster abc88",
      clusterDb.get(edge.fromCluster),
      JSON.stringify(points, null, 2)
    );
    points = cutPathAtIntersect(points.reverse(), clusterDb.get(edge.fromCluster).node).reverse();
    pointsHasChanged = true;
  }
  let lineData = points.filter((p) => !Number.isNaN(p.y));
  const edgeCurveType = resolveEdgeCurveType(edge.curve);
  if (edgeCurveType !== "rounded") {
    lineData = fixCorners(lineData);
  }
  let curve = curveLinear;
  switch (edgeCurveType) {
    case "linear":
      curve = curveLinear;
      break;
    case "basis":
      curve = curveBasis;
      break;
    case "cardinal":
      curve = curveCardinal;
      break;
    case "bumpX":
      curve = curveBumpX;
      break;
    case "bumpY":
      curve = curveBumpY;
      break;
    case "catmullRom":
      curve = curveCatmullRom;
      break;
    case "monotoneX":
      curve = curveMonotoneX;
      break;
    case "monotoneY":
      curve = curveMonotoneY;
      break;
    case "natural":
      curve = curveNatural;
      break;
    case "step":
      curve = curveStep;
      break;
    case "stepAfter":
      curve = curveStepAfter;
      break;
    case "stepBefore":
      curve = curveStepBefore;
      break;
    case "rounded":
      curve = curveLinear;
      break;
    default:
      curve = curveBasis;
  }
  const { x, y } = getLineFunctionsWithOffset(edge);
  const lineFunction = line().x(x).y(y).curve(curve);
  let strokeClasses;
  switch (edge.thickness) {
    case "normal":
      strokeClasses = "edge-thickness-normal";
      break;
    case "thick":
      strokeClasses = "edge-thickness-thick";
      break;
    case "invisible":
      strokeClasses = "edge-thickness-invisible";
      break;
    default:
      strokeClasses = "edge-thickness-normal";
  }
  switch (edge.pattern) {
    case "solid":
      strokeClasses += " edge-pattern-solid";
      break;
    case "dotted":
      strokeClasses += " edge-pattern-dotted";
      break;
    case "dashed":
      strokeClasses += " edge-pattern-dashed";
      break;
    default:
      strokeClasses += " edge-pattern-solid";
  }
  let svgPath;
  let linePath = edgeCurveType === "rounded" ? generateRoundedPath(applyMarkerOffsetsToPoints(lineData, edge), 5) : lineFunction(lineData);
  const edgeStyles = Array.isArray(edge.style) ? edge.style : [edge.style];
  let strokeColor = edgeStyles.find((style) => style?.startsWith("stroke:"));
  let animationClass = "";
  if (edge.animate) {
    animationClass = "edge-animation-fast";
  }
  if (edge.animation) {
    animationClass = "edge-animation-" + edge.animation;
  }
  let animatedEdge = false;
  if (edge.look === "handDrawn") {
    const rc = rough.svg(elem);
    Object.assign([], lineData);
    const svgPathNode = rc.path(linePath, {
      roughness: 0.3,
      seed: handDrawnSeed
    });
    strokeClasses += " transition";
    svgPath = select(svgPathNode).select("path").attr("id", `${diagramId}-${edge.id}`).attr(
      "class",
      " " + strokeClasses + (edge.classes ? " " + edge.classes : "") + (animationClass ? " " + animationClass : "")
    ).attr("style", edgeStyles ? edgeStyles.reduce((acc, style) => acc + ";" + style, "") : "");
    let d = svgPath.attr("d");
    svgPath.attr("d", d);
    elem.node().appendChild(svgPath.node());
  } else {
    const stylesFromClasses = edgeClassStyles.join(";");
    const styles = edgeStyles ? edgeStyles.reduce((acc, style) => acc + style + ";", "") : "";
    const pathStyle = (stylesFromClasses ? stylesFromClasses + ";" + styles + ";" : styles) + ";" + (edgeStyles ? edgeStyles.reduce((acc, style) => acc + ";" + style, "") : "");
    svgPath = elem.append("path").attr("d", linePath).attr("id", `${diagramId}-${edge.id}`).attr(
      "class",
      " " + strokeClasses + (edge.classes ? " " + edge.classes : "") + (animationClass ? " " + animationClass : "")
    ).attr("style", pathStyle);
    strokeColor = pathStyle.match(/stroke:([^;]+)/)?.[1];
    animatedEdge = edge.animate === true || !!edge.animation || stylesFromClasses.includes("animation");
    const pathNode = svgPath.node();
    const len = typeof pathNode.getTotalLength === "function" ? pathNode.getTotalLength() : 0;
    const oValueS = markerOffsets2[edge.arrowTypeStart] || 0;
    const oValueE = markerOffsets2[edge.arrowTypeEnd] || 0;
    if (edge.look === "neo" && !animatedEdge) {
      const dashArray = edge.pattern === "dotted" || edge.pattern === "dashed" ? generateDashArray(len, oValueS, oValueE) : `0 ${oValueS} ${len - oValueS - oValueE} ${oValueE}`;
      const mOffset = `stroke-dasharray: ${dashArray}; stroke-dashoffset: 0;`;
      svgPath.attr("style", mOffset + svgPath.attr("style"));
    }
  }
  svgPath.attr("data-edge", true);
  svgPath.attr("data-et", "edge");
  svgPath.attr("data-id", edge.id);
  svgPath.attr("data-points", pointsStr);
  svgPath.attr("data-look", handleUndefinedAttr(edge.look));
  if (edge.showPoints) {
    lineData.forEach((point3) => {
      elem.append("circle").style("stroke", "red").style("fill", "red").attr("r", 1).attr("cx", point3.x).attr("cy", point3.y);
    });
  }
  let url = "";
  if (getConfig2().flowchart.arrowMarkerAbsolute || getConfig2().state.arrowMarkerAbsolute) {
    url = window.location.protocol + "//" + window.location.host + window.location.pathname + window.location.search;
    url = url.replace(/\(/g, "\\(").replace(/\)/g, "\\)");
  }
  log.info("arrowTypeStart", edge.arrowTypeStart);
  log.info("arrowTypeEnd", edge.arrowTypeEnd);
  const useMargin = !animatedEdge && edge?.look === "neo";
  addEdgeMarkers(svgPath, edge, url, diagramId, diagramType, useMargin, strokeColor);
  const midIndex = Math.floor(points.length / 2);
  const point2 = points[midIndex];
  if (!utils_default.isLabelCoordinateInPath(point2, svgPath.attr("d"))) {
    pointsHasChanged = true;
  }
  let paths = {};
  if (pointsHasChanged) {
    paths.updatedPath = points;
  }
  paths.originalPath = edge.points;
  return paths;
}, "insertEdge");
function generateRoundedPath(points, radius) {
  if (points.length < 2) {
    return "";
  }
  let path = "";
  const size = points.length;
  const epsilon = 1e-5;
  for (let i = 0; i < size; i++) {
    const currPoint = points[i];
    const prevPoint = points[i - 1];
    const nextPoint = points[i + 1];
    if (i === 0) {
      path += `M${currPoint.x},${currPoint.y}`;
    } else if (i === size - 1) {
      path += `L${currPoint.x},${currPoint.y}`;
    } else {
      const dx1 = currPoint.x - prevPoint.x;
      const dy1 = currPoint.y - prevPoint.y;
      const dx2 = nextPoint.x - currPoint.x;
      const dy2 = nextPoint.y - currPoint.y;
      const len1 = Math.hypot(dx1, dy1);
      const len2 = Math.hypot(dx2, dy2);
      if (len1 < epsilon || len2 < epsilon) {
        path += `L${currPoint.x},${currPoint.y}`;
        continue;
      }
      const nx1 = dx1 / len1;
      const ny1 = dy1 / len1;
      const nx2 = dx2 / len2;
      const ny2 = dy2 / len2;
      const dot = nx1 * nx2 + ny1 * ny2;
      const clampedDot = Math.max(-1, Math.min(1, dot));
      const angle = Math.acos(clampedDot);
      if (angle < epsilon || Math.abs(Math.PI - angle) < epsilon) {
        path += `L${currPoint.x},${currPoint.y}`;
        continue;
      }
      const cutLen = Math.min(radius / Math.sin(angle / 2), len1 / 2, len2 / 2);
      const startX = currPoint.x - nx1 * cutLen;
      const startY = currPoint.y - ny1 * cutLen;
      const endX = currPoint.x + nx2 * cutLen;
      const endY = currPoint.y + ny2 * cutLen;
      path += `L${startX},${startY}`;
      path += `Q${currPoint.x},${currPoint.y} ${endX},${endY}`;
    }
  }
  return path;
}
__name(generateRoundedPath, "generateRoundedPath");
function calculateDeltaAndAngle(point1, point2) {
  if (!point1 || !point2) {
    return { angle: 0, deltaX: 0, deltaY: 0 };
  }
  const deltaX = point2.x - point1.x;
  const deltaY = point2.y - point1.y;
  const angle = Math.atan2(deltaY, deltaX);
  return { angle, deltaX, deltaY };
}
__name(calculateDeltaAndAngle, "calculateDeltaAndAngle");
function applyMarkerOffsetsToPoints(points, edge) {
  const newPoints = points.map((point2) => ({ ...point2 }));
  if (points.length >= 2 && markerOffsets[edge.arrowTypeStart]) {
    const offsetValue = markerOffsets[edge.arrowTypeStart];
    const point1 = points[0];
    const point2 = points[1];
    const { angle } = calculateDeltaAndAngle(point1, point2);
    const offsetX = offsetValue * Math.cos(angle);
    const offsetY = offsetValue * Math.sin(angle);
    newPoints[0].x = point1.x + offsetX;
    newPoints[0].y = point1.y + offsetY;
  }
  const n = points.length;
  if (n >= 2 && markerOffsets[edge.arrowTypeEnd]) {
    const offsetValue = markerOffsets[edge.arrowTypeEnd];
    const point1 = points[n - 1];
    const point2 = points[n - 2];
    const { angle } = calculateDeltaAndAngle(point2, point1);
    const offsetX = offsetValue * Math.cos(angle);
    const offsetY = offsetValue * Math.sin(angle);
    newPoints[n - 1].x = point1.x - offsetX;
    newPoints[n - 1].y = point1.y - offsetY;
  }
  return newPoints;
}
__name(applyMarkerOffsetsToPoints, "applyMarkerOffsetsToPoints");

// src/rendering-util/rendering-elements/markers.js
var insertMarkers = /* @__PURE__ */ __name((elem, markerArray, type, id) => {
  markerArray.forEach((markerName) => {
    markers[markerName](elem, type, id);
  });
}, "insertMarkers");
var extension = /* @__PURE__ */ __name((elem, type, id) => {
  log.trace("Making markers for ", id);
  elem.append("defs").append("marker").attr("id", id + "_" + type + "-extensionStart").attr("class", "marker extension " + type).attr("refX", 18).attr("refY", 7).attr("markerWidth", 20).attr("markerHeight", 28).attr("orient", "auto").attr("markerUnits", "userSpaceOnUse").append("path").attr("d", "M 1,7 L18,13 V 1 Z");
  elem.append("defs").append("marker").attr("id", id + "_" + type + "-extensionEnd").attr("class", "marker extension " + type).attr("refX", 1).attr("refY", 7).attr("markerWidth", 20).attr("markerHeight", 28).attr("orient", "auto").append("path").attr("d", "M 1,1 V 13 L18,7 Z");
  elem.append("marker").attr("id", id + "_" + type + "-extensionStart-margin").attr("class", "marker extension " + type).attr("refX", 18).attr("refY", 7).attr("markerWidth", 20).attr("markerHeight", 28).attr("orient", "auto").attr("markerUnits", "userSpaceOnUse").attr("viewBox", "0 0 20 14").append("polygon").attr("points", "10,7 18,13 18,1").style("stroke-width", 2).style("stroke-dasharray", "0");
  elem.append("defs").append("marker").attr("id", id + "_" + type + "-extensionEnd-margin").attr("class", "marker extension " + type).attr("refX", 9).attr("refY", 7).attr("markerWidth", 20).attr("markerHeight", 28).attr("orient", "auto").attr("markerUnits", "userSpaceOnUse").attr("viewBox", "0 0 20 14").append("polygon").attr("points", "10,1 10,13 18,7").style("stroke-width", 2).style("stroke-dasharray", "0");
}, "extension");
var composition = /* @__PURE__ */ __name((elem, type, id) => {
  elem.append("defs").append("marker").attr("id", id + "_" + type + "-compositionStart").attr("class", "marker composition " + type).attr("refX", 18).attr("refY", 7).attr("markerWidth", 190).attr("markerHeight", 240).attr("orient", "auto").append("path").attr("d", "M 18,7 L9,13 L1,7 L9,1 Z");
  elem.append("defs").append("marker").attr("id", id + "_" + type + "-compositionEnd").attr("class", "marker composition " + type).attr("refX", 1).attr("refY", 7).attr("markerWidth", 20).attr("markerHeight", 28).attr("orient", "auto").append("path").attr("d", "M 18,7 L9,13 L1,7 L9,1 Z");
  elem.append("defs").append("marker").attr("id", id + "_" + type + "-compositionStart-margin").attr("class", "marker composition " + type).attr("refX", 15).attr("refY", 7).attr("markerWidth", 190).attr("markerHeight", 240).attr("orient", "auto").attr("markerUnits", "userSpaceOnUse").append("path").style("stroke-width", 0).attr("viewBox", "0 0 15 15").attr("d", "M 18,7 L9,13 L1,7 L9,1 Z");
  elem.append("defs").append("marker").attr("id", id + "_" + type + "-compositionEnd-margin").attr("class", "marker composition " + type).attr("refX", 3.5).attr("refY", 7).attr("markerWidth", 20).attr("markerHeight", 28).attr("orient", "auto").attr("markerUnits", "userSpaceOnUse").append("path").style("stroke-width", 0).attr("d", "M 18,7 L9,13 L1,7 L9,1 Z");
}, "composition");
var aggregation = /* @__PURE__ */ __name((elem, type, id) => {
  elem.append("defs").append("marker").attr("id", id + "_" + type + "-aggregationStart").attr("class", "marker aggregation " + type).attr("refX", 18).attr("refY", 7).attr("markerWidth", 190).attr("markerHeight", 240).attr("orient", "auto").append("path").attr("d", "M 18,7 L9,13 L1,7 L9,1 Z");
  elem.append("defs").append("marker").attr("id", id + "_" + type + "-aggregationEnd").attr("class", "marker aggregation " + type).attr("refX", 1).attr("refY", 7).attr("markerWidth", 20).attr("markerHeight", 28).attr("orient", "auto").append("path").attr("d", "M 18,7 L9,13 L1,7 L9,1 Z");
  elem.append("defs").append("marker").attr("id", id + "_" + type + "-aggregationStart-margin").attr("class", "marker aggregation " + type).attr("refX", 15).attr("refY", 7).attr("markerWidth", 190).attr("markerHeight", 240).attr("orient", "auto").attr("markerUnits", "userSpaceOnUse").append("path").style("stroke-width", 2).attr("d", "M 18,7 L9,13 L1,7 L9,1 Z");
  elem.append("defs").append("marker").attr("id", id + "_" + type + "-aggregationEnd-margin").attr("class", "marker aggregation " + type).attr("refX", 1).attr("refY", 7).attr("markerWidth", 20).attr("markerHeight", 28).attr("orient", "auto").attr("markerUnits", "userSpaceOnUse").append("path").style("stroke-width", 2).attr("d", "M 18,7 L9,13 L1,7 L9,1 Z");
}, "aggregation");
var dependency = /* @__PURE__ */ __name((elem, type, id) => {
  elem.append("defs").append("marker").attr("id", id + "_" + type + "-dependencyStart").attr("class", "marker dependency " + type).attr("refX", 6).attr("refY", 7).attr("markerWidth", 190).attr("markerHeight", 240).attr("orient", "auto").append("path").attr("d", "M 5,7 L9,13 L1,7 L9,1 Z");
  elem.append("defs").append("marker").attr("id", id + "_" + type + "-dependencyEnd").attr("class", "marker dependency " + type).attr("refX", 13).attr("refY", 7).attr("markerWidth", 20).attr("markerHeight", 28).attr("orient", "auto").append("path").attr("d", "M 18,7 L9,13 L14,7 L9,1 Z");
  elem.append("defs").append("marker").attr("id", id + "_" + type + "-dependencyStart-margin").attr("class", "marker dependency " + type).attr("refX", 4).attr("refY", 7).attr("markerWidth", 190).attr("markerHeight", 240).attr("orient", "auto").attr("markerUnits", "userSpaceOnUse").append("path").style("stroke-width", 0).attr("d", "M 5,7 L9,13 L1,7 L9,1 Z");
  elem.append("defs").append("marker").attr("id", id + "_" + type + "-dependencyEnd-margin").attr("class", "marker dependency " + type).attr("refX", 16).attr("refY", 7).attr("markerWidth", 20).attr("markerHeight", 28).attr("orient", "auto").attr("markerUnits", "userSpaceOnUse").append("path").style("stroke-width", 0).attr("d", "M 18,7 L9,13 L14,7 L9,1 Z");
}, "dependency");
var lollipop = /* @__PURE__ */ __name((elem, type, id) => {
  elem.append("defs").append("marker").attr("id", id + "_" + type + "-lollipopStart").attr("class", "marker lollipop " + type).attr("refX", 13).attr("refY", 7).attr("markerWidth", 190).attr("markerHeight", 240).attr("orient", "auto").append("circle").attr("fill", "transparent").attr("cx", 7).attr("cy", 7).attr("r", 6);
  elem.append("defs").append("marker").attr("id", id + "_" + type + "-lollipopEnd").attr("class", "marker lollipop " + type).attr("refX", 1).attr("refY", 7).attr("markerWidth", 190).attr("markerHeight", 240).attr("orient", "auto").append("circle").attr("fill", "transparent").attr("cx", 7).attr("cy", 7).attr("r", 6);
  elem.append("defs").append("marker").attr("id", id + "_" + type + "-lollipopStart-margin").attr("class", "marker lollipop " + type).attr("refX", 13).attr("refY", 7).attr("markerWidth", 190).attr("markerHeight", 240).attr("orient", "auto").attr("markerUnits", "userSpaceOnUse").append("circle").attr("fill", "transparent").attr("cx", 7).attr("cy", 7).attr("r", 6).attr("stroke-width", 2);
  elem.append("defs").append("marker").attr("id", id + "_" + type + "-lollipopEnd-margin").attr("class", "marker lollipop " + type).attr("refX", 1).attr("refY", 7).attr("markerWidth", 190).attr("markerHeight", 240).attr("orient", "auto").attr("markerUnits", "userSpaceOnUse").append("circle").attr("fill", "transparent").attr("cx", 7).attr("cy", 7).attr("r", 6).attr("stroke-width", 2);
}, "lollipop");
var point = /* @__PURE__ */ __name((elem, type, id) => {
  elem.append("marker").attr("id", id + "_" + type + "-pointEnd").attr("class", "marker " + type).attr("viewBox", "0 0 10 10").attr("refX", 5).attr("refY", 5).attr("markerUnits", "userSpaceOnUse").attr("markerWidth", 8).attr("markerHeight", 8).attr("orient", "auto").append("path").attr("d", "M 0 0 L 10 5 L 0 10 z").attr("class", "arrowMarkerPath").style("stroke-width", 1).style("stroke-dasharray", "1,0");
  elem.append("marker").attr("id", id + "_" + type + "-pointStart").attr("class", "marker " + type).attr("viewBox", "0 0 10 10").attr("refX", 4.5).attr("refY", 5).attr("markerUnits", "userSpaceOnUse").attr("markerWidth", 8).attr("markerHeight", 8).attr("orient", "auto").append("path").attr("d", "M 0 5 L 10 10 L 10 0 z").attr("class", "arrowMarkerPath").style("stroke-width", 1).style("stroke-dasharray", "1,0");
  elem.append("marker").attr("id", id + "_" + type + "-pointEnd-margin").attr("class", "marker " + type).attr("viewBox", "0 0 11.5 14").attr("refX", 11.5).attr("refY", 7).attr("markerUnits", "userSpaceOnUse").attr("markerWidth", 10.5).attr("markerHeight", 14).attr("orient", "auto").append("path").attr("d", "M 0 0 L 11.5 7 L 0 14 z").attr("class", "arrowMarkerPath").style("stroke-width", 0).style("stroke-dasharray", "1,0");
  elem.append("marker").attr("id", id + "_" + type + "-pointStart-margin").attr("class", "marker " + type).attr("viewBox", "0 0 11.5 14").attr("refX", 1).attr("refY", 7).attr("markerUnits", "userSpaceOnUse").attr("markerWidth", 11.5).attr("markerHeight", 14).attr("orient", "auto").append("polygon").attr("points", "0,7 11.5,14 11.5,0").attr("class", "arrowMarkerPath").style("stroke-width", 0).style("stroke-dasharray", "1,0");
}, "point");
var circle = /* @__PURE__ */ __name((elem, type, id) => {
  elem.append("marker").attr("id", id + "_" + type + "-circleEnd").attr("class", "marker " + type).attr("viewBox", "0 0 10 10").attr("refX", 11).attr("refY", 5).attr("markerUnits", "userSpaceOnUse").attr("markerWidth", 11).attr("markerHeight", 11).attr("orient", "auto").append("circle").attr("cx", "5").attr("cy", "5").attr("r", "5").attr("class", "arrowMarkerPath").style("stroke-width", 1).style("stroke-dasharray", "1,0");
  elem.append("marker").attr("id", id + "_" + type + "-circleStart").attr("class", "marker " + type).attr("viewBox", "0 0 10 10").attr("refX", -1).attr("refY", 5).attr("markerUnits", "userSpaceOnUse").attr("markerWidth", 11).attr("markerHeight", 11).attr("orient", "auto").append("circle").attr("cx", "5").attr("cy", "5").attr("r", "5").attr("class", "arrowMarkerPath").style("stroke-width", 1).style("stroke-dasharray", "1,0");
  elem.append("marker").attr("id", id + "_" + type + "-circleEnd-margin").attr("class", "marker " + type).attr("viewBox", "0 0 10 10").attr("refY", 5).attr("refX", 12.25).attr("markerUnits", "userSpaceOnUse").attr("markerWidth", 14).attr("markerHeight", 14).attr("orient", "auto").append("circle").attr("cx", "5").attr("cy", "5").attr("r", "5").attr("class", "arrowMarkerPath").style("stroke-width", 0).style("stroke-dasharray", "1,0");
  elem.append("marker").attr("id", id + "_" + type + "-circleStart-margin").attr("class", "marker " + type).attr("viewBox", "0 0 10 10").attr("refX", -2).attr("refY", 5).attr("markerUnits", "userSpaceOnUse").attr("markerWidth", 14).attr("markerHeight", 14).attr("orient", "auto").append("circle").attr("cx", "5").attr("cy", "5").attr("r", "5").attr("class", "arrowMarkerPath").style("stroke-width", 0).style("stroke-dasharray", "1,0");
}, "circle");
var cross = /* @__PURE__ */ __name((elem, type, id) => {
  elem.append("marker").attr("id", id + "_" + type + "-crossEnd").attr("class", "marker cross " + type).attr("viewBox", "0 0 11 11").attr("refX", 12).attr("refY", 5.2).attr("markerUnits", "userSpaceOnUse").attr("markerWidth", 11).attr("markerHeight", 11).attr("orient", "auto").append("path").attr("d", "M 1,1 l 9,9 M 10,1 l -9,9").attr("class", "arrowMarkerPath").style("stroke-width", 2).style("stroke-dasharray", "1,0");
  elem.append("marker").attr("id", id + "_" + type + "-crossStart").attr("class", "marker cross " + type).attr("viewBox", "0 0 11 11").attr("refX", -1).attr("refY", 5.2).attr("markerUnits", "userSpaceOnUse").attr("markerWidth", 11).attr("markerHeight", 11).attr("orient", "auto").append("path").attr("d", "M 1,1 l 9,9 M 10,1 l -9,9").attr("class", "arrowMarkerPath").style("stroke-width", 2).style("stroke-dasharray", "1,0");
  elem.append("marker").attr("id", id + "_" + type + "-crossEnd-margin").attr("class", "marker cross " + type).attr("viewBox", "0 0 15 15").attr("refX", 17.7).attr("refY", 7.5).attr("markerUnits", "userSpaceOnUse").attr("markerWidth", 12).attr("markerHeight", 12).attr("orient", "auto").append("path").attr("d", "M 1,1 L 14,14 M 1,14 L 14,1").attr("class", "arrowMarkerPath").style("stroke-width", 2.5);
  elem.append("marker").attr("id", id + "_" + type + "-crossStart-margin").attr("class", "marker cross " + type).attr("viewBox", "0 0 15 15").attr("refX", -3.5).attr("refY", 7.5).attr("markerUnits", "userSpaceOnUse").attr("markerWidth", 12).attr("markerHeight", 12).attr("orient", "auto").append("path").attr("d", "M 1,1 L 14,14 M 1,14 L 14,1").attr("class", "arrowMarkerPath").style("stroke-width", 2.5).style("stroke-dasharray", "1,0");
}, "cross");
var barb = /* @__PURE__ */ __name((elem, type, id) => {
  elem.append("defs").append("marker").attr("id", id + "_" + type + "-barbEnd").attr("refX", 19).attr("refY", 7).attr("markerWidth", 20).attr("markerHeight", 14).attr("markerUnits", "userSpaceOnUse").attr("orient", "auto").append("path").attr("d", "M 19,7 L9,13 L14,7 L9,1 Z");
}, "barb");
var barbNeo = /* @__PURE__ */ __name((elem, type, id) => {
  const config = getConfig();
  const { themeVariables } = config;
  const { transitionColor } = themeVariables;
  elem.append("defs").append("marker").attr("id", id + "_" + type + "-barbEnd").attr("refX", 19).attr("refY", 7).attr("markerWidth", 20).attr("markerHeight", 14).attr("markerUnits", "strokeWidth").attr("orient", "auto").append("path").attr("d", "M 19,7 L11,14 L13,7 L11,0 Z");
  elem.append("defs").append("marker").attr("id", id + "_" + type + "-barbEnd-margin").attr("refX", 17).attr("refY", 7).attr("markerWidth", 20).attr("markerHeight", 14).attr("markerUnits", "userSpaceOnUse").attr("orient", "auto").append("path").attr("d", "M 19,7 L11,14 L13,7 L11,0 Z").attr("fill", `${transitionColor}`);
}, "barbNeo");
var only_one = /* @__PURE__ */ __name((elem, type, id) => {
  elem.append("defs").append("marker").attr("id", id + "_" + type + "-onlyOneStart").attr("class", "marker onlyOne " + type).attr("refX", 0).attr("refY", 9).attr("markerWidth", 18).attr("markerHeight", 18).attr("orient", "auto").append("path").attr("d", "M9,0 L9,18 M15,0 L15,18");
  elem.append("defs").append("marker").attr("id", id + "_" + type + "-onlyOneEnd").attr("class", "marker onlyOne " + type).attr("refX", 18).attr("refY", 9).attr("markerWidth", 18).attr("markerHeight", 18).attr("orient", "auto").append("path").attr("d", "M3,0 L3,18 M9,0 L9,18");
}, "only_one");
var zero_or_one = /* @__PURE__ */ __name((elem, type, id) => {
  const startMarker = elem.append("defs").append("marker").attr("id", id + "_" + type + "-zeroOrOneStart").attr("class", "marker zeroOrOne " + type).attr("refX", 0).attr("refY", 9).attr("markerWidth", 30).attr("markerHeight", 18).attr("orient", "auto");
  startMarker.append("circle").attr("fill", "white").attr("cx", 21).attr("cy", 9).attr("r", 6);
  startMarker.append("path").attr("d", "M9,0 L9,18");
  const endMarker = elem.append("defs").append("marker").attr("id", id + "_" + type + "-zeroOrOneEnd").attr("class", "marker zeroOrOne " + type).attr("refX", 30).attr("refY", 9).attr("markerWidth", 30).attr("markerHeight", 18).attr("orient", "auto");
  endMarker.append("circle").attr("fill", "white").attr("cx", 9).attr("cy", 9).attr("r", 6);
  endMarker.append("path").attr("d", "M21,0 L21,18");
}, "zero_or_one");
var one_or_more = /* @__PURE__ */ __name((elem, type, id) => {
  elem.append("defs").append("marker").attr("id", id + "_" + type + "-oneOrMoreStart").attr("class", "marker oneOrMore " + type).attr("refX", 18).attr("refY", 18).attr("markerWidth", 45).attr("markerHeight", 36).attr("orient", "auto").append("path").attr("d", "M0,18 Q 18,0 36,18 Q 18,36 0,18 M42,9 L42,27");
  elem.append("defs").append("marker").attr("id", id + "_" + type + "-oneOrMoreEnd").attr("class", "marker oneOrMore " + type).attr("refX", 27).attr("refY", 18).attr("markerWidth", 45).attr("markerHeight", 36).attr("orient", "auto").append("path").attr("d", "M3,9 L3,27 M9,18 Q27,0 45,18 Q27,36 9,18");
}, "one_or_more");
var zero_or_more = /* @__PURE__ */ __name((elem, type, id) => {
  const startMarker = elem.append("defs").append("marker").attr("id", id + "_" + type + "-zeroOrMoreStart").attr("class", "marker zeroOrMore " + type).attr("refX", 18).attr("refY", 18).attr("markerWidth", 57).attr("markerHeight", 36).attr("orient", "auto");
  startMarker.append("circle").attr("fill", "white").attr("cx", 48).attr("cy", 18).attr("r", 6);
  startMarker.append("path").attr("d", "M0,18 Q18,0 36,18 Q18,36 0,18");
  const endMarker = elem.append("defs").append("marker").attr("id", id + "_" + type + "-zeroOrMoreEnd").attr("class", "marker zeroOrMore " + type).attr("refX", 39).attr("refY", 18).attr("markerWidth", 57).attr("markerHeight", 36).attr("orient", "auto");
  endMarker.append("circle").attr("fill", "white").attr("cx", 9).attr("cy", 18).attr("r", 6);
  endMarker.append("path").attr("d", "M21,18 Q39,0 57,18 Q39,36 21,18");
}, "zero_or_more");
var only_one_neo = /* @__PURE__ */ __name((elem, type, id) => {
  const config = getConfig();
  const { themeVariables } = config;
  const { strokeWidth } = themeVariables;
  elem.append("defs").append("marker").attr("id", id + "_" + type + "-onlyOneStart").attr("class", "marker onlyOne " + type).attr("refX", 0).attr("refY", 9).attr("markerWidth", 18).attr("markerHeight", 18).attr("orient", "auto").attr("markerUnits", "userSpaceOnUse").append("path").attr("d", "M9,0 L9,18 M15,0 L15,18").attr("stroke-width", `${strokeWidth}`);
  elem.append("defs").append("marker").attr("id", id + "_" + type + "-onlyOneEnd").attr("class", "marker onlyOne " + type).attr("refX", 18).attr("refY", 9).attr("markerWidth", 18).attr("markerHeight", 18).attr("orient", "auto").attr("markerUnits", "userSpaceOnUse").append("path").attr("d", "M3,0 L3,18 M9,0 L9,18").attr("stroke-width", `${strokeWidth}`);
}, "only_one_neo");
var zero_or_one_neo = /* @__PURE__ */ __name((elem, type, id) => {
  const config = getConfig();
  const { themeVariables } = config;
  const { strokeWidth, mainBkg } = themeVariables;
  const startMarker = elem.append("defs").append("marker").attr("id", id + "_" + type + "-zeroOrOneStart").attr("class", "marker zeroOrOne " + type).attr("refX", 0).attr("refY", 9).attr("markerWidth", 30).attr("markerHeight", 18).attr("orient", "auto").attr("markerUnits", "userSpaceOnUse");
  startMarker.append("circle").attr("fill", mainBkg ?? "white").attr("cx", 21).attr("cy", 9).attr("stroke-width", `${strokeWidth}`).attr("r", 6);
  startMarker.append("path").attr("d", "M9,0 L9,18").attr("stroke-width", `${strokeWidth}`);
  const endMarker = elem.append("defs").append("marker").attr("id", id + "_" + type + "-zeroOrOneEnd").attr("class", "marker zeroOrOne " + type).attr("refX", 30).attr("refY", 9).attr("markerWidth", 30).attr("markerHeight", 18).attr("markerUnits", "userSpaceOnUse").attr("orient", "auto");
  endMarker.append("circle").attr("fill", mainBkg ?? "white").attr("cx", 9).attr("cy", 9).attr("stroke-width", `${strokeWidth}`).attr("r", 6);
  endMarker.append("path").attr("d", "M21,0 L21,18").attr("stroke-width", `${strokeWidth}`);
}, "zero_or_one_neo");
var one_or_more_neo = /* @__PURE__ */ __name((elem, type, id) => {
  const config = getConfig();
  const { themeVariables } = config;
  const { strokeWidth } = themeVariables;
  elem.append("defs").append("marker").attr("id", id + "_" + type + "-oneOrMoreStart").attr("class", "marker oneOrMore " + type).attr("refX", 18).attr("refY", 18).attr("markerWidth", 45).attr("markerHeight", 36).attr("orient", "auto").attr("markerUnits", "userSpaceOnUse").append("path").attr("d", "M0,18 Q 18,0 36,18 Q 18,36 0,18 M42,9 L42,27").attr("stroke-width", `${strokeWidth}`);
  elem.append("defs").append("marker").attr("id", id + "_" + type + "-oneOrMoreEnd").attr("class", "marker oneOrMore " + type).attr("refX", 27).attr("refY", 18).attr("markerWidth", 45).attr("markerHeight", 36).attr("markerUnits", "userSpaceOnUse").attr("orient", "auto").append("path").attr("d", "M3,9 L3,27 M9,18 Q27,0 45,18 Q27,36 9,18").attr("stroke-width", `${strokeWidth}`);
}, "one_or_more_neo");
var zero_or_more_neo = /* @__PURE__ */ __name((elem, type, id) => {
  const config = getConfig();
  const { themeVariables } = config;
  const { strokeWidth, mainBkg } = themeVariables;
  const startMarker = elem.append("defs").append("marker").attr("id", id + "_" + type + "-zeroOrMoreStart").attr("class", "marker zeroOrMore " + type).attr("refX", 18).attr("refY", 18).attr("markerWidth", 57).attr("markerHeight", 36).attr("markerUnits", "userSpaceOnUse").attr("orient", "auto");
  startMarker.append("circle").attr("fill", mainBkg ?? "white").attr("cx", 45.5).attr("cy", 18).attr("r", 6).attr("stroke-width", `${strokeWidth}`);
  startMarker.append("path").attr("d", "M0,18 Q18,0 36,18 Q18,36 0,18").attr("stroke-width", `${strokeWidth}`);
  const endMarker = elem.append("defs").append("marker").attr("id", id + "_" + type + "-zeroOrMoreEnd").attr("class", "marker zeroOrMore " + type).attr("refX", 39).attr("refY", 18).attr("markerWidth", 57).attr("markerHeight", 36).attr("orient", "auto").attr("markerUnits", "userSpaceOnUse");
  endMarker.append("circle").attr("fill", mainBkg ?? "white").attr("cx", 11).attr("cy", 18).attr("r", 6).attr("stroke-width", `${strokeWidth}`);
  endMarker.append("path").attr("d", "M21,18 Q39,0 57,18 Q39,36 21,18").attr("stroke-width", `${strokeWidth}`);
}, "zero_or_more_neo");
var requirement_arrow = /* @__PURE__ */ __name((elem, type, id) => {
  elem.append("defs").append("marker").attr("id", id + "_" + type + "-requirement_arrowEnd").attr("refX", 20).attr("refY", 10).attr("markerWidth", 20).attr("markerHeight", 20).attr("orient", "auto").append("path").attr(
    "d",
    `M0,0
      L20,10
      M20,10
      L0,20`
  );
}, "requirement_arrow");
var requirement_arrow_neo = /* @__PURE__ */ __name((elem, type, id) => {
  const config = getConfig();
  const { themeVariables } = config;
  const { strokeWidth } = themeVariables;
  elem.append("defs").append("marker").attr("id", id + "_" + type + "-requirement_arrowEnd").attr("refX", 20).attr("refY", 10).attr("markerWidth", 20).attr("markerHeight", 20).attr("orient", "auto").attr("markerUnits", "userSpaceOnUse").attr("stroke-width", `${strokeWidth}`).attr("viewBox", "0 0 25 20").append("path").attr(
    "d",
    `M0,0
      L20,10
      M20,10
      L0,20`
  ).attr("stroke-linejoin", "miter");
}, "requirement_arrow_neo");
var requirement_contains = /* @__PURE__ */ __name((elem, type, id) => {
  const containsNode = elem.append("defs").append("marker").attr("id", id + "_" + type + "-requirement_containsStart").attr("refX", 0).attr("refY", 10).attr("markerWidth", 20).attr("markerHeight", 20).attr("orient", "auto").append("g");
  containsNode.append("circle").attr("cx", 10).attr("cy", 10).attr("r", 9).attr("fill", "none");
  containsNode.append("line").attr("x1", 1).attr("x2", 19).attr("y1", 10).attr("y2", 10);
  containsNode.append("line").attr("y1", 1).attr("y2", 19).attr("x1", 10).attr("x2", 10);
}, "requirement_contains");
var requirement_contains_neo = /* @__PURE__ */ __name((elem, type, id) => {
  const config = getConfig();
  const { themeVariables } = config;
  const { strokeWidth } = themeVariables;
  const containsNode = elem.append("defs").append("marker").attr("id", id + "_" + type + "-requirement_containsStart").attr("refX", 0).attr("refY", 10).attr("markerWidth", 20).attr("markerHeight", 20).attr("orient", "auto").attr("markerUnits", "userSpaceOnUse").append("g");
  containsNode.append("circle").attr("cx", 10).attr("cy", 10).attr("r", 9).attr("fill", "none");
  containsNode.append("line").attr("x1", 1).attr("x2", 19).attr("y1", 10).attr("y2", 10);
  containsNode.append("line").attr("y1", 1).attr("y2", 19).attr("x1", 10).attr("x2", 10);
  containsNode.selectAll("*").attr("stroke-width", `${strokeWidth}`);
}, "requirement_contains_neo");
var markers = {
  extension,
  composition,
  aggregation,
  dependency,
  lollipop,
  point,
  circle,
  cross,
  barb,
  barbNeo,
  only_one,
  zero_or_one,
  one_or_more,
  zero_or_more,
  only_one_neo,
  zero_or_one_neo,
  one_or_more_neo,
  zero_or_more_neo,
  requirement_arrow,
  requirement_contains,
  requirement_arrow_neo,
  requirement_contains_neo
};
var markers_default = insertMarkers;

export {
  edgeLabels,
  terminalLabels,
  clear,
  insertEdgeLabel,
  positionEdgeLabel,
  insertEdge,
  markers_default
};
