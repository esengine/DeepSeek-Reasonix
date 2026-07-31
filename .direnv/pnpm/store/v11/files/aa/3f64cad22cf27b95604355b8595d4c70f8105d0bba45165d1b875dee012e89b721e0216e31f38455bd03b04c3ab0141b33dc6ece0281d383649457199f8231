/*!
 * MotionPathPlugin 3.15.0
 * https://gsap.com
 *
 * @license Copyright 2008-2026, GreenSock. All rights reserved.
 * Subject to the terms at https://gsap.com/standard-license
 * @author: Jack Doyle, jack@greensock.com
*/

/* eslint-disable */
import { getRawPath, cacheRawPathMeasurements, getPositionOnPath, pointsToSegment, flatPointsToSegment, sliceRawPath, stringToRawPath, rawPathToString, transformRawPath, convertToPath as _convertToPath } from "./utils/paths.js";
import { getGlobalMatrix } from "./utils/matrix.js";

var _xProps = "x,translateX,left,marginLeft,xPercent".split(","),
    _yProps = "y,translateY,top,marginTop,yPercent".split(","),
    _DEG2RAD = Math.PI / 180,
    gsap,
    PropTween,
    _getUnit,
    _toArray,
    _getStyleSaver,
    _reverting,
    _getGSAP = function _getGSAP() {
  return gsap || typeof window !== "undefined" && (gsap = window.gsap) && gsap.registerPlugin && gsap;
},
    _populateSegmentFromArray = function _populateSegmentFromArray(segment, values, property, mode) {
  //mode: 0 = x but don't fill y yet, 1 = y, 2 = x and fill y with 0.
  var l = values.length,
      si = mode === 2 ? 0 : mode,
      i = 0,
      v;

  for (; i < l; i++) {
    segment[si] = v = parseFloat(values[i][property]);
    mode === 2 && (segment[si + 1] = 0);
    si += 2;
  }

  return segment;
},
    _getPropNum = function _getPropNum(target, prop, unit) {
  return parseFloat(target._gsap.get(target, prop, unit || "px")) || 0;
},
    _relativize = function _relativize(segment) {
  var x = segment[0],
      y = segment[1],
      i;

  for (i = 2; i < segment.length; i += 2) {
    x = segment[i] += x;
    y = segment[i + 1] += y;
  }
},
    // feed in an array of quadratic bezier points like [{x: 0, y: 0}, ...] and it'll convert it to cubic bezier
