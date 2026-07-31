(function (global, factory) {
	typeof exports === 'object' && typeof module !== 'undefined' ? factory(exports) :
	typeof define === 'function' && define.amd ? define(['exports'], factory) :
	(global = global || self, factory(global.window = global.window || {}));
}(this, (function (exports) { 'use strict';

	var _svgPathExp = /[achlmqstvz]|(-?\d*\.?\d*(?:e[\-+]?\d+)?)[0-9]/ig,
	    _numbersExp = /(?:(-)?\d*\.?\d*(?:e[\-+]?\d+)?)[0-9]/ig,
	    _scientific = /[\+\-]?\d*\.?\d+e[\+\-]?\d+/ig,
	    _selectorExp = /(^[#\.][a-z]|[a-y][a-z])/i,
	    _DEG2RAD = Math.PI / 180,
	    _RAD2DEG = 180 / Math.PI,
	    _sin = Math.sin,
	    _cos = Math.cos,
	    _abs = Math.abs,
	    _sqrt = Math.sqrt,
	    _atan2 = Math.atan2,
	    _largeNum = 1e8,
	    _isString = function _isString(value) {
	  return typeof value === "string";
	},
	    _isNumber = function _isNumber(value) {
	  return typeof value === "number";
	},
	    _isUndefined = function _isUndefined(value) {
	  return typeof value === "undefined";
	},
	    _temp = {},
	    _temp2 = {},
	    _roundingNum = 1e5,
	    _wrapProgress = function _wrapProgress(progress) {
	  return Math.round((progress + _largeNum) % 1 * _roundingNum) / _roundingNum || (progress < 0 ? 0 : 1);
	},
	    _round = function _round(value) {
	  return Math.round(value * _roundingNum) / _roundingNum || 0;
	},
	    _roundPrecise = function _roundPrecise(value) {
	  return Math.round(value * 1e10) / 1e10 || 0;
	},
	    _segmentIsClosed = function _segmentIsClosed(segment) {
	  return segment.closed = Math.abs(segment[0] - segment[segment.length - 2]) < 0.001 && Math.abs(segment[1] - segment[segment.length - 1]) < 0.001;
	},
	    _splitSegment = function _splitSegment(rawPath, segIndex, i, t) {
	  var segment = rawPath[segIndex],
	      shift = t === 1 ? 6 : subdivideSegment(segment, i, t);

	  if ((shift || !t) && shift + i + 2 < segment.length) {
	    rawPath.splice(segIndex, 0, segment.slice(0, i + shift + 2));
	    segment.splice(0, i + shift);
	    return 1;
	  }
	},
	    _getSampleIndex = function _getSampleIndex(samples, length, progress) {
	  var l = samples.length,
	      i = ~~(progress * l);

	  if (samples[i] > length) {
	    while (--i && samples[i] > length) {}

	    i < 0 && (i = 0);
	  } else {
	    while (samples[++i] < length && i < l) {}
	  }

	  return i < l ? i : l - 1;
	},
	    _reverseRawPath = function _reverseRawPath(rawPath, skipOuter) {
	  var i = rawPath.length;
	  skipOuter || rawPath.reverse();

	  while (i--) {
	    rawPath[i].reversed || reverseSegment(rawPath[i]);
	  }
	},
	    _copyMetaData = function _copyMetaData(source, copy) {
	  copy.totalLength = source.totalLength;

	  if (source.samples) {
	    copy.samples = source.samples.slice(0);
	    copy.lookup = source.lookup.slice(0);
	    copy.minLength = source.minLength;
	    copy.resolution = source.resolution;
	  } else if (source.totalPoints) {
	    copy.totalPoints = source.totalPoints;
	  }

	  return copy;
	},
	    _appendOrMerge = function _appendOrMerge(rawPath, segment) {
	  var index = rawPath.length,
	      prevSeg = rawPath[index - 1] || [],
	      l = prevSeg.length;

	  if (index && segment[0] === prevSeg[l - 2] && segment[1] === prevSeg[l - 1]) {
	    segment = prevSeg.concat(segment.slice(2));
	    index--;
	  }

	  rawPath[index] = segment;
	};

	function getRawPath(value) {
	  value = _isString(value) && _selectorExp.test(value) ? document.querySelector(value) || value : value;
	  var e = value.getAttribute ? value : 0,
	      rawPath;

	  if (e && (value = value.getAttribute("d"))) {
	    if (!e._gsPath) {
	      e._gsPath = {};
	    }

	    rawPath = e._gsPath[value];
	    return rawPath && !rawPath._dirty ? rawPath : e._gsPath[value] = stringToRawPath(value);
	  }

	  return !value ? console.warn("Expecting a <path> element or an SVG path data string") : _isString(value) ? stringToRawPath(value) : _isNumber(value[0]) ? [value] : value;
	}
	function copyRawPath(rawPath) {
	  var a = [],
	      i = 0;

	  for (; i < rawPath.length; i++) {
	    a[i] = _copyMetaData(rawPath[i], rawPath[i].slice(0));
	  }

	  return _copyMetaData(rawPath, a);
	}
	function reverseSegment(segment) {
	  var i = 0,
	      y;
	  segment.reverse();

	  for (; i < segment.length; i += 2) {
	    y = segment[i];
	    segment[i] = segment[i + 1];
	    segment[i + 1] = y;
	  }

	  segment.reversed = !segment.reversed;
	}

	var _createPath = function _createPath(e, ignore) {
	  var path = document.createElementNS("http://www.w3.org/2000/svg", "path"),
	      attr = [].slice.call(e.attributes),
	      i = attr.length,
	      name;
	  ignore = "," + ignore + ",";

	  while (--i > -1) {
	    name = attr[i].nodeName.toLowerCase();

	    if (ignore.indexOf("," + name + ",") < 0) {
	      path.setAttributeNS(null, name, attr[i].nodeValue);
	    }
	  }

	  return path;
	},
	    _typeAttrs = {
	  rect: "rx,ry,x,y,width,height",
	  circle: "r,cx,cy",
	  ellipse: "rx,ry,cx,cy",
	  line: "x1,x2,y1,y2"
	},
	    _attrToObj = function _attrToObj(e, attrs) {
	  var props = attrs ? attrs.split(",") : [],
	      obj = {},
	      i = props.length;

	  while (--i > -1) {
	    obj[props[i]] = +e.getAttribute(props[i]) || 0;
	  }

	  return obj;
	};

	function convertToPath(element, swap) {
	  var type = element.tagName.toLowerCase(),
	      circ = 0.552284749831,
	      data,
	      x,
	      y,
	      r,
	      ry,
	      path,
	      rcirc,
	      rycirc,
	      points,
	      w,
	      h,
	      x2,
	      x3,
	      x4,
	      x5,
	      x6,
	      y2,
	      y3,
	      y4,
	      y5,
	      y6,
	      attr;

	  if (type === "path" || !element.getBBox) {
	    return element;
	  }

	  path = _createPath(element, "x,y,width,height,cx,cy,rx,ry,r,x1,x2,y1,y2,points");
	  attr = _attrToObj(element, _typeAttrs[type]);

	  if (type === "rect") {
	    r = attr.rx;
	    ry = attr.ry || r;
	    x = attr.x;
	    y = attr.y;
	    w = attr.width - r * 2;
	    h = attr.height - ry * 2;

	    if (r || ry) {
	      x2 = x + r * (1 - circ);
	      x3 = x + r;
	      x4 = x3 + w;
	      x5 = x4 + r * circ;
	      x6 = x4 + r;
	      y2 = y + ry * (1 - circ);
	      y3 = y + ry;
	      y4 = y3 + h;
	      y5 = y4 + ry * circ;
	      y6 = y4 + ry;
	      data = "M" + x6 + "," + y3 + " V" + y4 + " C" + [x6, y5, x5, y6, x4, y6, x4 - (x4 - x3) / 3, y6, x3 + (x4 - x3) / 3, y6, x3, y6, x2, y6, x, y5, x, y4, x, y4 - (y4 - y3) / 3, x, y3 + (y4 - y3) / 3, x, y3, x, y2, x2, y, x3, y, x3 + (x4 - x3) / 3, y, x4 - (x4 - x3) / 3, y, x4, y, x5, y, x6, y2, x6, y3].join(",") + "z";
	    } else {
	      data = "M" + (x + w) + "," + y + " v" + h + " h" + -w + " v" + -h + " h" + w + "z";
	    }
	  } else if (type === "circle" || type === "ellipse") {
	    if (type === "circle") {
	      r = ry = attr.r;
	      rycirc = r * circ;
	    } else {
	      r = attr.rx;
	      ry = attr.ry;
	      rycirc = ry * circ;
	    }

	    x = attr.cx;
	    y = attr.cy;
	    rcirc = r * circ;
	    data = "M" + (x + r) + "," + y + " C" + [x + r, y + rycirc, x + rcirc, y + ry, x, y + ry, x - rcirc, y + ry, x - r, y + rycirc, x - r, y, x - r, y - rycirc, x - rcirc, y - ry, x, y - ry, x + rcirc, y - ry, x + r, y - rycirc, x + r, y].join(",") + "z";
	  } else if (type === "line") {
	    data = "M" + attr.x1 + "," + attr.y1 + " L" + attr.x2 + "," + attr.y2;
	  } else if (type === "polyline" || type === "polygon") {
	    points = (element.getAttribute("points") + "").match(_numbersExp) || [];
	    x = points.shift();
	    y = points.shift();
	    data = "M" + x + "," + y + " L" + points.join(",");

	    if (type === "polygon") {
	      data += "," + x + "," + y + "z";
	    }
	  }

	  path.setAttribute("d", rawPathToString(path._gsRawPath = stringToRawPath(data)));

	  if (swap && element.parentNode) {
	    element.parentNode.insertBefore(path, element);
	    element.parentNode.removeChild(element);
	  }

	  return path;
	}

	function getRotationAtBezierT(segment, i, t) {
	  var a = segment[i],
	      b = segment[i + 2],
	      c = segment[i + 4],
	      x;
	  a += (b - a) * t;
	  b += (c - b) * t;
	  a += (b - a) * t;
	  x = b + (c + (segment[i + 6] - c) * t - b) * t - a;
	  a = segment[i + 1];
	  b = segment[i + 3];
	  c = segment[i + 5];
	  a += (b - a) * t;
	  b += (c - b) * t;
	  a += (b - a) * t;
	  return _round(_atan2(b + (c + (segment[i + 7] - c) * t - b) * t - a, x) * _RAD2DEG);
	}

	function sliceRawPath(rawPath, start, end) {
	  end = _isUndefined(end) ? 1 : _roundPrecise(end) || 0;
	  start = _roundPrecise(start) || 0;
	  var loops = Math.max(0, ~~(_abs(end - start) - 1e-8)),
	      path = copyRawPath(rawPath);

	  if (start > end) {
	    start = 1 - start;
	    end = 1 - end;

	    _reverseRawPath(path);

	    path.totalLength = 0;
	  }

	  if (start < 0 || end < 0) {
	    var offset = Math.abs(~~Math.min(start, end)) + 1;
	    start += offset;
	    end += offset;
	  }

	  path.totalLength || cacheRawPathMeasurements(path);
	  var wrap = end > 1,
	      s = getProgressData(path, start, _temp, true),
	      e = getProgressData(path, end, _temp2),
	      eSeg = e.segment,
	      sSeg = s.segment,
	      eSegIndex = e.segIndex,
	      sSegIndex = s.segIndex,
	      ei = e.i,
	      si = s.i,
	      sameSegment = sSegIndex === eSegIndex,
	      sameBezier = ei === si && sameSegment,
	      wrapsBehind,
	      sShift,
	      eShift,
	      i,
	      copy,
	      totalSegments,
	      l,
	      j;

	  if (wrap || loops) {
	    wrapsBehind = eSegIndex < sSegIndex || sameSegment && ei < si || sameBezier && e.t < s.t;

	    if (_splitSegment(path, sSegIndex, si, s.t)) {
	      sSegIndex++;

	      if (!wrapsBehind) {
	        eSegIndex++;

	        if (sameBezier) {
	          e.t = (e.t - s.t) / (1 - s.t);
	          ei = 0;
	        } else if (sameSegment) {
	          ei -= si;
	        }
	      }
	    }

	    if (Math.abs(1 - (end - start)) < 1e-5) {
	      eSegIndex = sSegIndex - 1;
	    } else if (!e.t && eSegIndex) {
	      eSegIndex--;
	    } else if (_splitSegment(path, eSegIndex, ei, e.t) && wrapsBehind) {
	      sSegIndex++;
	    }

	    if (s.t === 1) {
	      sSegIndex = (sSegIndex + 1) % path.length;
	    }

	    copy = [];
	    totalSegments = path.length;
	    l = 1 + totalSegments * loops;
	    j = sSegIndex;
	    l += (totalSegments - sSegIndex + eSegIndex) % totalSegments;

	    for (i = 0; i < l; i++) {
	      _appendOrMerge(copy, path[j++ % totalSegments]);
	    }

	    path = copy;
	  } else {
	    eShift = e.t === 1 ? 6 : subdivideSegment(eSeg, ei, e.t);

	    if (start !== end) {
	      sShift = subdivideSegment(sSeg, si, sameBezier ? s.t / e.t : s.t);
	      sameSegment && (eShift += sShift);
	      eSeg.splice(ei + eShift + 2);
	      (sShift || si) && sSeg.splice(0, si + sShift);
	      i = path.length;

	      while (i--) {
	        (i < sSegIndex || i > eSegIndex) && path.splice(i, 1);
	      }
	    } else {
	      eSeg.angle = getRotationAtBezierT(eSeg, ei + eShift, 0);
	      ei += eShift;
	      s = eSeg[ei];
	      e = eSeg[ei + 1];
	      eSeg.length = eSeg.totalLength = 0;
	      eSeg.totalPoints = path.totalPoints = 8;
	      eSeg.push(s, e, s, e, s, e, s, e);
	    }
	  }

	  path.totalLength = 0;
	  return path;
	}

	function measureSegment(segment, startIndex, bezierQty) {
	  startIndex = startIndex || 0;

	  if (!segment.samples) {
	    segment.samples = [];
	    segment.lookup = [];
	  }

	  var resolution = ~~segment.resolution || 12,
	      inc = 1 / resolution,
	      endIndex = bezierQty ? startIndex + bezierQty * 6 + 1 : segment.length,
	      x1 = segment[startIndex],
	      y1 = segment[startIndex + 1],
	      samplesIndex = startIndex ? startIndex / 6 * resolution : 0,
	      samples = segment.samples,
	      lookup = segment.lookup,
	      min = (startIndex ? segment.minLength : _largeNum) || _largeNum,
	      prevLength = samples[samplesIndex + bezierQty * resolution - 1],
	      length = startIndex ? samples[samplesIndex - 1] : 0,
	      i,
	      j,
	      x4,
	      x3,
	      x2,
	      xd,
	      xd1,
	      y4,
	      y3,
	      y2,
	      yd,
	      yd1,
	      inv,
	      t,
	      lengthIndex,
	      l,
	      segLength;
	  samples.length = lookup.length = 0;

	  for (j = startIndex + 2; j < endIndex; j += 6) {
	    x4 = segment[j + 4] - x1;
	    x3 = segment[j + 2] - x1;
	    x2 = segment[j] - x1;
	    y4 = segment[j + 5] - y1;
	    y3 = segment[j + 3] - y1;
	    y2 = segment[j + 1] - y1;
	    xd = xd1 = yd = yd1 = 0;

	    if (_abs(x4) < .01 && _abs(y4) < .01 && _abs(x2) + _abs(y2) < .01) {
	      if (segment.length > 8) {
	        segment.splice(j, 6);
	        j -= 6;
	        endIndex -= 6;
	      }
	    } else {
	      for (i = 1; i <= resolution; i++) {
	        t = inc * i;
	        inv = 1 - t;
	        xd = xd1 - (xd1 = (t * t * x4 + 3 * inv * (t * x3 + inv * x2)) * t);
	        yd = yd1 - (yd1 = (t * t * y4 + 3 * inv * (t * y3 + inv * y2)) * t);
	        l = _sqrt(yd * yd + xd * xd);

	        if (l < min) {
	          min = l;
	        }

	        length += l;
	        samples[samplesIndex++] = length;
	      }
	    }

	    x1 += x4;
	    y1 += y4;
	  }

	  if (prevLength) {
	    prevLength -= length;

	    for (; samplesIndex < samples.length; samplesIndex++) {
	      samples[samplesIndex] += prevLength;
	    }
	  }

	  if (samples.length && min) {
	    segment.totalLength = segLength = samples[samples.length - 1] || 0;
	    segment.minLength = min;

	    if (segLength / min < 9999) {
	      l = lengthIndex = 0;

	      for (i = 0; i < segLength; i += min) {
	        lookup[l++] = samples[lengthIndex] < i ? ++lengthIndex : lengthIndex;
	      }
	    }
	  } else {
	    segment.totalLength = samples[0] = 0;
	  }

	  return startIndex ? length - samples[startIndex / 2 - 1] : length;
	}

	function cacheRawPathMeasurements(rawPath, resolution) {
	  var pathLength, points, i;

	  for (i = pathLength = points = 0; i < rawPath.length; i++) {
	    rawPath[i].resolution = ~~resolution || 12;
	    pathLength += measureSegment(rawPath[i]);
	    points += rawPath[i].length;
	  }

	  rawPath.totalPoints = points;
	  rawPath.totalLength = pathLength;
	  return rawPath;
	}
	function subdivideSegment(segment, i, t) {
	  if (t <= 0 || t >= 1) {
	    return 0;
	  }

	  var ax = segment[i],
	      ay = segment[i + 1],
	      cp1x = segment[i + 2],
	      cp1y = segment[i + 3],
	      cp2x = segment[i + 4],
	      cp2y = segment[i + 5],
	      bx = segment[i + 6],
	      by = segment[i + 7],
	      x1a = ax + (cp1x - ax) * t,
	      x2 = cp1x + (cp2x - cp1x) * t,
	      y1a = ay + (cp1y - ay) * t,
	      y2 = cp1y + (cp2y - cp1y) * t,
	      x1 = x1a + (x2 - x1a) * t,
	      y1 = y1a + (y2 - y1a) * t,
	      x2a = cp2x + (bx - cp2x) * t,
	      y2a = cp2y + (by - cp2y) * t;
	  x2 += (x2a - x2) * t;
	  y2 += (y2a - y2) * t;
	  segment.splice(i + 2, 4, _round(x1a), _round(y1a), _round(x1), _round(y1), _round(x1 + (x2 - x1) * t), _round(y1 + (y2 - y1) * t), _round(x2), _round(y2), _round(x2a), _round(y2a));
	  segment.samples && segment.samples.splice(i / 6 * segment.resolution | 0, 0, 0, 0, 0, 0, 0, 0);
	  return 6;
	}

	function getProgressData(rawPath, progress, decoratee, pushToNextIfAtEnd) {
	  decoratee = decoratee || {};
	  rawPath.totalLength || cacheRawPathMeasurements(rawPath);

	  if (progress < 0 || progress > 1) {
	    progress = _wrapProgress(progress);
	  }

	  var segIndex = 0,
	      segment = rawPath[0],
	      samples,
	      resolution,
	      length,
	      min,
	      max,
	      i,
	      t;

	  if (!progress) {
	    t = i = segIndex = 0;
	    segment = rawPath[0];
	  } else if (progress === 1) {
	    t = 1;
	    segIndex = rawPath.length - 1;
	    segment = rawPath[segIndex];
	    i = segment.length - 8;
	  } else {
	    if (rawPath.length > 1) {
	      length = rawPath.totalLength * progress;
	      max = i = 0;

	      while ((max += rawPath[i++].totalLength) < length) {
	        segIndex = i;
	      }

	      segment = rawPath[segIndex];
	      min = max - segment.totalLength;
	      progress = (length - min) / (max - min) || 0;
	    }

	    samples = segment.samples;
	    resolution = segment.resolution;
	    length = segment.totalLength * progress;
	    i = segment.lookup.length ? segment.lookup[~~(length / segment.minLength)] || 0 : _getSampleIndex(samples, length, progress);
	    min = i ? samples[i - 1] : 0;
	    max = samples[i];

	    if (max < length) {
	      min = max;
	      max = samples[++i];
	    }

	    t = 1 / resolution * ((length - min) / (max - min) + i % resolution);
	    i = ~~(i / resolution) * 6;

	    if (pushToNextIfAtEnd && t === 1) {
	      if (i + 6 < segment.length) {
	        i += 6;
	        t = 0;
	      } else if (segIndex + 1 < rawPath.length) {
	        i = t = 0;
	        segment = rawPath[++segIndex];
	      }
	    }
	  }

	  decoratee.t = t;
	  decoratee.i = i;
	  decoratee.path = rawPath;
	  decoratee.segment = segment;
	  decoratee.segIndex = segIndex;
	  return decoratee;
	}

	function getPositionOnPath(rawPath, progress, includeAngle, point) {
	  var segment = rawPath[0],
	      result = point || {},
	      samples,
	      resolution,
	      length,
	      min,
	      max,
	      i,
	      t,
	      a,
	      inv;

	  if (progress < 0 || progress > 1) {
	    progress = _wrapProgress(progress);
	  }

	  segment.lookup || cacheRawPathMeasurements(rawPath);

	  if (rawPath.length > 1) {
	    length = rawPath.totalLength * progress;
	    max = i = 0;

	    while ((max += rawPath[i++].totalLength) < length) {
	      segment = rawPath[i];
	    }

	    min = max - segment.totalLength;
	    progress = (length - min) / (max - min) || 0;
	  }

	  samples = segment.samples;
	  resolution = segment.resolution;
	  length = segment.totalLength * progress;
	  i = segment.lookup.length ? segment.lookup[progress < 1 ? ~~(length / segment.minLength) : segment.lookup.length - 1] || 0 : _getSampleIndex(samples, length, progress);
	  min = i ? samples[i - 1] : 0;
	  max = samples[i];

	  if (max < length) {
	    min = max;
	    max = samples[++i];
	  }

	  t = 1 / resolution * ((length - min) / (max - min) + i % resolution) || 0;
	  inv = 1 - t;
	  i = ~~(i / resolution) * 6;
	  a = segment[i];
	  result.x = _round((t * t * (segment[i + 6] - a) + 3 * inv * (t * (segment[i + 4] - a) + inv * (segment[i + 2] - a))) * t + a);
	  result.y = _round((t * t * (segment[i + 7] - (a = segment[i + 1])) + 3 * inv * (t * (segment[i + 5] - a) + inv * (segment[i + 3] - a))) * t + a);

	  if (includeAngle) {
	    result.angle = segment.totalLength ? getRotationAtBezierT(segment, i, t >= 1 ? 1 - 1e-9 : t ? t : 1e-9) : segment.angle || 0;
	  }

	  return result;
	}
	function transformRawPath(rawPath, a, b, c, d, tx, ty) {
	  var j = rawPath.length,
	      segment,
	      l,
	      i,
	      x,
	      y;

	  while (--j > -1) {
	    segment = rawPath[j];
	    l = segment.length;

	    for (i = 0; i < l; i += 2) {
	      x = segment[i];
	      y = segment[i + 1];
	      segment[i] = x * a + y * c + tx;
	      segment[i + 1] = x * b + y * d + ty;
	    }
	  }

	  rawPath._dirty = 1;
	  return rawPath;
	}

	function arcToSegment(lastX, lastY, rx, ry, angle, largeArcFlag, sweepFlag, x, y) {
	  if (lastX === x && lastY === y) {
	    return;
	  }

	  rx = _abs(rx);
	  ry = _abs(ry);

	  var angleRad = angle % 360 * _DEG2RAD,
	      cosAngle = _cos(angleRad),
	      sinAngle = _sin(angleRad),
	      PI = Math.PI,
	      TWOPI = PI * 2,
	      dx2 = (lastX - x) / 2,
	      dy2 = (lastY - y) / 2,
	      x1 = cosAngle * dx2 + sinAngle * dy2,
	      y1 = -sinAngle * dx2 + cosAngle * dy2,
	      x1_sq = x1 * x1,
	      y1_sq = y1 * y1,
	      radiiCheck = x1_sq / (rx * rx) + y1_sq / (ry * ry);

	  if (radiiCheck > 1) {
	    rx = _sqrt(radiiCheck) * rx;
	    ry = _sqrt(radiiCheck) * ry;
	  }

	  var rx_sq = rx * rx,
	      ry_sq = ry * ry,
	      sq = (rx_sq * ry_sq - rx_sq * y1_sq - ry_sq * x1_sq) / (rx_sq * y1_sq + ry_sq * x1_sq);

	  if (sq < 0) {
	    sq = 0;
	  }

	  var coef = (largeArcFlag === sweepFlag ? -1 : 1) * _sqrt(sq),
	      cx1 = coef * (rx * y1 / ry),
	      cy1 = coef * -(ry * x1 / rx),
	      sx2 = (lastX + x) / 2,
	      sy2 = (lastY + y) / 2,
	      cx = sx2 + (cosAngle * cx1 - sinAngle * cy1),
	      cy = sy2 + (sinAngle * cx1 + cosAngle * cy1),
	      ux = (x1 - cx1) / rx,
	      uy = (y1 - cy1) / ry,
	      vx = (-x1 - cx1) / rx,
	      vy = (-y1 - cy1) / ry,
	      temp = ux * ux + uy * uy,
	      angleStart = (uy < 0 ? -1 : 1) * Math.acos(ux / _sqrt(temp)),
	      angleExtent = (ux * vy - uy * vx < 0 ? -1 : 1) * Math.acos((ux * vx + uy * vy) / _sqrt(temp * (vx * vx + vy * vy)));

	  isNaN(angleExtent) && (angleExtent = PI);

	  if (!sweepFlag && angleExtent > 0) {
	    angleExtent -= TWOPI;
	  } else if (sweepFlag && angleExtent < 0) {
	    angleExtent += TWOPI;
	  }

	  angleStart %= TWOPI;
	  angleExtent %= TWOPI;

	  var segments = Math.ceil(_abs(angleExtent) / (TWOPI / 4)),
	      rawPath = [],
	      angleIncrement = angleExtent / segments,
	      controlLength = 4 / 3 * _sin(angleIncrement / 2) / (1 + _cos(angleIncrement / 2)),
	      ma = cosAngle * rx,
	      mb = sinAngle * rx,
	      mc = sinAngle * -ry,
	      md = cosAngle * ry,
	      i;

	  for (i = 0; i < segments; i++) {
	    angle = angleStart + i * angleIncrement;
	    x1 = _cos(angle);
	    y1 = _sin(angle);
	    ux = _cos(angle += angleIncrement);
	    uy = _sin(angle);
	    rawPath.push(x1 - controlLength * y1, y1 + controlLength * x1, ux + controlLength * uy, uy - controlLength * ux, ux, uy);
	  }

	  for (i = 0; i < rawPath.length; i += 2) {
	    x1 = rawPath[i];
	    y1 = rawPath[i + 1];
	    rawPath[i] = x1 * ma + y1 * mc + cx;
	    rawPath[i + 1] = x1 * mb + y1 * md + cy;
	  }

	  rawPath[i - 2] = x;
	  rawPath[i - 1] = y;
	  return rawPath;
	}

	function stringToRawPath(d) {
	  var a = (d + "").replace(_scientific, function (m) {
	    var n = +m;
	    return n < 0.0001 && n > -0.0001 ? 0 : n;
	  }).match(_svgPathExp) || [],
	      path = [],
	      relativeX = 0,
	      relativeY = 0,
	      twoThirds = 2 / 3,
	      elements = a.length,
	      points = 0,
	      errorMessage = "ERROR: malformed path: " + d,
	      i,
	      j,
	      x,
	      y,
	      command,
	      isRelative,
	      segment,
	      startX,
	      startY,
	      difX,
	      difY,
	      beziers,
	      prevCommand,
	      flag1,
	      flag2,
	      line = function line(sx, sy, ex, ey) {
	    difX = (ex - sx) / 3;
	    difY = (ey - sy) / 3;
	    segment.push(sx + difX, sy + difY, ex - difX, ey - difY, ex, ey);
	  };

	  if (!d || !isNaN(a[0]) || isNaN(a[1])) {
	    console.log(errorMessage);
	    return path;
	  }

	  for (i = 0; i < elements; i++) {
	    prevCommand = command;

	    if (isNaN(a[i])) {
	      command = a[i].toUpperCase();
	      isRelative = command !== a[i];
	    } else {
	      i--;
	    }

	    x = +a[i + 1];
	    y = +a[i + 2];

	    if (isRelative) {
	      x += relativeX;
	      y += relativeY;
	    }

	    if (!i) {
	      startX = x;
	      startY = y;
	    }

	    if (command === "M") {
	      if (segment) {
	        if (segment.length < 8) {
	          path.length -= 1;
	        } else {
	          points += segment.length;
	        }

	        _segmentIsClosed(segment);
	      }

	      relativeX = startX = x;
	      relativeY = startY = y;
	      segment = [x, y];
	      path.push(segment);
	      i += 2;
	      command = "L";
	    } else if (command === "C") {
	      if (!segment) {
	        segment = [0, 0];
	      }

	      if (!isRelative) {
	        relativeX = relativeY = 0;
	      }

	      segment.push(x, y, relativeX + a[i + 3] * 1, relativeY + a[i + 4] * 1, relativeX += a[i + 5] * 1, relativeY += a[i + 6] * 1);
	      i += 6;
	    } else if (command === "S") {
	      difX = relativeX;
	      difY = relativeY;

	      if (prevCommand === "C" || prevCommand === "S") {
	        difX += relativeX - segment[segment.length - 4];
	        difY += relativeY - segment[segment.length - 3];
	      }

	      if (!isRelative) {
	        relativeX = relativeY = 0;
	      }

	      segment.push(difX, difY, x, y, relativeX += a[i + 3] * 1, relativeY += a[i + 4] * 1);
	      i += 4;
	    } else if (command === "Q") {
	      difX = relativeX + (x - relativeX) * twoThirds;
	      difY = relativeY + (y - relativeY) * twoThirds;

	      if (!isRelative) {
	        relativeX = relativeY = 0;
	      }

	      relativeX += a[i + 3] * 1;
	      relativeY += a[i + 4] * 1;
	      segment.push(difX, difY, relativeX + (x - relativeX) * twoThirds, relativeY + (y - relativeY) * twoThirds, relativeX, relativeY);
	      i += 4;
	    } else if (command === "T") {
	      difX = relativeX - segment[segment.length - 4];
	      difY = relativeY - segment[segment.length - 3];
	      segment.push(relativeX + difX, relativeY + difY, x + (relativeX + difX * 1.5 - x) * twoThirds, y + (relativeY + difY * 1.5 - y) * twoThirds, relativeX = x, relativeY = y);
	      i += 2;
	    } else if (command === "H") {
	      line(relativeX, relativeY, relativeX = x, relativeY);
	      i += 1;
	    } else if (command === "V") {
	      line(relativeX, relativeY, relativeX, relativeY = x + (isRelative ? relativeY - relativeX : 0));
	      i += 1;
	    } else if (command === "L" || command === "Z") {
	      if (command === "Z") {
	        x = startX;
	        y = startY;
	        segment.closed = true;
	      }

	      if (command === "L" || _abs(relativeX - x) > 0.5 || _abs(relativeY - y) > 0.5) {
	        line(relativeX, relativeY, x, y);

	        if (command === "L") {
	          i += 2;
	        }
	      }

	      relativeX = x;
	      relativeY = y;
	    } else if (command === "A") {
	      flag1 = a[i + 4];
	      flag2 = a[i + 5];
	      difX = a[i + 6];
	      difY = a[i + 7];
	      j = 7;

	      if (flag1.length > 1) {
	        if (flag1.length < 3) {
	          difY = difX;
	          difX = flag2;
	          j--;
	        } else {
	          difY = flag2;
	          difX = flag1.substr(2);
	          j -= 2;
	        }

	        flag2 = flag1.charAt(1);
	        flag1 = flag1.charAt(0);
	      }

	      beziers = arcToSegment(relativeX, relativeY, +a[i + 1], +a[i + 2], +a[i + 3], +flag1, +flag2, (isRelative ? relativeX : 0) + difX * 1, (isRelative ? relativeY : 0) + difY * 1);
	      i += j;

	      if (beziers) {
	        for (j = 0; j < beziers.length; j++) {
	          segment.push(beziers[j]);
	        }
	      }

	      relativeX = segment[segment.length - 2];
	      relativeY = segment[segment.length - 1];
	    } else {
	      console.log(errorMessage);
	    }
	  }

	  i = segment.length;

	  if (i < 6) {
	    path.pop();
	    i = 0;
	  } else {
	    _segmentIsClosed(segment);
	  }

	  path.totalPoints = points + i;
	  return path;
	}
	function flatPointsToSegment(points, curviness) {
	  if (curviness === void 0) {
	    curviness = 1;
	  }

	  var x = points[0],
	      y = 0,
	      segment = [x, y],
	      i = 2;

	  for (; i < points.length; i += 2) {
	    segment.push(x, y, points[i], y = (points[i] - x) * curviness / 2, x = points[i], -y);
	  }

	  return segment;
	}
	function pointsToSegment(points, curviness) {
	  _abs(points[0] - points[2]) < 1e-4 && _abs(points[1] - points[3]) < 1e-4 && (points = points.slice(2));
	  var l = points.length - 2,
	      x = +points[0],
	      y = +points[1],
	      nextX = +points[2],
	      nextY = +points[3],
	      segment = [x, y, x, y],
	      dx2 = nextX - x,
	      dy2 = nextY - y,
	      nonSmooth = points.nonSmooth || [],
	      closed = Math.abs(points[l] - x) < 0.001 && Math.abs(points[l + 1] - y) < 0.001,
	      prevX,
	      prevY,
	      i,
	      dx1,
	      dy1,
	      r1,
	      r2,
	      r3,
	      tl,
	      mx1,
	      mx2,
	      mxm,
	      my1,
	      my2,
	      mym;

	  if (!l) {
	    return [x, y, x, y, x, y, x, y];
	  }

	  if (closed) {
	    points.push(nextX, nextY);
	    nextX = x;
	    nextY = y;
	    x = points[l - 2];
	    y = points[l - 1];
	    points.unshift(x, y);
	    l += 4;
	    nonSmooth = [0, 0].concat(nonSmooth);
	  }

	  curviness = curviness || curviness === 0 ? +curviness : 1;

	  for (i = 2; i < l; i += 2) {
	    prevX = x;
	    prevY = y;
	    x = nextX;
	    y = nextY;
	    nextX = +points[i + 2];
	    nextY = +points[i + 3];

	    if (x === nextX && y === nextY) {
	      continue;
	    }

	    dx1 = dx2;
	    dy1 = dy2;
	    dx2 = nextX - x;
	    dy2 = nextY - y;

	    if (nonSmooth[i]) {
	      segment.push(x - (x - prevX) / 4, y - (y - prevY) / 4, x, y, x + (nextX - x) / 4, y + (nextY - y) / 4);
	      continue;
	    }

	    r1 = _sqrt(dx1 * dx1 + dy1 * dy1);
	    r2 = _sqrt(dx2 * dx2 + dy2 * dy2);
	    r3 = _sqrt(Math.pow(dx2 / r2 + dx1 / r1, 2) + Math.pow(dy2 / r2 + dy1 / r1, 2));
	    tl = (r1 + r2) * curviness * 0.25 / r3;
	    mx1 = x - (x - prevX) * (r1 ? tl / r1 : 0);
	    mx2 = x + (nextX - x) * (r2 ? tl / r2 : 0);
	    mxm = x - (mx1 + ((mx2 - mx1) * (r1 * 3 / (r1 + r2) + 0.5) / 4 || 0));
	    my1 = y - (y - prevY) * (r1 ? tl / r1 : 0);
	    my2 = y + (nextY - y) * (r2 ? tl / r2 : 0);
	    mym = y - (my1 + ((my2 - my1) * (r1 * 3 / (r1 + r2) + 0.5) / 4 || 0));
	    segment.push(_round(mx1 + mxm), _round(my1 + mym), _round(x), _round(y), _round(mx2 + mxm), _round(my2 + mym));
	  }

	  x !== nextX || y !== nextY || segment.length < 4 ? segment.push(_round(nextX), _round(nextY), _round(nextX), _round(nextY)) : segment.length -= 2;

	  if (segment.length === 2) {
	    segment.push(x, y, x, y, x, y);
	  } else if (closed) {
	    segment.splice(0, 6);
	    segment.length -= 6;
	  }

	  segment.closed = closed;
	  return segment;
	}
	function rawPathToString(rawPath) {
	  if (_isNumber(rawPath[0])) {
	    rawPath = [rawPath];
	  }

	  var result = "",
	      l = rawPath.length,
	      sl,
	      s,
	      i,
	      segment;

	  for (s = 0; s < l; s++) {
	    segment = rawPath[s];
	    result += "M" + _round(segment[0]) + "," + _round(segment[1]) + " C";
	    sl = segment.length;

	    for (i = 2; i < sl; i++) {
	      result += _round(segment[i++]) + "," + _round(segment[i++]) + " " + _round(segment[i++]) + "," + _round(segment[i++]) + " " + _round(segment[i++]) + "," + _round(segment[i]) + " ";
	    }

	    if (segment.closed) {
	      result += "z";
	    }
	  }

	  return result;
	}

	var _doc,
	    _win,
	    _docElement,
	    _body,
	    _divContainer,
	    _svgContainer,
	    _identityMatrix,
	    _gEl,
	    _transformProp = "transform",
	    _transformOriginProp = _transformProp + "Origin",
	    _hasOffsetBug,
	    _setDoc = function _setDoc(element) {
	  var doc = element.ownerDocument || element;

	  if (!(_transformProp in element.style) && "msTransform" in element.style) {
	    _transformProp = "msTransform";
	    _transformOriginProp = _transformProp + "Origin";
	  }

	  while (doc.parentNode && (doc = doc.parentNode)) {}

	  _win = window;
	  _identityMatrix = new Matrix2D();

	  if (doc) {
	    _doc = doc;
	    _docElement = doc.documentElement;
	    _body = doc.body;
	    _gEl = _doc.createElementNS("http://www.w3.org/2000/svg", "g");
	    _gEl.style.transform = "none";
	    var d1 = doc.createElement("div"),
	        d2 = doc.createElement("div"),
	        root = doc && (doc.body || doc.firstElementChild);

	    if (root && root.appendChild) {
	      root.appendChild(d1);
	      d1.appendChild(d2);
	      d1.style.position = "static";
	      d1.style.transform = "translate3d(0,0,1px)";
	      _hasOffsetBug = d2.offsetParent !== d1;
	      root.removeChild(d1);
	    }
	  }

	  return doc;
	},
	    _forceNonZeroScale = function _forceNonZeroScale(e) {
	  var a, cache;

	  while (e && e !== _body) {
	    cache = e._gsap;
	    cache && cache.uncache && cache.get(e, "x");

	    if (cache && !cache.scaleX && !cache.scaleY && cache.renderTransform) {
	      cache.scaleX = cache.scaleY = 1e-4;
	      cache.renderTransform(1, cache);
	      a ? a.push(cache) : a = [cache];
	    }

	    e = e.parentNode;
	  }

	  return a;
	},
	    _svgTemps = [],
	    _divTemps = [],
	    _getDocScrollTop = function _getDocScrollTop() {
	  return _win.pageYOffset || _doc.scrollTop || _docElement.scrollTop || _body.scrollTop || 0;
	},
	    _getDocScrollLeft = function _getDocScrollLeft() {
	  return _win.pageXOffset || _doc.scrollLeft || _docElement.scrollLeft || _body.scrollLeft || 0;
	},
	    _svgOwner = function _svgOwner(element) {
	  return element.ownerSVGElement || ((element.tagName + "").toLowerCase() === "svg" ? element : null);
	},
	    _isFixed = function _isFixed(element) {
	  if (_win.getComputedStyle(element).position === "fixed") {
	    return true;
	  }

	  element = element.parentNode;

	  if (element && element.nodeType === 1) {
	    return _isFixed(element);
	  }
	},
	    _createSibling = function _createSibling(element, i) {
	  if (element.parentNode && (_doc || _setDoc(element))) {
	    var svg = _svgOwner(element),
	        ns = svg ? svg.getAttribute("xmlns") || "http://www.w3.org/2000/svg" : "http://www.w3.org/1999/xhtml",
	        type = svg ? i ? "rect" : "g" : "div",
	        x = i !== 2 ? 0 : 100,
	        y = i === 3 ? 100 : 0,
	        css = {
	      position: "absolute",
	      display: "block",
	      pointerEvents: "none",
	      margin: "0",
	      padding: "0"
	    },
	        e = _doc.createElementNS ? _doc.createElementNS(ns.replace(/^https/, "http"), type) : _doc.createElement(type);

	    if (i) {
	      if (!svg) {
	        if (!_divContainer) {
	          _divContainer = _createSibling(element);
	          Object.assign(_divContainer.style, css);
	        }

	        Object.assign(e.style, css, {
	          width: "0.1px",
	          height: "0.1px",
	          top: y + "px",
	          left: x + "px"
	        });

	        _divContainer.appendChild(e);
	      } else {
	        _svgContainer || (_svgContainer = _createSibling(element));
	        e.setAttribute("width", 0.01);
	        e.setAttribute("height", 0.01);
	        e.setAttribute("transform", "translate(" + x + "," + y + ")");
	        e.setAttribute("fill", "transparent");

	        _svgContainer.appendChild(e);
	      }
	    }

	    return e;
	  }

	  throw "Need document and parent.";
	},
	    _consolidate = function _consolidate(m) {
	  var c = new Matrix2D(),
	      i = 0;

	  for (; i < m.numberOfItems; i++) {
	    c.multiply(m.getItem(i).matrix);
	  }

	  return c;
	},
	    _getCTM = function _getCTM(svg) {
	  var m = svg.getCTM(),
	      transform;

	  if (!m) {
	    transform = svg.style[_transformProp];
	    svg.style[_transformProp] = "none";
	    svg.appendChild(_gEl);
	    m = _gEl.getCTM();
	    svg.removeChild(_gEl);
	    transform ? svg.style[_transformProp] = transform : svg.style.removeProperty(_transformProp.replace(/([A-Z])/g, "-$1").toLowerCase());
	  }

	  return m || _identityMatrix.clone();
	},
	    _placeSiblings = function _placeSiblings(element, adjustGOffset) {
	  var svg = _svgOwner(element),
	      isRootSVG = element === svg,
	      siblings = svg ? _svgTemps : _divTemps,
	      parent = element.parentNode,
	      appendToEl = parent && !svg && parent.shadowRoot && parent.shadowRoot.appendChild ? parent.shadowRoot : parent,
	      container,
	      m,
	      b,
	      x,
	      y,
	      cs;

	  if (element === _win) {
	    return element;
	  }

	  siblings.length || siblings.push(_createSibling(element, 1), _createSibling(element, 2), _createSibling(element, 3));
	  container = svg ? _svgContainer : _divContainer;

	  if (svg) {
	    if (isRootSVG) {
	      b = _getCTM(element);
	      x = -b.e / b.a;
	      y = -b.f / b.d;
	      m = _identityMatrix;
	    } else if (element.getBBox) {
	      b = element.getBBox();
	      m = element.transform ? element.transform.baseVal : {};
	      m = !m.numberOfItems ? _identityMatrix : m.numberOfItems > 1 ? _consolidate(m) : m.getItem(0).matrix;
	      x = m.a * b.x + m.c * b.y;
	      y = m.b * b.x + m.d * b.y;
	    } else {
	      m = new Matrix2D();
	      x = y = 0;
	    }

	    if (adjustGOffset && element.tagName.toLowerCase() === "g") {
	      x = y = 0;
	    }

	    (isRootSVG || !element.getBoundingClientRect().width ? svg : parent).appendChild(container);
	    container.setAttribute("transform", "matrix(" + m.a + "," + m.b + "," + m.c + "," + m.d + "," + (m.e + x) + "," + (m.f + y) + ")");
	  } else {
	    x = y = 0;

	    if (_hasOffsetBug) {
	      m = element.offsetParent;
	      b = element;

	      while (b && (b = b.parentNode) && b !== m && b.parentNode) {
	        if ((_win.getComputedStyle(b)[_transformProp] + "").length > 4) {
	          x = b.offsetLeft;
	          y = b.offsetTop;
	          b = 0;
	        }
	      }
	    }

	    cs = _win.getComputedStyle(element);

	    if (cs.position !== "absolute" && cs.position !== "fixed") {
	      m = element.offsetParent;

	      while (parent && parent !== m) {
	        x += parent.scrollLeft || 0;
	        y += parent.scrollTop || 0;
	        parent = parent.parentNode;
	      }
	    }

	    b = container.style;
	    b.top = element.offsetTop - y + "px";
	    b.left = element.offsetLeft - x + "px";
	    b[_transformProp] = cs[_transformProp];
	    b[_transformOriginProp] = cs[_transformOriginProp];
	    b.position = cs.position === "fixed" ? "fixed" : "absolute";
	    appendToEl.appendChild(container);
	  }

	  return container;
	},
	    _setMatrix = function _setMatrix(m, a, b, c, d, e, f) {
	  m.a = a;
	  m.b = b;
	  m.c = c;
	  m.d = d;
	  m.e = e;
	  m.f = f;
	  return m;
	};

	var Matrix2D = function () {
	  function Matrix2D(a, b, c, d, e, f) {
	    if (a === void 0) {
	      a = 1;
	    }

	    if (b === void 0) {
	      b = 0;
	    }

	    if (c === void 0) {
	      c = 0;
	    }

	    if (d === void 0) {
	      d = 1;
	    }

	    if (e === void 0) {
	      e = 0;
	    }

	    if (f === void 0) {
	      f = 0;
	    }

	    _setMatrix(this, a, b, c, d, e, f);
	  }

	  var _proto = Matrix2D.prototype;

	  _proto.inverse = function inverse() {
	    var a = this.a,
	        b = this.b,
	        c = this.c,
	        d = this.d,
	        e = this.e,
	        f = this.f,
	        determinant = a * d - b * c || 1e-10;
	    return _setMatrix(this, d / determinant, -b / determinant, -c / determinant, a / determinant, (c * f - d * e) / determinant, -(a * f - b * e) / determinant);
	  };

	  _proto.multiply = function multiply(matrix) {
	    var a = this.a,
	        b = this.b,
	        c = this.c,
	        d = this.d,
	        e = this.e,
	        f = this.f,
	        a2 = matrix.a,
	        b2 = matrix.c,
	        c2 = matrix.b,
	        d2 = matrix.d,
	        e2 = matrix.e,
	        f2 = matrix.f;
	    return _setMatrix(this, a2 * a + c2 * c, a2 * b + c2 * d, b2 * a + d2 * c, b2 * b + d2 * d, e + e2 * a + f2 * c, f + e2 * b + f2 * d);
	  };

	  _proto.clone = function clone() {
	    return new Matrix2D(this.a, this.b, this.c, this.d, this.e, this.f);
	  };

	  _proto.equals = function equals(matrix) {
	    var a = this.a,
	        b = this.b,
	        c = this.c,
	        d = this.d,
	        e = this.e,
	        f = this.f;
	    return a === matrix.a && b === matrix.b && c === matrix.c && d === matrix.d && e === matrix.e && f === matrix.f;
	  };

	  _proto.apply = function apply(point, decoratee) {
	    if (decoratee === void 0) {
	      decoratee = {};
	    }

	    var x = point.x,
	        y = point.y,
	        a = this.a,
	        b = this.b,
	        c = this.c,
	        d = this.d,
	        e = this.e,
	        f = this.f;
	    decoratee.x = x * a + y * c + e || 0;
	    decoratee.y = x * b + y * d + f || 0;
	    return decoratee;
	  };

	  return Matrix2D;
	}();
	function getGlobalMatrix(element, inverse, adjustGOffset, includeScrollInFixed) {
	  if (!element || !element.parentNode || (_doc || _setDoc(element)).documentElement === element) {
	    return new Matrix2D();
	  }

	  var zeroScales = _forceNonZeroScale(element),
	      svg = _svgOwner(element),
	      temps = svg ? _svgTemps : _divTemps,
	      container = _placeSiblings(element, adjustGOffset),
	      b1 = temps[0].getBoundingClientRect(),
	      b2 = temps[1].getBoundingClientRect(),
	      b3 = temps[2].getBoundingClientRect(),
	      parent = container.parentNode,
	      isFixed = !includeScrollInFixed && _isFixed(element),
	      m = new Matrix2D((b2.left - b1.left) / 100, (b2.top - b1.top) / 100, (b3.left - b1.left) / 100, (b3.top - b1.top) / 100, b1.left + (isFixed ? 0 : _getDocScrollLeft()), b1.top + (isFixed ? 0 : _getDocScrollTop()));

	  parent.removeChild(container);

	  if (zeroScales) {
	    b1 = zeroScales.length;

	    while (b1--) {
	      b2 = zeroScales[b1];
	      b2.scaleX = b2.scaleY = 0;
	      b2.renderTransform(1, b2);
	    }
	  }

	  return inverse ? m.inverse() : m;
	}

	/*!
	 * MotionPathPlugin 3.15.0
	 * https://gsap.com
	 *
	 * @license Copyright 2008-2026, GreenSock. All rights reserved.
	 * Subject to the terms at https://gsap.com/standard-license
	 * @author: Jack Doyle, jack@greensock.com
	*/

	var _xProps = "x,translateX,left,marginLeft,xPercent".split(","),
	    _yProps = "y,translateY,top,marginTop,yPercent".split(","),
	    _DEG2RAD$1 = Math.PI / 180,
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
	  return cacheRawPathMeasurements(segment, vars.resolution || (vars.curviness === 0 ? 20 : 12));
	},
	    _emptyFunc = function _emptyFunc(v) {
	  return v;
	},
	    _numExp = /[-+\.]*\d+\.?(?:e-|e\+)?\d*/g,
	    _originToPoint = function _originToPoint(element, origin, parentMatrix) {
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
	  }

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

	var MotionPathPlugin = {
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
	      this.rOffset = parseFloat(autoRotate) || 0;
	      this.radians = !!vars.useRadians;
	      this.rProp = vars.rotation || "rotation";
	      this.rSet = target._gsap.set(target, this.rProp, this);
	      this.ru = _getUnit(target._gsap.get(target, this.rProp)) || 0;
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

	      data.rotate && data.rSet(data.target, data.rProp, rawPaths[0].angle * (data.radians ? _DEG2RAD$1 : 1) + data.rOffset + data.ru, data, ratio);
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
	  convertToPath: function convertToPath$1(targets, swap) {
	    return _toArray(targets).map(function (target) {
	      return convertToPath(target, swap !== false);
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

	exports.MotionPathPlugin = MotionPathPlugin;
	exports.default = MotionPathPlugin;

	Object.defineProperty(exports, '__esModule', { value: true });

})));
