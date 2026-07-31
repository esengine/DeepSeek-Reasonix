(function (global, factory) {
	typeof exports === 'object' && typeof module !== 'undefined' ? factory(exports) :
	typeof define === 'function' && define.amd ? define(['exports'], factory) :
	(global = global || self, factory(global.window = global.window || {}));
}(this, (function (exports) { 'use strict';

	/*!
	 * DrawSVGPlugin 3.15.0
	 * https://gsap.com
	 *
	 * @license Copyright 2008-2026, GreenSock. All rights reserved.
	 * Subject to the terms at https://gsap.com/standard-license
	 * @author: Jack Doyle, jack@greensock.com
	*/
	var gsap,
	    _toArray,
	    _win,
	    _isEdge,
	    _coreInitted,
	    _warned,
	    _getStyleSaver,
	    _reverting,
	    _windowExists = function _windowExists() {
	  return typeof window !== "undefined";
	},
	    _getGSAP = function _getGSAP() {
	  return gsap || _windowExists() && (gsap = window.gsap) && gsap.registerPlugin && gsap;
	},
	    _numExp = /[-+=\.]*\d+[\.e\-\+]*\d*[e\-\+]*\d*/gi,
	    _types = {
	  rect: ["width", "height"],
	  circle: ["r", "r"],
	  ellipse: ["rx", "ry"],
	  line: ["x2", "y2"]
	},
	    _round = function _round(value) {
	  return Math.round(value * 10000) / 10000;
	},
	    _parseNum = function _parseNum(value) {
	  return parseFloat(value) || 0;
	},
	    _parseSingleVal = function _parseSingleVal(value, length) {
	  var num = _parseNum(value);

	  return ~value.indexOf("%") ? num / 100 * length : num;
	},
	    _getAttributeAsNumber = function _getAttributeAsNumber(target, attr) {
	  return _parseNum(target.getAttribute(attr));
	},
	    _sqrt = Math.sqrt,
	    _getDistance = function _getDistance(x1, y1, x2, y2, scaleX, scaleY) {
	  return _sqrt(Math.pow((_parseNum(x2) - _parseNum(x1)) * scaleX, 2) + Math.pow((_parseNum(y2) - _parseNum(y1)) * scaleY, 2));
	},
	    _warn = function _warn(message) {
	  return console.warn(message);
	},
	    _hasNonScalingStroke = function _hasNonScalingStroke(target) {
	  return target.getAttribute("vector-effect") === "non-scaling-stroke";
	},
	    _bonusValidated = 1,
	    _parse = function _parse(value, length, defaultStart) {
	  var i = value.indexOf(" "),
	      s,
	      e;

	  if (i < 0) {
	    s = defaultStart !== undefined ? defaultStart + "" : value;
	    e = value;
	  } else {
	    s = value.substr(0, i);
	    e = value.substr(i + 1);
	  }

	  s = _parseSingleVal(s, length);
	  e = _parseSingleVal(e, length);
	  return s > e ? [e, s] : [s, e];
	},
	    _getLength = function _getLength(target) {
	  target = _toArray(target)[0];

	  if (!target) {
	    return 0;
	  }

	  var type = target.tagName.toLowerCase(),
	      style = target.style,
	      scaleX = 1,
	      scaleY = 1,
	      length,
	      bbox,
	      points,
	      prevPoint,
	      i,
	      rx,
	      ry;

	  if (_hasNonScalingStroke(target)) {
	    scaleY = target.getScreenCTM();
	    scaleX = _sqrt(scaleY.a * scaleY.a + scaleY.b * scaleY.b);
	    scaleY = _sqrt(scaleY.d * scaleY.d + scaleY.c * scaleY.c);
	  }

	  try {
	    bbox = target.getBBox();
	  } catch (e) {
	    _warn("Some browsers won't measure invisible elements (like display:none or masks inside defs).");
	  }

	  var _ref = bbox || {
	    x: 0,
	    y: 0,
	    width: 0,
	    height: 0
	  },
	      x = _ref.x,
	      y = _ref.y,
	      width = _ref.width,
	      height = _ref.height;

	  if ((!bbox || !width && !height) && _types[type]) {
	    width = _getAttributeAsNumber(target, _types[type][0]);
	    height = _getAttributeAsNumber(target, _types[type][1]);

	    if (type !== "rect" && type !== "line") {
	      width *= 2;
	      height *= 2;
	    }

	    if (type === "line") {
	      x = _getAttributeAsNumber(target, "x1");
	      y = _getAttributeAsNumber(target, "y1");
	      width = Math.abs(width - x);
	      height = Math.abs(height - y);
	    }
	  }

	  if (type === "path") {
	    prevPoint = style.strokeDasharray;
	    style.strokeDasharray = "none";
	    length = target.getTotalLength() || 0;
	    _round(scaleX) !== _round(scaleY) && !_warned && (_warned = 1) && _warn("Warning: <path> length cannot be measured when vector-effect is non-scaling-stroke and the element isn't proportionally scaled.");
	    length *= (scaleX + scaleY) / 2;
	    style.strokeDasharray = prevPoint;
	  } else if (type === "rect") {
	    length = width * 2 * scaleX + height * 2 * scaleY;
	  } else if (type === "line") {
	    length = _getDistance(x, y, x + width, y + height, scaleX, scaleY);
	  } else if (type === "polyline" || type === "polygon") {
	    points = target.getAttribute("points").match(_numExp) || [];
	    type === "polygon" && points.push(points[0], points[1]);
	    length = 0;

	    for (i = 2; i < points.length; i += 2) {
	      length += _getDistance(points[i - 2], points[i - 1], points[i], points[i + 1], scaleX, scaleY) || 0;
	    }
	  } else if (type === "circle" || type === "ellipse") {
	    rx = width / 2 * scaleX;
	    ry = height / 2 * scaleY;
	    length = Math.PI * (3 * (rx + ry) - _sqrt((3 * rx + ry) * (rx + 3 * ry)));
	  }

	  return length || 0;
	},
	    _getPosition = function _getPosition(target, length) {
	  target = _toArray(target)[0];

	  if (!target) {
	    return [0, 0];
	  }

	  length || (length = _getLength(target) + 1);

	  var cs = _win.getComputedStyle(target),
	      dash = cs.strokeDasharray || "",
	      offset = _parseNum(cs.strokeDashoffset),
	      i = dash.indexOf(",");

	  i < 0 && (i = dash.indexOf(" "));
	  dash = i < 0 ? length : _parseNum(dash.substr(0, i));
	  dash > length && (dash = length);
	  return [-offset || 0, dash - offset || 0];
	},
	    _initCore = function _initCore() {
	  if (_windowExists()) {
	    _win = window;
	    _coreInitted = gsap = _getGSAP();
	    _toArray = gsap.utils.toArray;
	    _getStyleSaver = gsap.core.getStyleSaver;

	    _reverting = gsap.core.reverting || function () {};

	    _isEdge = ((_win.navigator || {}).userAgent || "").indexOf("Edge") !== -1;
	  }
	};

	var DrawSVGPlugin = {
	  version: "3.15.0",
	  name: "drawSVG",
	  register: function register(core) {
	    gsap = core;

	    _initCore();
	  },
	  init: function init(target, value, tween, index, targets) {
	    if (!target.getBBox) {
	      return false;
	    }

	    _coreInitted || _initCore();

	    var length = _getLength(target),
	        start,
	        end,
	        cs;

	    this.styles = _getStyleSaver && _getStyleSaver(target, "strokeDashoffset,strokeDasharray,strokeMiterlimit");
	    this.tween = tween;
	    this._style = target.style;
	    this._target = target;

	    if (value + "" === "true") {
	      value = "0 100%";
	    } else if (!value) {
	      value = "0 0";
	    } else if ((value + "").indexOf(" ") === -1) {
	      value = "0 " + value;
	    }

	    start = _getPosition(target, length);
	    end = _parse(value, length, start[0]);
	    this._length = _round(length);
	    this._dash = _round(start[1] - start[0]);
	    this._offset = _round(-start[0]);
	    this._dashPT = this.add(this, "_dash", this._dash, _round(end[1] - end[0]), 0, 0, 0, 0, 0, 1);
	    this._offsetPT = this.add(this, "_offset", this._offset, _round(-end[0]), 0, 0, 0, 0, 0, 1);

	    if (_isEdge) {
	      cs = _win.getComputedStyle(target);

	      if (cs.strokeLinecap !== cs.strokeLinejoin) {
	        end = _parseNum(cs.strokeMiterlimit);
	        this.add(target.style, "strokeMiterlimit", end, end + 0.01);
	      }
	    }

	    this._live = _hasNonScalingStroke(target) || ~(value + "").indexOf("live");
	    this._nowrap = ~(value + "").indexOf("nowrap");

	    this._props.push("drawSVG");

	    return _bonusValidated;
	  },
	  render: function render(ratio, data) {
	    if (data.tween._time || !_reverting()) {
	      var pt = data._pt,
	          style = data._style,
	          length,
	          lengthRatio,
	          dash,
	          offset;

	      if (pt) {
	        if (data._live) {
	          length = _getLength(data._target);

	          if (length !== data._length) {
	            lengthRatio = length / data._length;
	            data._length = length;

	            if (data._offsetPT) {
	              data._offsetPT.s *= lengthRatio;
	              data._offsetPT.c *= lengthRatio;
	            }

	            if (data._dashPT) {
	              data._dashPT.s *= lengthRatio;
	              data._dashPT.c *= lengthRatio;
	            } else {
	              data._dash *= lengthRatio;
	            }
	          }
	        }

	        while (pt) {
	          pt.r(ratio, pt.d);
	          pt = pt._next;
	        }

	        dash = data._dash || ratio && ratio !== 1 && 0.0001 || 0;
	        length = data._length - dash + 0.1;
	        offset = data._offset;
	        dash && offset && dash + Math.abs(offset % data._length) > data._length - 0.05 && (offset += offset < 0 ? 0.005 : -0.005) && (length += 0.005);
	        style.strokeDashoffset = dash ? offset : offset + 0.001;
	        style.strokeDasharray = length < 0.1 ? "none" : dash ? dash + "px," + (data._nowrap ? 999999 : length) + "px" : "0px, 999999px";
	      }
	    } else {
	      data.styles.revert();
	    }
	  },
	  getLength: _getLength,
	  getPosition: _getPosition
	};
	_getGSAP() && gsap.registerPlugin(DrawSVGPlugin);

	exports.DrawSVGPlugin = DrawSVGPlugin;
	exports.default = DrawSVGPlugin;

	Object.defineProperty(exports, '__esModule', { value: true });

})));