// _quadToCubic = points => {
// 	let cubic = [],
// 		l = points.length - 1,
// 		i = 1,
// 		a, b, c;
// 	for (; i < l; i+=2) {
// 		a = points[i-1];
// 		b = points[i];
// 		c = points[i+1];
// 		cubic.push(a, {x: (2 * b.x + a.x) / 3, y: (2 * b.y + a.y) / 3}, {x: (2 * b.x + c.x) / 3, y: (2 * b.y + c.y) / 3});
// 	}
// 	cubic.push(points[l]);
// 	return cubic;
// },
_segmentToRawPath = function _segmentToRawPath(plugin, segment, target, x, y, slicer, vars, unitX, unitY) {
  if (vars.type === "cubic") {
    segment = [segment];
  } else {
    vars.fromCurrent !== false && segment.unshift(_getPropNum(target, x, unitX), y ? _getPropNum(target, y, unitY) : 0);
    vars.relative && _relativize(segment);
    var pointFunc = y ? pointsToSegment : flatPointsToSegment;
    segment = [pointFunc(segment, vars.curviness)];
  }

  segment = slicer(_align(segment, target, vars));

  _addDimensionalPropTween(plugin, target, x, segment, "x", unitX);

  y && _addDimensionalPropTween(plugin, target, y, segment, "y", unitY);
  return cacheRawPathMeasurements(segment, vars.resolution || (vars.curviness === 0 ? 20 : 12)); //when curviness is 0, it creates control points right on top of the anchors which makes it more sensitive to resolution, thus we change the default accordingly.
},
    _emptyFunc = function _emptyFunc(v) {
  return v;
},
    _numExp = /[-+\.]*\d+\.?(?:e-|e\+)?\d*/g,
    _originToPoint = function _originToPoint(element, origin, parentMatrix) {
  // origin is an array of normalized values (0-1) in relation to the width/height, so [0.5, 0.5] would be the center. It can also be "auto" in which case it will be the top left unless it's a <path>, when it will start at the beginning of the path itself.
  var m = getGlobalMatrix(element),
      x = 0,
      y = 0,
      svg;

  if ((element.tagName + "").toLowerCase() === "svg") {
    svg = element.viewBox.baseVal;
    svg.width || (svg = {
      width: +element.getAttribute("width"),
      height: +element.getAttribute("height")
    });
  } else {
    svg = origin && element.getBBox && element.getBBox();
  }

  if (origin && origin !== "auto") {
    x = origin.push ? origin[0] * (svg ? svg.width : element.offsetWidth || 0) : origin.x;
    y = origin.push ? origin[1] * (svg ? svg.height : element.offsetHeight || 0) : origin.y;
  }

  return parentMatrix.apply(x || y ? m.apply({
    x: x,
    y: y
  }) : {
    x: m.e,
    y: m.f
  });
},
    _getAlignMatrix = function _getAlignMatrix(fromElement, toElement, fromOrigin, toOrigin) {
  var parentMatrix = getGlobalMatrix(fromElement.parentNode, true, true),
      m = parentMatrix.clone().multiply(getGlobalMatrix(toElement)),
      fromPoint = _originToPoint(fromElement, fromOrigin, parentMatrix),
      _originToPoint2 = _originToPoint(toElement, toOrigin, parentMatrix),
      x = _originToPoint2.x,
      y = _originToPoint2.y,
      p;

  m.e = m.f = 0;

  if (toOrigin === "auto" && toElement.getTotalLength && toElement.tagName.toLowerCase() === "path") {
    p = toElement.getAttribute("d").match(_numExp) || [];
    p = m.apply({
      x: +p[0],
      y: +p[1]
    });
    x += p.x;
    y += p.y;
  } //if (p || (toElement.getBBox && fromElement.getBBox && toElement.ownerSVGElement === fromElement.ownerSVGElement)) {


  if (p) {
    p = m.apply(toElement.getBBox());
    x -= p.x;
    y -= p.y;
  }

  m.e = x - fromPoint.x;
  m.f = y - fromPoint.y;
  return m;
},
    _align = function _align(rawPath, target, _ref) {
  var align = _ref.align,
      matrix = _ref.matrix,
      offsetX = _ref.offsetX,
      offsetY = _ref.offsetY,
      alignOrigin = _ref.alignOrigin;

  var x = rawPath[0][0],
      y = rawPath[0][1],
      curX = _getPropNum(target, "x"),
      curY = _getPropNum(target, "y"),
      alignTarget,
      m,
      p;

  if (!rawPath || !rawPath.length) {
    return getRawPath("M0,0L0,0");
  }

  if (align) {
    if (align === "self" || (alignTarget = _toArray(align)[0] || target) === target) {
      transformRawPath(rawPath, 1, 0, 0, 1, curX - x, curY - y);
    } else {
      if (alignOrigin && alignOrigin[2] !== false) {
        gsap.set(target, {
          transformOrigin: alignOrigin[0] * 100 + "% " + alignOrigin[1] * 100 + "%"
        });
      } else {
        alignOrigin = [_getPropNum(target, "xPercent") / -100, _getPropNum(target, "yPercent") / -100];
      }

      m = _getAlignMatrix(target, alignTarget, alignOrigin, "auto");
      p = m.apply({
        x: x,
        y: y
      });
      transformRawPath(rawPath, m.a, m.b, m.c, m.d, curX + m.e - (p.x - m.e), curY + m.f - (p.y - m.f));
    }
  }

  if (matrix) {
    transformRawPath(rawPath, matrix.a, matrix.b, matrix.c, matrix.d, matrix.e, matrix.f);
  } else if (offsetX || offsetY) {
    transformRawPath(rawPath, 1, 0, 0, 1, offsetX || 0, offsetY || 0);
  }

  return rawPath;
},
    _addDimensionalPropTween = function _addDimensionalPropTween(plugin, target, property, rawPath, pathProperty, forceUnit) {
  var cache = target._gsap,
      harness = cache.harness,
      alias = harness && harness.aliases && harness.aliases[property],
      prop = alias && alias.indexOf(",") < 0 ? alias : property,
      pt = plugin._pt = new PropTween(plugin._pt, target, prop, 0, 0, _emptyFunc, 0, cache.set(target, prop, plugin));
  pt.u = _getUnit(cache.get(target, prop, forceUnit)) || 0;
  pt.path = rawPath;
  pt.pp = pathProperty;

  plugin._props.push(prop);
},
    _sliceModifier = function _sliceModifier(start, end) {
  return function (rawPath) {
    return start || end !== 1 ? sliceRawPath(rawPath, start, end) : rawPath;
  };
};

