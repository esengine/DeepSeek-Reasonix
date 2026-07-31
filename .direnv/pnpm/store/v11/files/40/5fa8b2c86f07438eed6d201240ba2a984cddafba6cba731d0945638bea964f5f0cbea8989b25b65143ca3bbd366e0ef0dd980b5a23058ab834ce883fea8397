(function (global, factory) {
  typeof exports === 'object' && typeof module !== 'undefined' ? factory(exports) :
  typeof define === 'function' && define.amd ? define(['exports'], factory) :
  (global = global || self, factory(global.window = global.window || {}));
}(this, (function (exports) { 'use strict';

  function _extends() {
    _extends = Object.assign || function (target) {
      for (var i = 1; i < arguments.length; i++) {
        var source = arguments[i];

        for (var key in source) {
          if (Object.prototype.hasOwnProperty.call(source, key)) {
            target[key] = source[key];
          }
        }
      }

      return target;
    };

    return _extends.apply(this, arguments);
  }

  var _svgPathExp = /[achlmqstvz]|(-?\d*\.?\d*(?:e[\-+]?\d+)?)[0-9]/ig,
      _numbersExp = /(?:(-)?\d*\.?\d*(?:e[\-+]?\d+)?)[0-9]/ig,
      _scientific = /[\+\-]?\d*\.?\d+e[\+\-]?\d+/ig,
      _selectorExp = /(^[#\.][a-z]|[a-y][a-z])/i,
      _DEG2RAD = Math.PI / 180,
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
      _roundingNum = 1e5,
      _round = function _round(value) {
    return Math.round(value * _roundingNum) / _roundingNum || 0;
  },
      _segmentIsClosed = function _segmentIsClosed(segment) {
    return segment.closed = Math.abs(segment[0] - segment[segment.length - 2]) < 0.001 && Math.abs(segment[1] - segment[segment.length - 1]) < 0.001;
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
  function segmentToDistributedPoints(segment, totalPoints) {
    segment.samples || measureSegment(segment);
    var samples = segment.samples,
        lookup = segment.lookup,
        resolution = segment.resolution,
        totalLength = segment.totalLength,
        points = segment.slice(0, 2),
        curveStoppingPoints = [],
        l = segment.length - 4,
        i = 6,
        limit = 0.2,
        startLength = 0,
        curvePointsCumulative = 0,
        t,
        curvePoints,
        min,
        max,
        ci,
        ratioInc,
        j,
        inv,
        curveLength,
        length,
        a,
        nonSmooth,
        curveStoppingPointIndex,
        sampleIndex;

    for (; i < l; i += 6) {
      if (Math.abs(_atan2(segment[i + 1] - segment[i - 1], segment[i] - segment[i - 2]) - _atan2(segment[i + 3] - segment[i + 1], segment[i + 2] - segment[i])) > limit) {
        curveStoppingPoints.push(i);
      }
    }

    curveStoppingPoints.push(segment.length - 2);
    l = curveStoppingPoints.length;
    points.nonSmooth = nonSmooth = [1];

    if (totalPoints > l) {
      totalPoints -= l;

      for (ci = 0; ci < l; ci++) {
        curveStoppingPointIndex = curveStoppingPoints[ci];
        sampleIndex = Math.round(curveStoppingPointIndex / 6 * resolution);
        curveLength = samples[sampleIndex - 1] - startLength;
        curvePoints = Math.round(samples[sampleIndex - 1] / totalLength * totalPoints) - curvePointsCumulative;
        curvePointsCumulative += curvePoints;
        ratioInc = 1 / (curvePoints + 1);

        for (j = 1; j <= curvePoints; j++) {
          length = startLength + curveLength * j * ratioInc;
          i = lookup.length ? lookup[length < totalLength ? ~~(length / segment.minLength) : lookup.length - 1] || 0 : _getSampleIndex(samples, length, length / totalLength);
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
          points.push(_round((t * t * (segment[i + 6] - a) + 3 * inv * (t * (segment[i + 4] - a) + inv * (segment[i + 2] - a))) * t + a), _round((t * t * (segment[i + 7] - (a = segment[i + 1])) + 3 * inv * (t * (segment[i + 5] - a) + inv * (segment[i + 3] - a))) * t + a));
        }

        nonSmooth[points.length] = 1;
        points.push(segment[curveStoppingPointIndex], segment[curveStoppingPointIndex + 1]);
        startLength += curveLength;
      }
    }

    i = segment.length - 2;

    if (segment.closed && Math.abs(_atan2(segment[i + 1] - segment[i - 1], segment[i] - segment[i - 2]) - _atan2(segment[3] - segment[1], segment[2] - segment[0])) <= limit) {
      nonSmooth[0] = nonSmooth[nonSmooth.length - 1] = 0;
    }

    return points;
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

  var gsap,
      _toArray,
      _lastLinkedAnchor,
      _coreInitted,
      PluginClass,
      _reverting,
      _getGSAP = function _getGSAP() {
    return gsap || typeof window !== "undefined" && (gsap = window.gsap) && gsap.registerPlugin && gsap;
  },
      _isFunction = function _isFunction(value) {
    return typeof value === "function";
  },
      _atan2$1 = Math.atan2,
      _cos$1 = Math.cos,
      _sin$1 = Math.sin,
      _sqrt$1 = Math.sqrt,
      _PI = Math.PI,
      _2PI = _PI * 2,
      _angleMin = _PI * 0.3,
      _angleMax = _PI * 0.7,
      _bigNum = 1e20,
      _numExp = /[-+=.]*\d+[.e\-+]*\d*[e\-+]*\d*/gi,
      _selectorExp$1 = /(^[#.][a-z]|[a-y][a-z])/i,
      _commands = /[achlmqstvz]/i,
      _log = function _log(message) {
    return console && console.warn(message);
  },
      _round$1 = function _round(value) {
    return Math.round(value * 1e5) / 1e5 || 0;
  },
      _getAverageXY = function _getAverageXY(segment) {
    var l = segment.length,
        x = 0,
        y = 0,
        i;

    for (i = 0; i < l; i++) {
      x += segment[i++];
      y += segment[i];
    }

    return [x / (l / 2), y / (l / 2)];
  },
      _getSize = function _getSize(segment) {
    var l = segment.length,
        xMax = segment[0],
        xMin = xMax,
        yMax = segment[1],
        yMin = yMax,
        x,
        y,
        i;

    for (i = 6; i < l; i += 6) {
      x = segment[i];
      y = segment[i + 1];

      if (x > xMax) {
        xMax = x;
      } else if (x < xMin) {
        xMin = x;
      }

      if (y > yMax) {
        yMax = y;
      } else if (y < yMin) {
        yMin = y;
      }
    }

    segment.centerX = (xMax + xMin) / 2;
    segment.centerY = (yMax + yMin) / 2;
    return segment.size = (xMax - xMin) * (yMax - yMin);
  },
      _getTotalSize = function _getTotalSize(rawPath, samplesPerBezier) {
    if (samplesPerBezier === void 0) {
      samplesPerBezier = 3;
    }

    var j = rawPath.length,
        xMax = rawPath[0][0],
        xMin = xMax,
        yMax = rawPath[0][1],
        yMin = yMax,
        inc = 1 / samplesPerBezier,
        l,
        x,
        y,
        i,
        segment,
        k,
        t,
        inv,
        x1,
        y1,
        x2,
        x3,
        x4,
        y2,
        y3,
        y4;

    while (--j > -1) {
      segment = rawPath[j];
      l = segment.length;

      for (i = 6; i < l; i += 6) {
        x1 = segment[i];
        y1 = segment[i + 1];
        x2 = segment[i + 2] - x1;
        y2 = segment[i + 3] - y1;
        x3 = segment[i + 4] - x1;
        y3 = segment[i + 5] - y1;
        x4 = segment[i + 6] - x1;
        y4 = segment[i + 7] - y1;
        k = samplesPerBezier;

        while (--k > -1) {
          t = inc * k;
          inv = 1 - t;
          x = (t * t * x4 + 3 * inv * (t * x3 + inv * x2)) * t + x1;
          y = (t * t * y4 + 3 * inv * (t * y3 + inv * y2)) * t + y1;

          if (x > xMax) {
            xMax = x;
          } else if (x < xMin) {
            xMin = x;
          }

          if (y > yMax) {
            yMax = y;
          } else if (y < yMin) {
            yMin = y;
          }
        }
      }
    }

    rawPath.centerX = (xMax + xMin) / 2;
    rawPath.centerY = (yMax + yMin) / 2;
    rawPath.left = xMin;
    rawPath.width = xMax - xMin;
    rawPath.top = yMin;
    rawPath.height = yMax - yMin;
    return rawPath.size = (xMax - xMin) * (yMax - yMin);
  },
      _sortByComplexity = function _sortByComplexity(a, b) {
    return b.length - a.length;
  },
      _sortBySize = function _sortBySize(a, b) {
    var sizeA = a.size || _getSize(a),
        sizeB = b.size || _getSize(b);

    return Math.abs(sizeB - sizeA) < (sizeA + sizeB) / 20 ? b.centerX - a.centerX || b.centerY - a.centerY : sizeB - sizeA;
  },
      _offsetSegment = function _offsetSegment(segment, shapeIndex) {
    var a = segment.slice(0),
        l = segment.length,
        wrap = l - 2,
        i,
        index;
    shapeIndex = shapeIndex | 0;

    for (i = 0; i < l; i++) {
      index = (i + shapeIndex) % wrap;
      segment[i++] = a[index];
      segment[i] = a[index + 1];
    }
  },
      _getTotalMovement = function _getTotalMovement(sb, eb, shapeIndex, offsetX, offsetY) {
    var l = sb.length,
        d = 0,
        wrap = l - 2,
        index,
        i,
        x,
        y;
    shapeIndex *= 6;

    for (i = 0; i < l; i += 6) {
      index = (i + shapeIndex) % wrap;
      y = sb[index] - (eb[i] - offsetX);
      x = sb[index + 1] - (eb[i + 1] - offsetY);
      d += _sqrt$1(x * x + y * y);
    }

    return d;
  },
      _getClosestShapeIndex = function _getClosestShapeIndex(sb, eb, checkReverse) {
    var l = sb.length,
        sCenter = _getAverageXY(sb),
        eCenter = _getAverageXY(eb),
        offsetX = eCenter[0] - sCenter[0],
        offsetY = eCenter[1] - sCenter[1],
        min = _getTotalMovement(sb, eb, 0, offsetX, offsetY),
        minIndex = 0,
        copy,
        d,
        i;

    for (i = 6; i < l; i += 6) {
      d = _getTotalMovement(sb, eb, i / 6, offsetX, offsetY);

      if (d < min) {
        min = d;
        minIndex = i;
      }
    }

    if (checkReverse) {
      copy = sb.slice(0);
      reverseSegment(copy);

      for (i = 6; i < l; i += 6) {
        d = _getTotalMovement(copy, eb, i / 6, offsetX, offsetY);

        if (d < min) {
          min = d;
          minIndex = -i;
        }
      }
    }

    return minIndex / 6;
  },
      _getClosestAnchor = function _getClosestAnchor(rawPath, x, y) {
    var j = rawPath.length,
        closestDistance = _bigNum,
        closestX = 0,
        closestY = 0,
        segment,
        dx,
        dy,
        d,
        i,
        l;

    while (--j > -1) {
      segment = rawPath[j];
      l = segment.length;

      for (i = 0; i < l; i += 6) {
        dx = segment[i] - x;
        dy = segment[i + 1] - y;
        d = _sqrt$1(dx * dx + dy * dy);

        if (d < closestDistance) {
          closestDistance = d;
          closestX = segment[i];
          closestY = segment[i + 1];
        }
      }
    }

    return [closestX, closestY];
  },
      _getClosestSegment = function _getClosestSegment(bezier, pool, startIndex, sortRatio, offsetX, offsetY) {
    var l = pool.length,
        index = 0,
        minSize = Math.min(bezier.size || _getSize(bezier), pool[startIndex].size || _getSize(pool[startIndex])) * sortRatio,
        min = _bigNum,
        cx = bezier.centerX + offsetX,
        cy = bezier.centerY + offsetY,
        size,
        i,
        dx,
        dy,
        d;

    for (i = startIndex; i < l; i++) {
      size = pool[i].size || _getSize(pool[i]);

      if (size < minSize) {
        break;
      }

      dx = pool[i].centerX - cx;
      dy = pool[i].centerY - cy;
      d = _sqrt$1(dx * dx + dy * dy);

      if (d < min) {
        index = i;
        min = d;
      }
    }

    d = pool[index];
    pool.splice(index, 1);
    return d;
  },
      _addAnchorsToBezier = function _addAnchorsToBezier(segment, i, quantity) {
    if (quantity === void 0) {
      quantity = 1;
    }

    var ax = segment[i],
        ay = segment[i + 1],
        cp1x = segment[i + 2],
        cp1y = segment[i + 3],
        cp2x = segment[i + 4],
        cp2y = segment[i + 5],
        bx = segment[i + 6],
        by = segment[i + 7],
        t,
        x1a,
        x2,
        y1a,
        y2,
        x1,
        y1,
        x2a,
        y2a;

    while (quantity-- > 0) {
      t = 1 - 1 / (quantity + 2);
      x1a = ax + (cp1x - ax) * t;
      x2 = cp1x + (cp2x - cp1x) * t;
      y1a = ay + (cp1y - ay) * t;
      y2 = cp1y + (cp2y - cp1y) * t;
      x1 = x1a + (x2 - x1a) * t;
      y1 = y1a + (y2 - y1a) * t;
      x2a = cp2x + (bx - cp2x) * t;
      y2a = cp2y + (by - cp2y) * t;
      x2 += (x2a - x2) * t;
      y2 += (y2a - y2) * t;
      segment.splice(i + 2, 4, cp1x = _round$1(x1a), cp1y = _round$1(y1a), cp2x = _round$1(x1), cp2y = _round$1(y1), bx = _round$1(x1 + (x2 - x1) * t), by = _round$1(y1 + (y2 - y1) * t), _round$1(x2), _round$1(y2), _round$1(x2a), _round$1(y2a));
    }
  },
      _getLargestIndex = function _getLargestIndex(a) {
    var i = a.length,
        max = -_bigNum,
        largestIndex;

    while (i--) {
      if (a[i] > max) {
        max = a[i];
        largestIndex = i;
      }
    }

    return largestIndex;
  },
      _subdivideSegmentQty = function _subdivideSegmentQty(segment, quantity) {
    var distances = [],
        anchorsToAdd = [],
        l = segment.length - 2,
        i = 0;

    for (; i < l; i += 6) {
      distances.push(Math.pow(segment[i] - segment[i + 6], 2) + Math.pow(segment[i + 1] - segment[i + 7], 2));
    }

    while (quantity--) {
      i = _getLargestIndex(distances);
      anchorsToAdd[i] = l = (anchorsToAdd[i] || 0) + 1;
      distances[i] *= l / (l + 1);
    }

    i = distances.length;

    while (i--) {
      anchorsToAdd[i] && _addAnchorsToBezier(segment, i * 6, anchorsToAdd[i]);
    }
  },
      _getDefaultSmoothPoints = function _getDefaultSmoothPoints(rawPath, skipMeasure) {
    skipMeasure || cacheRawPathMeasurements(rawPath);
    return Math.max(4, Math.round(rawPath.totalLength / 4));
  },
      _cloneAndSortRawPath = function _cloneAndSortRawPath(ar) {
    return ar.slice(0).sort(_sortByComplexity);
  },
      _segmentCanBeIgnored = function _segmentCanBeIgnored(segment) {
    var x = segment[0],
        y = segment[1],
        i = 2;

    for (; i < segment.length; i += 2) {
      if (Math.abs(segment[i] - x) > 0.01 || Math.abs(segment[i + 1] - y) > 0.01) {
        return false;
      }
    }

    return true;
  },
      _smoothRawPath = function _smoothRawPath(rawPath, config) {
    config = config || {};
    var _config = config,
        redraw = _config.redraw,
        points = _config.points,
        _config$maxSegments = _config.maxSegments,
        maxSegments = _config$maxSegments === void 0 ? 999 : _config$maxSegments,
        pointsAdded = 0,
        sortedRawPath = rawPath,
        templateRawPath = Array.isArray(points) ? points : 0,
        segmentPointsToAdd,
        j,
        segment,
        smoothSegment,
        anchorDistance;
    redraw = redraw !== false;

    if (redraw) {
      cacheRawPathMeasurements(rawPath);
    } else {
      rawPath.totalPoints = 0;
      j = rawPath.length;

      while (j--) {
        rawPath.totalPoints += rawPath[j].length;
      }
    }

    if (templateRawPath) {
      sortedRawPath = _cloneAndSortRawPath(rawPath);
      templateRawPath = _cloneAndSortRawPath(templateRawPath);
      anchorDistance = templateRawPath[0].totalLength / Math.round(templateRawPath[0].length / 6);
    } else {
      if (!points || points === "auto") {
        points = _getDefaultSmoothPoints(rawPath, redraw);
        redraw || (points -= Math.round(rawPath.totalPoints / 6));
      }

      points = Math.max(redraw ? 10 : 4, Math.min(999, points));
    }

    for (j = 0; j < sortedRawPath.length; j++) {
      segment = sortedRawPath[j];
      segmentPointsToAdd = Math.max(redraw ? 10 : 4, templateRawPath ? Math.round(templateRawPath[j] ? templateRawPath[j].length / 6 : sortedRawPath[j].totalLength / anchorDistance || 0) : Math.round((pointsAdded / points + (redraw ? segment.totalLength / rawPath.totalLength : segment.length / rawPath.totalPoints)) * points) - pointsAdded);

      if (j >= maxSegments || templateRawPath && (!templateRawPath[j] || _segmentCanBeIgnored(templateRawPath[j]))) ; else if (redraw) {
        var _segment;

        smoothSegment = pointsToSegment(segmentToDistributedPoints(segment, segmentPointsToAdd), config.curviness);
        segment.length = 0;

        (_segment = segment).push.apply(_segment, smoothSegment);
      } else {
        _subdivideSegmentQty(segment, segmentPointsToAdd);
      }

      pointsAdded += segmentPointsToAdd;
    }

    return rawPath;
  },
      _equalizeSegmentQuantity = function _equalizeSegmentQuantity(start, end, shapeIndex, map, fillSafe) {
    var dif = end.length - start.length,
        longer = dif > 0 ? end : start,
        shorter = dif > 0 ? start : end,
        added = 0,
        sortMethod = map === "complexity" ? _sortByComplexity : _sortBySize,
        sortRatio = map === "position" ? 0 : typeof map === "number" ? map : 0.8,
        i = shorter.length,
        shapeIndices = typeof shapeIndex === "object" && shapeIndex.push ? shapeIndex.slice(0) : [shapeIndex],
        reverse = shapeIndices[0] === "reverse" || shapeIndices[0] < 0,
        log = shapeIndex === "log",
        eb,
        sb,
        b,
        x,
        y,
        offsetX,
        offsetY;

    if (!shorter[0]) {
      return;
    }

    if (longer.length > 1) {
      start.sort(sortMethod);
      end.sort(sortMethod);
      longer.size || _getTotalSize(longer);
      shorter.size || _getTotalSize(shorter);
      offsetX = longer.centerX - shorter.centerX;
      offsetY = longer.centerY - shorter.centerY;

      if (sortMethod === _sortBySize) {
        for (i = 0; i < shorter.length; i++) {
          longer.splice(i, 0, _getClosestSegment(shorter[i], longer, i, sortRatio, offsetX, offsetY));
        }
      }
    }

    if (dif) {
      dif < 0 && (dif = -dif);
      longer[0].length > shorter[0].length && _subdivideSegmentQty(shorter[0], (longer[0].length - shorter[0].length) / 6 | 0);
      i = shorter.length;

      while (added < dif) {
        x = longer[i].size || _getSize(longer[i]);
        b = _getClosestAnchor(shorter, longer[i].centerX, longer[i].centerY);
        x = b[0];
        y = b[1];
        shorter[i++] = [x, y, x, y, x, y, x, y];
        shorter.totalPoints += 8;
        added++;
      }
    }

    for (i = 0; i < start.length; i++) {
      eb = end[i];
      sb = start[i];
      dif = eb.length - sb.length;

      if (dif < 0) {
        _subdivideSegmentQty(eb, -dif / 6 | 0);
      } else if (dif > 0) {
        _subdivideSegmentQty(sb, dif / 6 | 0);
      }

      if (reverse && fillSafe !== false && !sb.reversed) {
        reverseSegment(sb);
      }

      shapeIndex = shapeIndices[i] || shapeIndices[i] === 0 ? shapeIndices[i] : "auto";

      if (shapeIndex) {
        if (sb.closed || Math.abs(sb[0] - sb[sb.length - 2]) < 0.5 && Math.abs(sb[1] - sb[sb.length - 1]) < 0.5) {
          if (shapeIndex === "auto" || shapeIndex === "log") {
            shapeIndices[i] = shapeIndex = _getClosestShapeIndex(sb, eb, !i || fillSafe === false);

            if (shapeIndex < 0) {
              reverse = true;
              reverseSegment(sb);
              shapeIndex = -shapeIndex;
            }

            _offsetSegment(sb, shapeIndex * 6);
          } else if (shapeIndex !== "reverse") {
            if (i && shapeIndex < 0) {
              reverseSegment(sb);
            }

            _offsetSegment(sb, (shapeIndex < 0 ? -shapeIndex : shapeIndex) * 6);
          }
        } else if (!reverse && (shapeIndex === "auto" && Math.abs(eb[0] - sb[0]) + Math.abs(eb[1] - sb[1]) + Math.abs(eb[eb.length - 2] - sb[sb.length - 2]) + Math.abs(eb[eb.length - 1] - sb[sb.length - 1]) > Math.abs(eb[0] - sb[sb.length - 2]) + Math.abs(eb[1] - sb[sb.length - 1]) + Math.abs(eb[eb.length - 2] - sb[0]) + Math.abs(eb[eb.length - 1] - sb[1]) || shapeIndex % 2)) {
          reverseSegment(sb);
          shapeIndices[i] = -1;
          reverse = true;
        } else if (shapeIndex === "auto") {
          shapeIndices[i] = 0;
        } else if (shapeIndex === "reverse") {
          shapeIndices[i] = -1;
        }

        if (sb.closed !== eb.closed) {
          sb.closed = eb.closed = false;
        }
      }
    }

    log && _log("shapeIndex:[" + shapeIndices.join(",") + "]");
    start.shapeIndex = shapeIndices;
    return shapeIndices;
  },
      _pathFilter = function _pathFilter(a, shapeIndex, map, precompile, fillSafe) {
    var start = stringToRawPath(a[0]),
        end = stringToRawPath(a[1]);

    if (!_equalizeSegmentQuantity(start, end, shapeIndex || shapeIndex === 0 ? shapeIndex : "auto", map, fillSafe)) {
      return;
    }

    a[0] = rawPathToString(start);
    a[1] = rawPathToString(end);
    (precompile === "log" || precompile === true) && _log('precompile:["' + a[0] + '","' + a[1] + '"]');
  },
      _offsetPoints = function _offsetPoints(text, offset) {
    if (!offset) {
      return text;
    }

    var a = text.match(_numExp) || [],
        l = a.length,
        s = "",
        inc,
        i,
        j;

    if (offset === "reverse") {
      i = l - 1;
      inc = -2;
    } else {
      i = ((parseInt(offset, 10) || 0) * 2 + 1 + l * 100) % l;
      inc = 2;
    }

    for (j = 0; j < l; j += 2) {
      s += a[i - 1] + "," + a[i] + " ";
      i = (i + inc) % l;
    }

    return s;
  },
      _equalizePointQuantity = function _equalizePointQuantity(a, quantity) {
    var tally = 0,
        x = parseFloat(a[0]),
        y = parseFloat(a[1]),
        s = x + "," + y + " ",
        max = 0.999999,
        newPointsPerSegment,
        i,
        l,
        j,
        factor,
        nextX,
        nextY;
    l = a.length;
    newPointsPerSegment = quantity * 0.5 / (l * 0.5 - 1);

    for (i = 0; i < l - 2; i += 2) {
      tally += newPointsPerSegment;
      nextX = parseFloat(a[i + 2]);
      nextY = parseFloat(a[i + 3]);

      if (tally > max) {
        factor = 1 / (Math.floor(tally) + 1);
        j = 1;

        while (tally > max) {
          s += (x + (nextX - x) * factor * j).toFixed(2) + "," + (y + (nextY - y) * factor * j).toFixed(2) + " ";
          tally--;
          j++;
        }
      }

      s += nextX + "," + nextY + " ";
      x = nextX;
      y = nextY;
    }

    return s;
  },
      _pointsFilter = function _pointsFilter(a) {
    var startNums = a[0].match(_numExp) || [],
        endNums = a[1].match(_numExp) || [],
        dif = endNums.length - startNums.length;

    if (dif > 0) {
      a[0] = _equalizePointQuantity(startNums, dif);
    } else {
      a[1] = _equalizePointQuantity(endNums, -dif);
    }
  },
      _buildPointsFilter = function _buildPointsFilter(shapeIndex) {
    return !isNaN(shapeIndex) ? function (a) {
      _pointsFilter(a);

      a[1] = _offsetPoints(a[1], parseInt(shapeIndex, 10));
    } : _pointsFilter;
  },
      _parseShape = function _parseShape(shape, forcePath, target) {
    var isString = typeof shape === "string",
        e,
        type;

    if (!isString || _selectorExp$1.test(shape) || (shape.match(_numExp) || []).length < 3) {
      e = _toArray(shape)[0];

      if (e) {
        type = (e.nodeName + "").toUpperCase();

        if (forcePath && type !== "PATH") {
          e = convertToPath(e, false);
          type = "PATH";
        }

        shape = e.getAttribute(type === "PATH" ? "d" : "points") || "";

        if (e === target) {
          shape = e.getAttributeNS(null, "data-original") || shape;
        }
      } else {
        _log("WARNING: invalid morph to: " + shape);

        shape = false;
      }
    }

    return shape;
  },
      _recordControlPointData = function _recordControlPointData(rawPath) {
    var j = rawPath.length,
        segment,
        x,
        y,
        x2,
        y2,
        i,
        l,
        cpData;

    while (--j > -1) {
      segment = rawPath[j];
      cpData = segment.cpData = segment.cpData || [];
      cpData.length = 0;
      l = segment.length - 2;

      for (i = 0; i < l; i += 6) {
        x = segment[i] - segment[i + 2];
        y = segment[i + 1] - segment[i + 3];
        x2 = segment[i + 6] - segment[i + 4];
        y2 = segment[i + 7] - segment[i + 5];
        cpData[i + 2] = _atan2$1(y, x);
        cpData[i + 3] = _sqrt$1(x * x + y * y);
        cpData[i + 4] = _atan2$1(y2, x2);
        cpData[i + 5] = _sqrt$1(x2 * x2 + y2 * y2);
      }
    }

    return rawPath;
  },
      _parseOriginFactors = function _parseOriginFactors(v) {
    var a = v.trim().split(" "),
        x = ~v.indexOf("left") ? 0 : ~v.indexOf("right") ? 100 : isNaN(parseFloat(a[0])) ? 50 : parseFloat(a[0]),
        y = ~v.indexOf("top") ? 0 : ~v.indexOf("bottom") ? 100 : isNaN(parseFloat(a[1])) ? 50 : parseFloat(a[1]);
    return {
      x: x / 100,
      y: y / 100
    };
  },
      _shortAngle = function _shortAngle(dif) {
    return dif !== dif % _PI ? dif + (dif < 0 ? _2PI : -_2PI) : dif;
  },
      _morphMessage = "Use MorphSVGPlugin.convertToPath() to convert to a path before morphing.",
      _tweenRotation = function _tweenRotation(start, end, i, linkedPT) {
    var so = this._origin,
        eo = this._eOrigin,
        dx = start[i] - so.x,
        dy = start[i + 1] - so.y,
        d = _sqrt$1(dx * dx + dy * dy),
        sa = _atan2$1(dy, dx),
        angleDif,
        _short;

    dx = end[i] - eo.x;
    dy = end[i + 1] - eo.y;
    angleDif = _atan2$1(dy, dx) - sa;
    _short = _shortAngle(angleDif);

    if (!linkedPT && _lastLinkedAnchor && Math.abs(_short + _lastLinkedAnchor.ca) < _angleMin) {
      linkedPT = _lastLinkedAnchor;
    }

    return this._anchorPT = _lastLinkedAnchor = {
      _next: this._anchorPT,
      t: start,
      sa: sa,
      ca: linkedPT && _short * linkedPT.ca < 0 && Math.abs(_short) > _angleMax ? angleDif : _short,
      sl: d,
      cl: _sqrt$1(dx * dx + dy * dy) - d,
      i: i
    };
  },
      _initCore = function _initCore(required) {
    gsap = _getGSAP();
    PluginClass = PluginClass || gsap && gsap.plugins.morphSVG;

    if (gsap && PluginClass) {
      _toArray = gsap.utils.toArray;

      _reverting = gsap.core.reverting || function () {};
      PluginClass.prototype._tweenRotation = _tweenRotation;
      _coreInitted = 1;
    } else if (required) {
      _log("Please gsap.registerPlugin(MorphSVGPlugin)");
    }
  };

  var MorphSVGPlugin = {
    version: "3.15.0",
    name: "morphSVG",
    rawVars: 1,
    register: function register(core, Plugin) {
      gsap = core;
      PluginClass = Plugin;

      _initCore();
    },
    init: function init(target, value, tween, index, targets) {
      _coreInitted || _initCore(1);

      if (!value) {
        _log("invalid shape");

        return false;
      }

      _isFunction(value) && (value = value.call(tween, index, target, targets));
      var type, p, pt, shape, isPoly, shapeIndex, map, startCPData, endCPData, start, end, i, j, l, startSeg, endSeg, precompiled, originFactors, useRotation, curveMode;

      if (typeof value === "string" || value.getBBox || value[0]) {
        value = {
          shape: value
        };
      } else if (typeof value === "object") {
        type = {};

        for (p in value) {
          type[p] = _isFunction(value[p]) && p !== "render" ? value[p].call(tween, index, target, targets) : value[p];
        }

        value = type;
      }

      var cs = target.nodeType ? window.getComputedStyle(target) : {},
          fill = cs.fill + "",
          fillSafe = !(fill === "none" || (fill.match(_numExp) || [])[3] === "0" || cs.fillRule === "evenodd"),
          smooth = value.smooth,
          origins = (value.origin || "50 50").split(",");
      smooth === true || smooth === "auto" ? smooth = {} : typeof smooth === "number" && (smooth = {
        points: smooth
      });
      type = (target.nodeName + "").toUpperCase();
      isPoly = type === "POLYLINE" || type === "POLYGON";

      if (type !== "PATH" && !isPoly && !value.prop) {
        _log("Cannot morph a <" + type + "> element. " + _morphMessage);

        return false;
      }

      p = type === "PATH" ? "d" : "points";

      if (!value.prop && !_isFunction(target.setAttribute)) {
        return false;
      }

      shape = _parseShape(value.shape || value.d || value.points || "", p === "d", target);

      if (isPoly && _commands.test(shape)) {
        _log("A <" + type + "> cannot accept path data. " + _morphMessage);

        return false;
      }

      shapeIndex = value.shapeIndex || value.shapeIndex === 0 ? value.shapeIndex : "auto";
      map = value.map || MorphSVGPlugin.defaultMap;
      this._prop = value.prop;
      this._render = value.render || MorphSVGPlugin.defaultRender;
      this._apply = "updateTarget" in value ? value.updateTarget : MorphSVGPlugin.defaultUpdateTarget;
      this._rnd = Math.pow(10, isNaN(value.precision) ? 2 : +value.precision);
      this._tween = tween;

      if (shape) {
        this._target = target;
        precompiled = typeof value.precompile === "object";
        start = this._original = this._prop ? target[this._prop] : target.getAttribute(p);

        if (!this._prop && !target.getAttributeNS(null, "data-original")) {
          target.setAttributeNS(null, "data-original", start);
        }

        if (p === "d" || this._prop) {
          start = stringToRawPath(precompiled ? value.precompile[0] : start);
          end = stringToRawPath(precompiled ? value.precompile[1] : shape);

          if (smooth) {
            j = start.length;

            while (--j) {
              _segmentCanBeIgnored(start[j]) && start.splice(j, 1);
            }

            _smoothRawPath(start, _extends({}, smooth, {
              points: +smooth.points || Math.max(_getDefaultSmoothPoints(start), _getDefaultSmoothPoints(end)),
              maxSegments: end.length
            }));

            _smoothRawPath(end, smooth.redraw === false ? smooth : _extends({}, smooth, {
              points: start
            }));
          }

          if (!precompiled && !_equalizeSegmentQuantity(start, end, shapeIndex, map, fillSafe)) {
            return false;
          }

          if (value.precompile === "log" || value.precompile === true) {
            _log('precompile:["' + rawPathToString(start) + '","' + rawPathToString(end) + '"]');
          }

          useRotation = (value.type || MorphSVGPlugin.defaultType) !== "linear";
          curveMode = value.curveMode || useRotation;

          _recordControlPointData(start);

          _recordControlPointData(end);

          if (useRotation) {
            start.size || _getTotalSize(start);
            end.size || _getTotalSize(end);
            originFactors = _parseOriginFactors(origins[0]);
            this._origin = start.origin = {
              x: start.left + originFactors.x * start.width,
              y: start.top + originFactors.y * start.height
            };
            origins[1] && (originFactors = _parseOriginFactors(origins[1]));
            this._eOrigin = {
              x: end.left + originFactors.x * end.width,
              y: end.top + originFactors.y * end.height
            };
          }

          this._rawPath = target._gsRawPath = start;
          j = start.length;

          while (--j > -1) {
            startSeg = start[j];
            endSeg = end[j];
            startCPData = startSeg.cpData;
            endCPData = endSeg.cpData;
            l = startSeg.length;
            _lastLinkedAnchor = 0;

            for (i = 0; i < l; i += 6) {
              if (endSeg[i] !== startSeg[i] || endSeg[i + 1] !== startSeg[i + 1]) {
                if (useRotation) {
                  pt = this._tweenRotation(startSeg, endSeg, i);
                } else {
                  pt = this.add(startSeg, i, startSeg[i], endSeg[i], 0, 0, 0, 0, 0, 1);
                  pt = this.add(startSeg, i + 1, startSeg[i + 1], endSeg[i + 1], 0, 0, 0, 0, 0, 1) || pt;
                }
              }
            }

            for (i = 0; i < l; i += 2) {
              if (curveMode && (startCPData[i] !== endCPData[i] || startCPData[i + 1] !== endCPData[i + 1]) && startCPData[i + 1] && endCPData[i + 1]) {
                this._controlPT = {
                  _next: this._controlPT,
                  i: i,
                  j: j,
                  ai: i % 6 > 3 ? i + 2 : i - 2,
                  sa: startCPData[i],
                  ca: _shortAngle(endCPData[i] - startCPData[i]),
                  sl: startCPData[i + 1],
                  cl: endCPData[i + 1] - startCPData[i + 1]
                };
              } else {
                endSeg[i] !== startSeg[i] && (pt = this.add(startSeg, i, startSeg[i], endSeg[i], 0, 0, 0, 0, 0, 1));
                endSeg[i + 1] !== startSeg[i + 1] && (pt = this.add(startSeg, i + 1, startSeg[i + 1], endSeg[i + 1], 0, 0, 0, 0, 0, 1) || pt);
              }
            }
          }
        } else {
          pt = this.add(target, "setAttribute", target.getAttribute(p) + "", shape + "", index, targets, 0, _buildPointsFilter(shapeIndex), p);
        }

        if (useRotation) {
          this.add(this._origin, "x", this._origin.x, this._eOrigin.x, 0, 0, 0, 0, 0, 1);
          pt = this.add(this._origin, "y", this._origin.y, this._eOrigin.y, 0, 0, 0, 0, 0, 1);
        }

        if (pt) {
          this._props.push("morphSVG");

          pt.end = smooth && smooth.persist !== false ? rawPathToString(end) : shape;
          pt.endProp = p;
        }
      }

      return 1;
    },
    render: function render(ratio, data) {
      var rawPath = data._rawPath,
          controlPT = data._controlPT,
          anchorPT = data._anchorPT,
          rnd = data._rnd,
          target = data._target,
          pt = data._pt,
          s,
          space,
          segment,
          l,
          angle,
          i,
          j,
          sin,
          cos;

      while (pt) {
        pt.r(ratio, pt.d);
        pt = pt._next;
      }

      if (ratio === 1 && data._apply) {
        pt = data._pt;

        while (pt) {
          if (pt.end) {
            if (data._prop) {
              target[data._prop] = pt.end;
            } else {
              target.setAttribute(pt.endProp, pt.end);
            }
          }

          pt = pt._next;
        }
      } else if (rawPath) {
        while (anchorPT) {
          angle = anchorPT.sa + ratio * anchorPT.ca;
          l = anchorPT.sl + ratio * anchorPT.cl;
          anchorPT.t[anchorPT.i] = data._origin.x + _cos$1(angle) * l;
          anchorPT.t[anchorPT.i + 1] = data._origin.y + _sin$1(angle) * l;
          anchorPT = anchorPT._next;
        }

        while (controlPT) {
          segment = rawPath[controlPT.j];
          i = controlPT.i;
          angle = controlPT.sa + ratio * controlPT.ca;
          sin = _sin$1(angle);
          cos = _cos$1(angle);
          l = controlPT.sl + ratio * controlPT.cl;
          segment[i] = segment[controlPT.ai] - cos * l;
          segment[i + 1] = segment[controlPT.ai + 1] - sin * l;
          controlPT = controlPT._next;
        }

        if (!ratio && _reverting()) {
          rawPath = stringToRawPath(data._original);
        }

        target._gsRawPath = rawPath;

        if (data._apply) {
          s = "";
          space = " ";

          for (j = 0; j < rawPath.length; j++) {
            segment = rawPath[j];
            l = segment.length;
            s += "M" + (segment[0] * rnd | 0) / rnd + space + (segment[1] * rnd | 0) / rnd + " C";

            for (i = 2; i < l; i++) {
              s += (segment[i] * rnd | 0) / rnd + space;
            }

            segment.closed && (s += "z");
          }

          if (data._prop) {
            target[data._prop] = s;
          } else {
            target.setAttribute("d", s);
          }
        }
      }

      data._render && rawPath && data._render.call(data._tween, rawPath, target);
    },
    kill: function kill(property) {
      this._pt = this._rawPath = 0;
    },
    getRawPath: getRawPath,
    stringToRawPath: stringToRawPath,
    rawPathToString: rawPathToString,
    smoothRawPath: _smoothRawPath,
    normalizeStrings: function normalizeStrings(shape1, shape2, _ref) {
      var shapeIndex = _ref.shapeIndex,
          map = _ref.map;
      var result = [shape1, shape2];

      _pathFilter(result, shapeIndex, map);

      return result;
    },
    pathFilter: _pathFilter,
    pointsFilter: _pointsFilter,
    getTotalSize: _getTotalSize,
    equalizeSegmentQuantity: _equalizeSegmentQuantity,
    convertToPath: function convertToPath$1(targets, swap) {
      return _toArray(targets).map(function (target) {
        return convertToPath(target, swap !== false);
      });
    },
    defaultType: "linear",
    defaultUpdateTarget: true,
    defaultMap: "size"
  };
  _getGSAP() && gsap.registerPlugin(MorphSVGPlugin);

  exports.MorphSVGPlugin = MorphSVGPlugin;
  exports.default = MorphSVGPlugin;

  if (typeof(window) === 'undefined' || window !== exports) {Object.defineProperty(exports, '__esModule', { value: true });} else {delete window.default;}

})));