export var MotionPathPlugin = {
  version: "3.15.0",
  name: "motionPath",
  register: function register(core, Plugin, propTween) {
    gsap = core;
    _getUnit = gsap.utils.getUnit;
    _toArray = gsap.utils.toArray;
    _getStyleSaver = gsap.core.getStyleSaver;

    _reverting = gsap.core.reverting || function () {};

    PropTween = propTween;
  },
  init: function init(target, vars, tween) {
    if (!gsap) {
      console.warn("Please gsap.registerPlugin(MotionPathPlugin)");
      return false;
    }

    if (!(typeof vars === "object" && !vars.style) || !vars.path) {
      vars = {
        path: vars
      };
    }

    var rawPaths = [],
        _vars = vars,
        path = _vars.path,
        autoRotate = _vars.autoRotate,
        unitX = _vars.unitX,
        unitY = _vars.unitY,
        x = _vars.x,
        y = _vars.y,
        firstObj = path[0],
        slicer = _sliceModifier(vars.start, "end" in vars ? vars.end : 1),
        rawPath,
        p;

    this.rawPaths = rawPaths;
    this.target = target;
    this.tween = tween;
    this.styles = _getStyleSaver && _getStyleSaver(target, "transform");

    if (this.rotate = autoRotate || autoRotate === 0) {
      //get the rotational data FIRST so that the setTransform() method is called in the correct order in the render() loop - rotation gets set last.
      this.rOffset = parseFloat(autoRotate) || 0;
      this.radians = !!vars.useRadians;
      this.rProp = vars.rotation || "rotation"; // rotation property

      this.rSet = target._gsap.set(target, this.rProp, this); // rotation setter

      this.ru = _getUnit(target._gsap.get(target, this.rProp)) || 0; // rotation units
    }

    if (Array.isArray(path) && !("closed" in path) && typeof firstObj !== "number") {
      for (p in firstObj) {
        if (!x && ~_xProps.indexOf(p)) {
          x = p;
        } else if (!y && ~_yProps.indexOf(p)) {
          y = p;
        }
      }

      if (x && y) {
        //correlated values
        rawPaths.push(_segmentToRawPath(this, _populateSegmentFromArray(_populateSegmentFromArray([], path, x, 0), path, y, 1), target, x, y, slicer, vars, unitX || _getUnit(path[0][x]), unitY || _getUnit(path[0][y])));
      } else {
        x = y = 0;
      }

      for (p in firstObj) {
        p !== x && p !== y && rawPaths.push(_segmentToRawPath(this, _populateSegmentFromArray([], path, p, 2), target, p, 0, slicer, vars, _getUnit(path[0][p])));
      }
    } else {
      rawPath = slicer(_align(getRawPath(vars.path), target, vars));
      cacheRawPathMeasurements(rawPath, vars.resolution);
      rawPaths.push(rawPath);

      _addDimensionalPropTween(this, target, vars.x || "x", rawPath, "x", vars.unitX || "px");

      _addDimensionalPropTween(this, target, vars.y || "y", rawPath, "y", vars.unitY || "px");
    }

    tween.vars.immediateRender && this.render(tween.progress(), this);
  },
  render: function render(ratio, data) {
    var rawPaths = data.rawPaths,
        i = rawPaths.length,
        pt = data._pt;

    if (data.tween._time || !_reverting()) {
      if (ratio > 1) {
        ratio = 1;
      } else if (ratio < 0) {
        ratio = 0;
      }

      while (i--) {
        getPositionOnPath(rawPaths[i], ratio, !i && data.rotate, rawPaths[i]);
      }

      while (pt) {
        pt.set(pt.t, pt.p, pt.path[pt.pp] + pt.u, pt.d, ratio);
        pt = pt._next;
      }

      data.rotate && data.rSet(data.target, data.rProp, rawPaths[0].angle * (data.radians ? _DEG2RAD : 1) + data.rOffset + data.ru, data, ratio);
    } else {
      data.styles.revert();
    }
  },
  getLength: function getLength(path) {
    return cacheRawPathMeasurements(getRawPath(path)).totalLength;
  },
  sliceRawPath: sliceRawPath,
  getRawPath: getRawPath,
  pointsToSegment: pointsToSegment,
  stringToRawPath: stringToRawPath,
  rawPathToString: rawPathToString,
  transformRawPath: transformRawPath,
  getGlobalMatrix: getGlobalMatrix,
  getPositionOnPath: getPositionOnPath,
  cacheRawPathMeasurements: cacheRawPathMeasurements,
  convertToPath: function convertToPath(targets, swap) {
    return _toArray(targets).map(function (target) {
      return _convertToPath(target, swap !== false);
    });
  },
  convertCoordinates: function convertCoordinates(fromElement, toElement, point) {
    var m = getGlobalMatrix(toElement, true, true).multiply(getGlobalMatrix(fromElement));
    return point ? m.apply(point) : m;
  },
  getAlignMatrix: _getAlignMatrix,
  getRelativePosition: function getRelativePosition(fromElement, toElement, fromOrigin, toOrigin) {
    var m = _getAlignMatrix(fromElement, toElement, fromOrigin, toOrigin);

    return {
      x: m.e,
      y: m.f
    };
  },
  arrayToRawPath: function arrayToRawPath(value, vars) {
    vars = vars || {};

    var segment = _populateSegmentFromArray(_populateSegmentFromArray([], value, vars.x || "x", 0), value, vars.y || "y", 1);

    vars.relative && _relativize(segment);
    return [vars.type === "cubic" ? segment : pointsToSegment(segment, vars.curviness)];
  }
};
_getGSAP() && gsap.registerPlugin(MotionPathPlugin);
export { MotionPathPlugin as default };