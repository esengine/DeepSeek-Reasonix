(function (global, factory) {
  typeof exports === 'object' && typeof module !== 'undefined' ? factory(exports) :
  typeof define === 'function' && define.amd ? define(['exports'], factory) :
  (global = global || self, factory(global.window = global.window || {}));
}(this, (function (exports) { 'use strict';

  function _inheritsLoose(subClass, superClass) {
    subClass.prototype = Object.create(superClass.prototype);
    subClass.prototype.constructor = subClass;
    subClass.__proto__ = superClass;
  }

  function _assertThisInitialized(self) {
    if (self === void 0) {
      throw new ReferenceError("this hasn't been initialised - super() hasn't been called");
    }

    return self;
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

  var gsap,
      _win$1,
      _doc$1,
      _docElement$1,
      _body$1,
      _tempDiv,
      _placeholderDiv,
      _coreInitted,
      _checkPrefix,
      _toArray,
      _supportsPassive,
      _isTouchDevice,
      _touchEventLookup,
      _isMultiTouching,
      _isAndroid,
      InertiaPlugin,
      _defaultCursor,
      _supportsPointer,
      _context,
      _getStyleSaver,
      _dragCount = 0,
      _windowExists = function _windowExists() {
    return typeof window !== "undefined";
  },
      _getGSAP = function _getGSAP() {
    return gsap || _windowExists() && (gsap = window.gsap) && gsap.registerPlugin && gsap;
  },
      _isFunction = function _isFunction(value) {
    return typeof value === "function";
  },
      _isObject = function _isObject(value) {
    return typeof value === "object";
  },
      _isUndefined = function _isUndefined(value) {
    return typeof value === "undefined";
  },
      _emptyFunc = function _emptyFunc() {
    return false;
  },
      _transformProp$1 = "transform",
      _transformOriginProp$1 = "transformOrigin",
      _round = function _round(value) {
    return Math.round(value * 10000) / 10000;
  },
      _isArray = Array.isArray,
      _createElement = function _createElement(type, ns) {
    var e = _doc$1.createElementNS ? _doc$1.createElementNS((ns || "http://www.w3.org/1999/xhtml").replace(/^https/, "http"), type) : _doc$1.createElement(type);
    return e.style ? e : _doc$1.createElement(type);
  },
      _RAD2DEG = 180 / Math.PI,
      _bigNum = 1e20,
      _identityMatrix$1 = new Matrix2D(),
      _getTime = Date.now || function () {
    return new Date().getTime();
  },
      _renderQueue = [],
      _lookup = {},
      _lookupCount = 0,
      _clickableTagExp = /^(?:a|input|textarea|button|select)$/i,
      _lastDragTime = 0,
      _temp1 = {},
      _windowProxy = {},
      _copy = function _copy(obj, factor) {
    var copy = {},
        p;

    for (p in obj) {
      copy[p] = factor ? obj[p] * factor : obj[p];
    }

    return copy;
  },
      _extend = function _extend(obj, defaults) {
    for (var p in defaults) {
      if (!(p in obj)) {
        obj[p] = defaults[p];
      }
    }

    return obj;
  },
      _setTouchActionForAllDescendants = function _setTouchActionForAllDescendants(elements, value) {
    var i = elements.length,
        children;

    while (i--) {
      value ? elements[i].style.touchAction = value : elements[i].style.removeProperty("touch-action");
      children = elements[i].children;
      children && children.length && _setTouchActionForAllDescendants(children, value);
    }
  },
      _renderQueueTick = function _renderQueueTick() {
    return _renderQueue.forEach(function (func) {
      return func();
    });
  },
      _addToRenderQueue = function _addToRenderQueue(func) {
    _renderQueue.push(func);

    if (_renderQueue.length === 1) {
      gsap.ticker.add(_renderQueueTick);
    }
  },
      _renderQueueTimeout = function _renderQueueTimeout() {
    return !_renderQueue.length && gsap.ticker.remove(_renderQueueTick);
  },
      _removeFromRenderQueue = function _removeFromRenderQueue(func) {
    var i = _renderQueue.length;

    while (i--) {
      if (_renderQueue[i] === func) {
        _renderQueue.splice(i, 1);
      }
    }

    gsap.to(_renderQueueTimeout, {
      overwrite: true,
      delay: 15,
      duration: 0,
      onComplete: _renderQueueTimeout,
      data: "_draggable"
    });
  },
      _setDefaults = function _setDefaults(obj, defaults) {
    for (var p in defaults) {
      if (!(p in obj)) {
        obj[p] = defaults[p];
      }
    }

    return obj;
  },
      _addListener = function _addListener(element, type, func, capture) {
    if (element.addEventListener) {
      var touchType = _touchEventLookup[type];
      capture = capture || (_supportsPassive ? {
        passive: false
      } : null);
      element.addEventListener(touchType || type, func, capture);
      touchType && type !== touchType && element.addEventListener(type, func, capture);
    }
  },
      _removeListener = function _removeListener(element, type, func, capture) {
    if (element.removeEventListener) {
      var touchType = _touchEventLookup[type];
      element.removeEventListener(touchType || type, func, capture);
      touchType && type !== touchType && element.removeEventListener(type, func, capture);
    }
  },
      _preventDefault = function _preventDefault(event) {
    event.preventDefault && event.preventDefault();
    event.preventManipulation && event.preventManipulation();
  },
      _hasTouchID = function _hasTouchID(list, ID) {
    var i = list.length;

    while (i--) {
      if (list[i].identifier === ID) {
        return true;
      }
    }
  },
      _onMultiTouchDocumentEnd = function _onMultiTouchDocumentEnd(event) {
    _isMultiTouching = event.touches && _dragCount < event.touches.length;

    _removeListener(event.target, "touchend", _onMultiTouchDocumentEnd);
  },
      _onMultiTouchDocument = function _onMultiTouchDocument(event) {
    _isMultiTouching = event.touches && _dragCount < event.touches.length;

    _addListener(event.target, "touchend", _onMultiTouchDocumentEnd);
  },
      _getDocScrollTop$1 = function _getDocScrollTop(doc) {
    return _win$1.pageYOffset || doc.scrollTop || doc.documentElement.scrollTop || doc.body.scrollTop || 0;
  },
      _getDocScrollLeft$1 = function _getDocScrollLeft(doc) {
    return _win$1.pageXOffset || doc.scrollLeft || doc.documentElement.scrollLeft || doc.body.scrollLeft || 0;
  },
      _addScrollListener = function _addScrollListener(e, callback) {
    _addListener(e, "scroll", callback);

    if (!_isRoot(e.parentNode)) {
      _addScrollListener(e.parentNode, callback);
    }
  },
      _removeScrollListener = function _removeScrollListener(e, callback) {
    _removeListener(e, "scroll", callback);

    if (!_isRoot(e.parentNode)) {
      _removeScrollListener(e.parentNode, callback);
    }
  },
      _isRoot = function _isRoot(e) {
    return !!(!e || e === _docElement$1 || e.nodeType === 9 || e === _doc$1.body || e === _win$1 || !e.nodeType || !e.parentNode);
  },
      _getMaxScroll = function _getMaxScroll(element, axis) {
    var dim = axis === "x" ? "Width" : "Height",
        scroll = "scroll" + dim,
        client = "client" + dim;
    return Math.max(0, _isRoot(element) ? Math.max(_docElement$1[scroll], _body$1[scroll]) - (_win$1["inner" + dim] || _docElement$1[client] || _body$1[client]) : element[scroll] - element[client]);
  },
      _recordMaxScrolls = function _recordMaxScrolls(e, skipCurrent) {
    var x = _getMaxScroll(e, "x"),
        y = _getMaxScroll(e, "y");

    if (_isRoot(e)) {
      e = _windowProxy;
    } else {
      _recordMaxScrolls(e.parentNode, skipCurrent);
    }

    e._gsMaxScrollX = x;
    e._gsMaxScrollY = y;

    if (!skipCurrent) {
      e._gsScrollX = e.scrollLeft || 0;
      e._gsScrollY = e.scrollTop || 0;
    }
  },
      _setStyle = function _setStyle(element, property, value) {
    var style = element.style;

    if (!style) {
      return;
    }

    if (_isUndefined(style[property])) {
      property = _checkPrefix(property, element) || property;
    }

    if (value == null) {
      style.removeProperty && style.removeProperty(property.replace(/([A-Z])/g, "-$1").toLowerCase());
    } else {
      style[property] = value;
    }
  },
      _getComputedStyle = function _getComputedStyle(element) {
    return _win$1.getComputedStyle(element instanceof Element ? element : element.host || (element.parentNode || {}).host || element);
  },
      _tempRect = {},
      _parseRect = function _parseRect(e) {
    if (e === _win$1) {
      _tempRect.left = _tempRect.top = 0;
      _tempRect.width = _tempRect.right = _docElement$1.clientWidth || e.innerWidth || _body$1.clientWidth || 0;
      _tempRect.height = _tempRect.bottom = (e.innerHeight || 0) - 20 < _docElement$1.clientHeight ? _docElement$1.clientHeight : e.innerHeight || _body$1.clientHeight || 0;
      return _tempRect;
    }

    var doc = e.ownerDocument || _doc$1,
        r = !_isUndefined(e.pageX) ? {
      left: e.pageX - _getDocScrollLeft$1(doc),
      top: e.pageY - _getDocScrollTop$1(doc),
      right: e.pageX - _getDocScrollLeft$1(doc) + 1,
      bottom: e.pageY - _getDocScrollTop$1(doc) + 1
    } : !e.nodeType && !_isUndefined(e.left) && !_isUndefined(e.top) ? e : _toArray(e)[0].getBoundingClientRect();

    if (_isUndefined(r.right) && !_isUndefined(r.width)) {
      r.right = r.left + r.width;
      r.bottom = r.top + r.height;
    } else if (_isUndefined(r.width)) {
      r = {
        width: r.right - r.left,
        height: r.bottom - r.top,
        right: r.right,
        left: r.left,
        bottom: r.bottom,
        top: r.top
      };
    }

    return r;
  },
      _dispatchEvent = function _dispatchEvent(target, type, callbackName) {
    var vars = target.vars,
        callback = vars[callbackName],
        listeners = target._listeners[type],
        result;

    if (_isFunction(callback)) {
      result = callback.apply(vars.callbackScope || target, vars[callbackName + "Params"] || [target.pointerEvent]);
    }

    if (listeners && target.dispatchEvent(type) === false) {
      result = false;
    }

    return result;
  },
      _getBounds = function _getBounds(target, context) {
    var e = _toArray(target)[0],
        top,
        left,
        offset;

    if (!e.nodeType && e !== _win$1) {
      if (!_isUndefined(target.left)) {
        offset = {
          x: 0,
          y: 0
        };
        return {
          left: target.left - offset.x,
          top: target.top - offset.y,
          width: target.width,
          height: target.height
        };
      }

      left = target.min || target.minX || target.minRotation || 0;
      top = target.min || target.minY || 0;
      return {
        left: left,
        top: top,
        width: (target.max || target.maxX || target.maxRotation || 0) - left,
        height: (target.max || target.maxY || 0) - top
      };
    }

    return _getElementBounds(e, context);
  },
      _point1 = {},
      _getElementBounds = function _getElementBounds(element, context) {
    context = _toArray(context)[0];
    var isSVG = element.getBBox && element.ownerSVGElement,
        doc = element.ownerDocument || _doc$1,
        left,
        right,
        top,
        bottom,
        matrix,
        p1,
        p2,
        p3,
        p4,
        bbox,
        width,
        height,
        cs;

    if (element === _win$1) {
      top = _getDocScrollTop$1(doc);
      left = _getDocScrollLeft$1(doc);
      right = left + (doc.documentElement.clientWidth || element.innerWidth || doc.body.clientWidth || 0);
      bottom = top + ((element.innerHeight || 0) - 20 < doc.documentElement.clientHeight ? doc.documentElement.clientHeight : element.innerHeight || doc.body.clientHeight || 0);
    } else if (context === _win$1 || _isUndefined(context)) {
      return element.getBoundingClientRect();
    } else {
      left = top = 0;

      if (isSVG) {
        bbox = element.getBBox();
        width = bbox.width;
        height = bbox.height;
      } else {
        if (element.viewBox && (bbox = element.viewBox.baseVal)) {
          left = bbox.x || 0;
          top = bbox.y || 0;
          width = bbox.width;
          height = bbox.height;
        }

        if (!width) {
          cs = _getComputedStyle(element);
          bbox = cs.boxSizing === "border-box";
          width = (parseFloat(cs.width) || element.clientWidth || 0) + (bbox ? 0 : parseFloat(cs.borderLeftWidth) + parseFloat(cs.borderRightWidth));
          height = (parseFloat(cs.height) || element.clientHeight || 0) + (bbox ? 0 : parseFloat(cs.borderTopWidth) + parseFloat(cs.borderBottomWidth));
        }
      }

      right = width;
      bottom = height;
    }

    if (element === context) {
      return {
        left: left,
        top: top,
        width: right - left,
        height: bottom - top
      };
    }

    matrix = getGlobalMatrix(context, true).multiply(getGlobalMatrix(element));
    p1 = matrix.apply({
      x: left,
      y: top
    });
    p2 = matrix.apply({
      x: right,
      y: top
    });
    p3 = matrix.apply({
      x: right,
      y: bottom
    });
    p4 = matrix.apply({
      x: left,
      y: bottom
    });
    left = Math.min(p1.x, p2.x, p3.x, p4.x);
    top = Math.min(p1.y, p2.y, p3.y, p4.y);
    return {
      left: left,
      top: top,
      width: Math.max(p1.x, p2.x, p3.x, p4.x) - left,
      height: Math.max(p1.y, p2.y, p3.y, p4.y) - top
    };
  },
      _parseInertia = function _parseInertia(draggable, snap, max, min, factor, forceZeroVelocity) {
    var vars = {},
        a,
        i,
        l;

    if (snap) {
      if (factor !== 1 && snap instanceof Array) {
        vars.end = a = [];
        l = snap.length;

        if (_isObject(snap[0])) {
          for (i = 0; i < l; i++) {
            a[i] = _copy(snap[i], factor);
          }
        } else {
          for (i = 0; i < l; i++) {
            a[i] = snap[i] * factor;
          }
        }

        max += 1.1;
        min -= 1.1;
      } else if (_isFunction(snap)) {
        vars.end = function (value) {
          var result = snap.call(draggable, value),
              copy,
              p;

          if (factor !== 1) {
            if (_isObject(result)) {
              copy = {};

              for (p in result) {
                copy[p] = result[p] * factor;
              }

              result = copy;
            } else {
              result *= factor;
            }
          }

          return result;
        };
      } else {
        vars.end = snap;
      }
    }

    if (max || max === 0) {
      vars.max = max;
    }

    if (min || min === 0) {
      vars.min = min;
    }

    if (forceZeroVelocity) {
      vars.velocity = 0;
    }

    return vars;
  },
      _isClickable = function _isClickable(element) {
    var data;
    return !element || !element.getAttribute || element === _body$1 ? false : (data = element.getAttribute("data-clickable")) === "true" || data !== "false" && (_clickableTagExp.test(element.nodeName + "") || element.getAttribute("contentEditable") === "true") ? true : _isClickable(element.parentNode);
  },
      _setSelectable = function _setSelectable(elements, selectable) {
    var i = elements.length,
        e;

    while (i--) {
      e = elements[i];
      e.ondragstart = e.onselectstart = selectable ? null : _emptyFunc;
      gsap.set(e, {
        lazy: true,
        userSelect: selectable ? "text" : "none"
      });
    }
  },
      _isFixed$1 = function _isFixed(element) {
    if (_getComputedStyle(element).position === "fixed") {
      return true;
    }

    element = element.parentNode;

    if (element && element.nodeType === 1) {
      return _isFixed(element);
    }
  },
      _supports3D,
      _addPaddingBR,
      ScrollProxy = function ScrollProxy(element, vars) {
    element = gsap.utils.toArray(element)[0];
    vars = vars || {};
    var content = document.createElement("div"),
        style = content.style,
        node = element.firstChild,
        offsetTop = 0,
        offsetLeft = 0,
        prevTop = element.scrollTop,
        prevLeft = element.scrollLeft,
        scrollWidth = element.scrollWidth,
        scrollHeight = element.scrollHeight,
        extraPadRight = 0,
        maxLeft = 0,
        maxTop = 0,
        elementWidth,
        elementHeight,
        contentHeight,
        nextNode,
        transformStart,
        transformEnd;

    if (_supports3D && vars.force3D !== false) {
      transformStart = "translate3d(";
      transformEnd = "px,0px)";
    } else if (_transformProp$1) {
      transformStart = "translate(";
      transformEnd = "px)";
    }

    this.scrollTop = function (value, force) {
      if (!arguments.length) {
        return -this.top();
      }

      this.top(-value, force);
    };

    this.scrollLeft = function (value, force) {
      if (!arguments.length) {
        return -this.left();
      }

      this.left(-value, force);
    };

    this.left = function (value, force) {
      if (!arguments.length) {
        return -(element.scrollLeft + offsetLeft);
      }

      var dif = element.scrollLeft - prevLeft,
          oldOffset = offsetLeft;

      if ((dif > 2 || dif < -2) && !force) {
        prevLeft = element.scrollLeft;
        gsap.killTweensOf(this, {
          left: 1,
          scrollLeft: 1
        });
        this.left(-prevLeft);

        if (vars.onKill) {
          vars.onKill();
        }

        return;
      }

      value = -value;

      if (value < 0) {
        offsetLeft = value - 0.5 | 0;
        value = 0;
      } else if (value > maxLeft) {
        offsetLeft = value - maxLeft | 0;
        value = maxLeft;
      } else {
        offsetLeft = 0;
      }

      if (offsetLeft || oldOffset) {
        if (!this._skip) {
          style[_transformProp$1] = transformStart + -offsetLeft + "px," + -offsetTop + transformEnd;
        }

        if (offsetLeft + extraPadRight >= 0) {
          style.paddingRight = offsetLeft + extraPadRight + "px";
        }
      }

      element.scrollLeft = value | 0;
      prevLeft = element.scrollLeft;
    };

    this.top = function (value, force) {
      if (!arguments.length) {
        return -(element.scrollTop + offsetTop);
      }

      var dif = element.scrollTop - prevTop,
          oldOffset = offsetTop;

      if ((dif > 2 || dif < -2) && !force) {
        prevTop = element.scrollTop;
        gsap.killTweensOf(this, {
          top: 1,
          scrollTop: 1
        });
        this.top(-prevTop);

        if (vars.onKill) {
          vars.onKill();
        }

        return;
      }

      value = -value;

      if (value < 0) {
        offsetTop = value - 0.5 | 0;
        value = 0;
      } else if (value > maxTop) {
        offsetTop = value - maxTop | 0;
        value = maxTop;
      } else {
        offsetTop = 0;
      }

      if (offsetTop || oldOffset) {
        if (!this._skip) {
          style[_transformProp$1] = transformStart + -offsetLeft + "px," + -offsetTop + transformEnd;
        }
      }

      element.scrollTop = value | 0;
      prevTop = element.scrollTop;
    };

    this.maxScrollTop = function () {
      return maxTop;
    };

    this.maxScrollLeft = function () {
      return maxLeft;
    };

    this.disable = function () {
      node = content.firstChild;

      while (node) {
        nextNode = node.nextSibling;
        element.appendChild(node);
        node = nextNode;
      }

      if (element === content.parentNode) {
        element.removeChild(content);
      }
    };

    this.enable = function () {
      node = element.firstChild;

      if (node === content) {
        return;
      }

      while (node) {
        nextNode = node.nextSibling;
        content.appendChild(node);
        node = nextNode;
      }

      element.appendChild(content);
      this.calibrate();
    };

    this.calibrate = function (force) {
      var widthMatches = element.clientWidth === elementWidth,
          cs,
          x,
          y;
      prevTop = element.scrollTop;
      prevLeft = element.scrollLeft;

      if (widthMatches && element.clientHeight === elementHeight && content.offsetHeight === contentHeight && scrollWidth === element.scrollWidth && scrollHeight === element.scrollHeight && !force) {
        return;
      }

      if (offsetTop || offsetLeft) {
        x = this.left();
        y = this.top();
        this.left(-element.scrollLeft);
        this.top(-element.scrollTop);
      }

      cs = _getComputedStyle(element);

      if (!widthMatches || force) {
        style.display = "block";
        style.width = "auto";
        style.paddingRight = "0px";
        extraPadRight = Math.max(0, element.scrollWidth - element.clientWidth);

        if (extraPadRight) {
          extraPadRight += parseFloat(cs.paddingLeft) + (_addPaddingBR ? parseFloat(cs.paddingRight) : 0);
        }
      }

      style.display = "inline-block";
      style.position = "relative";
      style.overflow = "visible";
      style.verticalAlign = "top";
      style.boxSizing = "content-box";
      style.width = "100%";
      style.paddingRight = extraPadRight + "px";

      if (_addPaddingBR) {
        style.paddingBottom = cs.paddingBottom;
      }

      elementWidth = element.clientWidth;
      elementHeight = element.clientHeight;
      scrollWidth = element.scrollWidth;
      scrollHeight = element.scrollHeight;
      maxLeft = element.scrollWidth - elementWidth;
      maxTop = element.scrollHeight - elementHeight;
      contentHeight = content.offsetHeight;
      style.display = "block";

      if (x || y) {
        this.left(x);
        this.top(y);
      }
    };

    this.content = content;
    this.element = element;
    this._skip = false;
    this.enable();
  },
      _initCore = function _initCore(required) {
    if (_windowExists() && document.body) {
      var nav = window && window.navigator;
      _win$1 = window;
      _doc$1 = document;
      _docElement$1 = _doc$1.documentElement;
      _body$1 = _doc$1.body;
      _tempDiv = _createElement("div");
      _supportsPointer = !!window.PointerEvent;
      _placeholderDiv = _createElement("div");
      _placeholderDiv.style.cssText = "visibility:hidden;height:1px;top:-1px;pointer-events:none;position:relative;clear:both;cursor:grab";
      _defaultCursor = _placeholderDiv.style.cursor === "grab" ? "grab" : "move";
      _isAndroid = nav && nav.userAgent.toLowerCase().indexOf("android") !== -1;
      _isTouchDevice = "ontouchstart" in _docElement$1 && "orientation" in _win$1 || nav && (nav.MaxTouchPoints > 0 || nav.msMaxTouchPoints > 0);

      _addPaddingBR = function () {
        var div = _createElement("div"),
            child = _createElement("div"),
            childStyle = child.style,
            parent = _body$1,
            val;

        childStyle.display = "inline-block";
        childStyle.position = "relative";
        div.style.cssText = "width:90px;height:40px;padding:10px;overflow:auto;visibility:hidden";
        div.appendChild(child);
        parent.appendChild(div);
        val = child.offsetHeight + 18 > div.scrollHeight;
        parent.removeChild(div);
        return val;
      }();

      _touchEventLookup = function (types) {
        var standard = types.split(","),
            converted = ("onpointerdown" in _tempDiv ? "pointerdown,pointermove,pointerup,pointercancel" : "onmspointerdown" in _tempDiv ? "MSPointerDown,MSPointerMove,MSPointerUp,MSPointerCancel" : types).split(","),
            obj = {},
            i = 4;

        while (--i > -1) {
          obj[standard[i]] = converted[i];
          obj[converted[i]] = standard[i];
        }

        try {
          _docElement$1.addEventListener("test", null, Object.defineProperty({}, "passive", {
            get: function get() {
              _supportsPassive = 1;
            }
          }));
        } catch (e) {}

        return obj;
      }("touchstart,touchmove,touchend,touchcancel");

      _addListener(_doc$1, "touchcancel", _emptyFunc);

      _addListener(_win$1, "touchmove", _emptyFunc);

      _body$1 && _body$1.addEventListener("touchstart", _emptyFunc);

      _addListener(_doc$1, "contextmenu", function () {
        for (var p in _lookup) {
          if (_lookup[p].isPressed) {
            _lookup[p].endDrag();
          }
        }
      });

      gsap = _coreInitted = _getGSAP();
    }

    if (gsap) {
      InertiaPlugin = gsap.plugins.inertia;

      _context = gsap.core.context || function () {};

      _checkPrefix = gsap.utils.checkPrefix;
      _transformProp$1 = _checkPrefix(_transformProp$1);
      _transformOriginProp$1 = _checkPrefix(_transformOriginProp$1);
      _toArray = gsap.utils.toArray;
      _getStyleSaver = gsap.core.getStyleSaver;
      _supports3D = !!_checkPrefix("perspective");
    } else if (required) {
      console.warn("Please gsap.registerPlugin(Draggable)");
    }
  };

  var EventDispatcher = function () {
    function EventDispatcher(target) {
      this._listeners = {};
      this.target = target || this;
    }

    var _proto = EventDispatcher.prototype;

    _proto.addEventListener = function addEventListener(type, callback) {
      var list = this._listeners[type] || (this._listeners[type] = []);

      if (!~list.indexOf(callback)) {
        list.push(callback);
      }
    };

    _proto.removeEventListener = function removeEventListener(type, callback) {
      var list = this._listeners[type],
          i = list && list.indexOf(callback);
      i >= 0 && list.splice(i, 1);
    };

    _proto.dispatchEvent = function dispatchEvent(type) {
      var _this = this;

      var result;
      (this._listeners[type] || []).forEach(function (callback) {
        return callback.call(_this, {
          type: type,
          target: _this.target
        }) === false && (result = false);
      });
      return result;
    };

    return EventDispatcher;
  }();

  var Draggable = function (_EventDispatcher) {
    _inheritsLoose(Draggable, _EventDispatcher);

    function Draggable(target, vars) {
      var _this2;

      _this2 = _EventDispatcher.call(this) || this;
      _coreInitted || _initCore(1);
      target = _toArray(target)[0];
      _this2.styles = _getStyleSaver && _getStyleSaver(target, "transform,left,top");

      if (!InertiaPlugin) {
        InertiaPlugin = gsap.plugins.inertia;
      }

      _this2.vars = vars = _copy(vars || {});
      _this2.target = target;
      _this2.x = _this2.y = _this2.rotation = 0;
      _this2.dragResistance = parseFloat(vars.dragResistance) || 0;
      _this2.edgeResistance = isNaN(vars.edgeResistance) ? 1 : parseFloat(vars.edgeResistance) || 0;
      _this2.lockAxis = vars.lockAxis;
      _this2.autoScroll = vars.autoScroll || 0;
      _this2.lockedAxis = null;
      _this2.allowEventDefault = !!vars.allowEventDefault;
      gsap.getProperty(target, "x");

      var type = (vars.type || "x,y").toLowerCase(),
          xyMode = ~type.indexOf("x") || ~type.indexOf("y"),
          rotationMode = type.indexOf("rotation") !== -1,
          xProp = rotationMode ? "rotation" : xyMode ? "x" : "left",
          yProp = xyMode ? "y" : "top",
          allowX = !!(~type.indexOf("x") || ~type.indexOf("left") || type === "scroll"),
          allowY = !!(~type.indexOf("y") || ~type.indexOf("top") || type === "scroll"),
          minimumMovement = vars.minimumMovement || 2,
          self = _assertThisInitialized(_this2),
          triggers = _toArray(vars.trigger || vars.handle || target),
          killProps = {},
          dragEndTime = 0,
          checkAutoScrollBounds = false,
          autoScrollMarginTop = vars.autoScrollMarginTop || 40,
          autoScrollMarginRight = vars.autoScrollMarginRight || 40,
          autoScrollMarginBottom = vars.autoScrollMarginBottom || 40,
          autoScrollMarginLeft = vars.autoScrollMarginLeft || 40,
          isClickable = vars.clickableTest || _isClickable,
          clickTime = 0,
          gsCache = target._gsap || gsap.core.getCache(target),
          isFixed = _isFixed$1(target),
          getPropAsNum = function getPropAsNum(property, unit) {
        return parseFloat(gsCache.get(target, property, unit));
      },
          ownerDoc = target.ownerDocument || _doc$1,
          enabled,
          scrollProxy,
          startPointerX,
          startPointerY,
          startElementX,
          startElementY,
          hasBounds,
          hasDragCallback,
          hasMoveCallback,
          maxX,
          minX,
          maxY,
          minY,
          touch,
          touchID,
          rotationOrigin,
          dirty,
          old,
          snapX,
          snapY,
          snapXY,
          isClicking,
          touchEventTarget,
          matrix,
          interrupted,
          allowNativeTouchScrolling,
          touchDragAxis,
          isDispatching,
          clickDispatch,
          trustedClickDispatch,
          isPreventingDefault,
          innerMatrix,
          dragged,
          onContextMenu = function onContextMenu(e) {
        _preventDefault(e);

        e.stopImmediatePropagation && e.stopImmediatePropagation();
        return false;
      },
          render = function render(suppressEvents) {
        if (self.autoScroll && self.isDragging && (checkAutoScrollBounds || dirty)) {
          var e = target,
              autoScrollFactor = self.autoScroll * 15,
              parent,
              isRoot,
              rect,
              pointerX,
              pointerY,
              changeX,
              changeY,
              gap;
          checkAutoScrollBounds = false;
          _windowProxy.scrollTop = _win$1.pageYOffset != null ? _win$1.pageYOffset : ownerDoc.documentElement.scrollTop != null ? ownerDoc.documentElement.scrollTop : ownerDoc.body.scrollTop;
          _windowProxy.scrollLeft = _win$1.pageXOffset != null ? _win$1.pageXOffset : ownerDoc.documentElement.scrollLeft != null ? ownerDoc.documentElement.scrollLeft : ownerDoc.body.scrollLeft;
          pointerX = self.pointerX - _windowProxy.scrollLeft;
          pointerY = self.pointerY - _windowProxy.scrollTop;

          while (e && !isRoot) {
            isRoot = _isRoot(e.parentNode);
            parent = isRoot ? _windowProxy : e.parentNode;
            rect = isRoot ? {
              bottom: Math.max(_docElement$1.clientHeight, _win$1.innerHeight || 0),
              right: Math.max(_docElement$1.clientWidth, _win$1.innerWidth || 0),
              left: 0,
              top: 0
            } : parent.getBoundingClientRect();
            changeX = changeY = 0;

            if (allowY) {
              gap = parent._gsMaxScrollY - parent.scrollTop;

              if (gap < 0) {
                changeY = gap;
              } else if (pointerY > rect.bottom - autoScrollMarginBottom && gap) {
                checkAutoScrollBounds = true;
                changeY = Math.min(gap, autoScrollFactor * (1 - Math.max(0, rect.bottom - pointerY) / autoScrollMarginBottom) | 0);
              } else if (pointerY < rect.top + autoScrollMarginTop && parent.scrollTop) {
                checkAutoScrollBounds = true;
                changeY = -Math.min(parent.scrollTop, autoScrollFactor * (1 - Math.max(0, pointerY - rect.top) / autoScrollMarginTop) | 0);
              }

              if (changeY) {
                parent.scrollTop += changeY;
              }
            }

            if (allowX) {
              gap = parent._gsMaxScrollX - parent.scrollLeft;

              if (gap < 0) {
                changeX = gap;
              } else if (pointerX > rect.right - autoScrollMarginRight && gap) {
                checkAutoScrollBounds = true;
                changeX = Math.min(gap, autoScrollFactor * (1 - Math.max(0, rect.right - pointerX) / autoScrollMarginRight) | 0);
              } else if (pointerX < rect.left + autoScrollMarginLeft && parent.scrollLeft) {
                checkAutoScrollBounds = true;
                changeX = -Math.min(parent.scrollLeft, autoScrollFactor * (1 - Math.max(0, pointerX - rect.left) / autoScrollMarginLeft) | 0);
              }

              if (changeX) {
                parent.scrollLeft += changeX;
              }
            }

            if (isRoot && (changeX || changeY)) {
              _win$1.scrollTo(parent.scrollLeft, parent.scrollTop);

              setPointerPosition(self.pointerX + changeX, self.pointerY + changeY);
            }

            e = parent;
          }
        }

        if (dirty) {
          var x = self.x,
              y = self.y;

          if (rotationMode) {
            self.deltaX = x - parseFloat(gsCache.rotation);
            self.rotation = x;
            gsCache.rotation = x + "deg";
            gsCache.renderTransform(1, gsCache);
          } else {
            if (scrollProxy) {
              if (allowY) {
                self.deltaY = y - scrollProxy.top();
                scrollProxy.top(y);
              }

              if (allowX) {
                self.deltaX = x - scrollProxy.left();
                scrollProxy.left(x);
              }
            } else if (xyMode) {
              if (allowY) {
                self.deltaY = y - parseFloat(gsCache.y);
                gsCache.y = y + "px";
              }

              if (allowX) {
                self.deltaX = x - parseFloat(gsCache.x);
                gsCache.x = x + "px";
              }

              gsCache.renderTransform(1, gsCache);
            } else {
              if (allowY) {
                self.deltaY = y - parseFloat(target.style.top || 0);
                target.style.top = y + "px";
              }

              if (allowX) {
                self.deltaX = x - parseFloat(target.style.left || 0);
                target.style.left = x + "px";
              }
            }
          }

          if (hasDragCallback && !suppressEvents && !isDispatching) {
            isDispatching = true;

            if (_dispatchEvent(self, "drag", "onDrag") === false) {
              if (allowX) {
                self.x -= self.deltaX;
              }

              if (allowY) {
                self.y -= self.deltaY;
              }

              render(true);
            }

            isDispatching = false;
          }
        }

        dirty = false;
      },
          syncXY = function syncXY(skipOnUpdate, skipSnap) {
        var x = self.x,
            y = self.y,
            snappedValue,
            cs;

        if (!target._gsap) {
          gsCache = gsap.core.getCache(target);
        }

        gsCache.uncache && gsap.getProperty(target, "x");

        if (xyMode) {
          self.x = parseFloat(gsCache.x);
          self.y = parseFloat(gsCache.y);
        } else if (rotationMode) {
          self.x = self.rotation = _round(parseFloat(gsCache.rotation));
        } else if (scrollProxy) {
          self.y = scrollProxy.top();
          self.x = scrollProxy.left();
        } else {
          self.y = parseFloat(target.style.top || (cs = _getComputedStyle(target)) && cs.top) || 0;
          self.x = parseFloat(target.style.left || (cs || {}).left) || 0;
        }

        if ((snapX || snapY || snapXY) && !skipSnap && (self.isDragging || self.isThrowing)) {
          if (snapXY) {
            _temp1.x = self.x;
            _temp1.y = self.y;
            snappedValue = snapXY(_temp1);

            if (snappedValue.x !== self.x) {
              self.x = snappedValue.x;
              dirty = true;
            }

            if (snappedValue.y !== self.y) {
              self.y = snappedValue.y;
              dirty = true;
            }
          }

          if (snapX) {
            snappedValue = snapX(self.x);

            if (snappedValue !== self.x) {
              self.x = snappedValue;

              if (rotationMode) {
                self.rotation = snappedValue;
              }

              dirty = true;
            }
          }

          if (snapY) {
            snappedValue = snapY(self.y);

            if (snappedValue !== self.y) {
              self.y = snappedValue;
            }

            dirty = true;
          }
        }

        dirty && render(true);

        if (!skipOnUpdate) {
          self.deltaX = self.x - x;
          self.deltaY = self.y - y;

          _dispatchEvent(self, "throwupdate", "onThrowUpdate");
        }
      },
          buildSnapFunc = function buildSnapFunc(snap, min, max, factor) {
        if (min == null) {
          min = -_bigNum;
        }

        if (max == null) {
          max = _bigNum;
        }

        if (_isFunction(snap)) {
          return function (n) {
            var edgeTolerance = !self.isPressed ? 1 : 1 - self.edgeResistance;
            return snap.call(self, (n > max ? max + (n - max) * edgeTolerance : n < min ? min + (n - min) * edgeTolerance : n) * factor) * factor;
          };
        }

        if (_isArray(snap)) {
          return function (n) {
            var i = snap.length,
                closest = 0,
                absDif = _bigNum,
                val,
                dif;

            while (--i > -1) {
              val = snap[i];
              dif = val - n;

              if (dif < 0) {
                dif = -dif;
              }

              if (dif < absDif && val >= min && val <= max) {
                closest = i;
                absDif = dif;
              }
            }

            return snap[closest];
          };
        }

        return isNaN(snap) ? function (n) {
          return n;
        } : function () {
          return snap * factor;
        };
      },
          buildPointSnapFunc = function buildPointSnapFunc(snap, minX, maxX, minY, maxY, radius, factor) {
        radius = radius && radius < _bigNum ? radius * radius : _bigNum;

        if (_isFunction(snap)) {
          return function (point) {
            var edgeTolerance = !self.isPressed ? 1 : 1 - self.edgeResistance,
                x = point.x,
                y = point.y,
                result,
                dx,
                dy;
            point.x = x = x > maxX ? maxX + (x - maxX) * edgeTolerance : x < minX ? minX + (x - minX) * edgeTolerance : x;
            point.y = y = y > maxY ? maxY + (y - maxY) * edgeTolerance : y < minY ? minY + (y - minY) * edgeTolerance : y;
            result = snap.call(self, point);

            if (result !== point) {
              point.x = result.x;
              point.y = result.y;
            }

            if (factor !== 1) {
              point.x *= factor;
              point.y *= factor;
            }

            if (radius < _bigNum) {
              dx = point.x - x;
              dy = point.y - y;

              if (dx * dx + dy * dy > radius) {
                point.x = x;
                point.y = y;
              }
            }

            return point;
          };
        }

        if (_isArray(snap)) {
          return function (p) {
            var i = snap.length,
                closest = 0,
                minDist = _bigNum,
                x,
                y,
                point,
                dist;

            while (--i > -1) {
              point = snap[i];
              x = point.x - p.x;
              y = point.y - p.y;
              dist = x * x + y * y;

              if (dist < minDist) {
                closest = i;
                minDist = dist;
              }
            }

            return minDist <= radius ? snap[closest] : p;
          };
        }

        return function (n) {
          return n;
        };
      },
          calculateBounds = function calculateBounds() {
        var bounds, targetBounds, snap, snapIsRaw;
        hasBounds = false;

        if (scrollProxy) {
          scrollProxy.calibrate();
          self.minX = minX = -scrollProxy.maxScrollLeft();
          self.minY = minY = -scrollProxy.maxScrollTop();
          self.maxX = maxX = self.maxY = maxY = 0;
          hasBounds = true;
        } else if (!!vars.bounds) {
          bounds = _getBounds(vars.bounds, target.parentNode);

          if (rotationMode) {
            self.minX = minX = bounds.left;
            self.maxX = maxX = bounds.left + bounds.width;
            self.minY = minY = self.maxY = maxY = 0;
          } else if (!_isUndefined(vars.bounds.maxX) || !_isUndefined(vars.bounds.maxY)) {
            bounds = vars.bounds;
            self.minX = minX = bounds.minX;
            self.minY = minY = bounds.minY;
            self.maxX = maxX = bounds.maxX;
            self.maxY = maxY = bounds.maxY;
          } else {
            targetBounds = _getBounds(target, target.parentNode);
            self.minX = minX = Math.round(getPropAsNum(xProp, "px") + bounds.left - targetBounds.left);
            self.minY = minY = Math.round(getPropAsNum(yProp, "px") + bounds.top - targetBounds.top);
            self.maxX = maxX = Math.round(minX + (bounds.width - targetBounds.width));
            self.maxY = maxY = Math.round(minY + (bounds.height - targetBounds.height));
          }

          if (minX > maxX) {
            self.minX = maxX;
            self.maxX = maxX = minX;
            minX = self.minX;
          }

          if (minY > maxY) {
            self.minY = maxY;
            self.maxY = maxY = minY;
            minY = self.minY;
          }

          if (rotationMode) {
            self.minRotation = minX;
            self.maxRotation = maxX;
          }

          hasBounds = true;
        }

        if (vars.liveSnap) {
          snap = vars.liveSnap === true ? vars.snap || {} : vars.liveSnap;
          snapIsRaw = _isArray(snap) || _isFunction(snap);

          if (rotationMode) {
            snapX = buildSnapFunc(snapIsRaw ? snap : snap.rotation, minX, maxX, 1);
            snapY = null;
          } else {
            if (snap.points) {
              snapXY = buildPointSnapFunc(snapIsRaw ? snap : snap.points, minX, maxX, minY, maxY, snap.radius, scrollProxy ? -1 : 1);
            } else {
              if (allowX) {
                snapX = buildSnapFunc(snapIsRaw ? snap : snap.x || snap.left || snap.scrollLeft, minX, maxX, scrollProxy ? -1 : 1);
              }

              if (allowY) {
                snapY = buildSnapFunc(snapIsRaw ? snap : snap.y || snap.top || snap.scrollTop, minY, maxY, scrollProxy ? -1 : 1);
              }
            }
          }
        }
      },
          onThrowComplete = function onThrowComplete() {
        self.isThrowing = false;

        _dispatchEvent(self, "throwcomplete", "onThrowComplete");
      },
          onThrowInterrupt = function onThrowInterrupt() {
        self.isThrowing = false;
      },
          animate = function animate(inertia, forceZeroVelocity) {
        var snap, snapIsRaw, tween, overshootTolerance;

        if (inertia && InertiaPlugin) {
          if (inertia === true) {
            snap = vars.snap || vars.liveSnap || {};
            snapIsRaw = _isArray(snap) || _isFunction(snap);
            inertia = {
              resistance: (vars.throwResistance || vars.resistance || 1000) / (rotationMode ? 10 : 1)
            };

            if (rotationMode) {
              inertia.rotation = _parseInertia(self, snapIsRaw ? snap : snap.rotation, maxX, minX, 1, forceZeroVelocity);
            } else {
              if (allowX) {
                inertia[xProp] = _parseInertia(self, snapIsRaw ? snap : snap.points || snap.x || snap.left, maxX, minX, scrollProxy ? -1 : 1, forceZeroVelocity || self.lockedAxis === "x");
              }

              if (allowY) {
                inertia[yProp] = _parseInertia(self, snapIsRaw ? snap : snap.points || snap.y || snap.top, maxY, minY, scrollProxy ? -1 : 1, forceZeroVelocity || self.lockedAxis === "y");
              }

              if (snap.points || _isArray(snap) && _isObject(snap[0])) {
                inertia.linkedProps = xProp + "," + yProp;
                inertia.radius = snap.radius;
              }
            }
          }

          self.isThrowing = true;
          overshootTolerance = !isNaN(vars.overshootTolerance) ? vars.overshootTolerance : vars.edgeResistance === 1 ? 0 : 1 - self.edgeResistance + 0.2;

          if (!inertia.duration) {
            inertia.duration = {
              max: Math.max(vars.minDuration || 0, "maxDuration" in vars ? vars.maxDuration : 2),
              min: !isNaN(vars.minDuration) ? vars.minDuration : overshootTolerance === 0 || _isObject(inertia) && inertia.resistance > 1000 ? 0 : 0.5,
              overshoot: overshootTolerance
            };
          }

          self.tween = tween = gsap.to(scrollProxy || target, {
            inertia: inertia,
            data: "_draggable",
            inherit: false,
            onComplete: onThrowComplete,
            onInterrupt: onThrowInterrupt,
            onUpdate: vars.fastMode ? _dispatchEvent : syncXY,
            onUpdateParams: vars.fastMode ? [self, "onthrowupdate", "onThrowUpdate"] : snap && snap.radius ? [false, true] : []
          });

          if (!vars.fastMode) {
            if (scrollProxy) {
              scrollProxy._skip = true;
            }

            tween.render(1e9, true, true);
            syncXY(true, true);
            self.endX = self.x;
            self.endY = self.y;

            if (rotationMode) {
              self.endRotation = self.x;
            }

            tween.play(0);
            syncXY(true, true);

            if (scrollProxy) {
              scrollProxy._skip = false;
            }
          }
        } else if (hasBounds) {
          self.applyBounds();
        }
      },
          updateMatrix = function updateMatrix(shiftStart) {
        var start = matrix,
            p;
        matrix = getGlobalMatrix(target.parentNode, true);

        if (shiftStart && self.isPressed && !matrix.equals(start || new Matrix2D())) {
          p = start.inverse().apply({
            x: startPointerX,
            y: startPointerY
          });
          matrix.apply(p, p);
          startPointerX = p.x;
          startPointerY = p.y;
        }

        if (matrix.equals(_identityMatrix$1)) {
          matrix = null;
        }
      },
          recordStartPositions = function recordStartPositions() {
        var edgeTolerance = 1 - self.edgeResistance,
            offsetX = isFixed ? _getDocScrollLeft$1(ownerDoc) : 0,
            offsetY = isFixed ? _getDocScrollTop$1(ownerDoc) : 0,
            parsedOrigin,
            x,
            y;

        if (xyMode) {
          gsCache.x = getPropAsNum(xProp, "px") + "px";
          gsCache.y = getPropAsNum(yProp, "px") + "px";
          gsCache.renderTransform();
        }

        updateMatrix(false);
        _point1.x = self.pointerX - offsetX;
        _point1.y = self.pointerY - offsetY;
        matrix && matrix.apply(_point1, _point1);
        startPointerX = _point1.x;
        startPointerY = _point1.y;

        if (dirty) {
          setPointerPosition(self.pointerX, self.pointerY);
          render(true);
        }

        innerMatrix = getGlobalMatrix(target);

        if (scrollProxy) {
          calculateBounds();
          startElementY = scrollProxy.top();
          startElementX = scrollProxy.left();
        } else {
          if (isTweening()) {
            syncXY(true, true);
            calculateBounds();
          } else {
            self.applyBounds();
          }

          if (rotationMode) {
            parsedOrigin = target.ownerSVGElement ? [gsCache.xOrigin - target.getBBox().x, gsCache.yOrigin - target.getBBox().y] : (_getComputedStyle(target)[_transformOriginProp$1] || "0 0").split(" ");
            rotationOrigin = self.rotationOrigin = getGlobalMatrix(target).apply({
              x: parseFloat(parsedOrigin[0]) || 0,
              y: parseFloat(parsedOrigin[1]) || 0
            });
            syncXY(true, true);
            x = self.pointerX - rotationOrigin.x - offsetX;
            y = rotationOrigin.y - self.pointerY + offsetY;
            startElementX = self.x;
            startElementY = self.y = Math.atan2(y, x) * _RAD2DEG;
          } else {
            startElementY = getPropAsNum(yProp, "px");
            startElementX = getPropAsNum(xProp, "px");
          }
        }

        if (hasBounds && edgeTolerance) {
          if (startElementX > maxX) {
            startElementX = maxX + (startElementX - maxX) / edgeTolerance;
          } else if (startElementX < minX) {
            startElementX = minX - (minX - startElementX) / edgeTolerance;
          }

          if (!rotationMode) {
            if (startElementY > maxY) {
              startElementY = maxY + (startElementY - maxY) / edgeTolerance;
            } else if (startElementY < minY) {
              startElementY = minY - (minY - startElementY) / edgeTolerance;
            }
          }
        }

        self.startX = startElementX = _round(startElementX);
        self.startY = startElementY = _round(startElementY);
      },
          isTweening = function isTweening() {
        return self.tween && self.tween.isActive();
      },
          removePlaceholder = function removePlaceholder() {
        if (_placeholderDiv.parentNode && !isTweening() && !self.isDragging) {
          _placeholderDiv.parentNode.removeChild(_placeholderDiv);
        }
      },
          onPress = function onPress(e, force) {
        var i;

        if (!enabled || self.isPressed || !e || (e.type === "mousedown" || e.type === "pointerdown") && !force && _getTime() - clickTime < 30 && _touchEventLookup[self.pointerEvent.type]) {
          isPreventingDefault && e && enabled && _preventDefault(e);
          return;
        }

        interrupted = isTweening();
        dragged = false;
        self.pointerEvent = e;

        if (_touchEventLookup[e.type]) {
          touchEventTarget = ~e.type.indexOf("touch") ? e.currentTarget || e.target : ownerDoc;

          _addListener(touchEventTarget, "touchend", onRelease);

          _addListener(touchEventTarget, "touchmove", onMove);

          _addListener(touchEventTarget, "touchcancel", onRelease);

          _addListener(ownerDoc, "touchstart", _onMultiTouchDocument);
        } else {
          touchEventTarget = null;

          _addListener(ownerDoc, "mousemove", onMove);
        }

        touchDragAxis = null;

        if (!_supportsPointer || !touchEventTarget) {
          _addListener(ownerDoc, "mouseup", onRelease);

          e && e.target && _addListener(e.target, "mouseup", onRelease);
        }

        isClicking = isClickable.call(self, e.target) && vars.dragClickables === false && !force;

        if (isClicking) {
          _addListener(e.target, "change", onRelease);

          _dispatchEvent(self, "pressInit", "onPressInit");

          _dispatchEvent(self, "press", "onPress");

          _setSelectable(triggers, true);

          isPreventingDefault = false;
          return;
        }

        allowNativeTouchScrolling = !touchEventTarget || allowX === allowY || self.vars.allowNativeTouchScrolling === false || self.vars.allowContextMenu && e && (e.ctrlKey || e.which > 2) ? false : allowX ? "y" : "x";
        isPreventingDefault = !allowNativeTouchScrolling && !self.allowEventDefault;

        if (isPreventingDefault) {
          _preventDefault(e);

          _addListener(_win$1, "touchforcechange", _preventDefault);
        }

        if (e.changedTouches) {
          e = touch = e.changedTouches[0];
          touchID = e.identifier;
        } else if (e.pointerId) {
          touchID = e.pointerId;
        } else {
          touch = touchID = null;
        }

        _dragCount++;

        _addToRenderQueue(render);

        startPointerY = self.pointerY = e.pageY;
        startPointerX = self.pointerX = e.pageX;

        _dispatchEvent(self, "pressInit", "onPressInit");

        if (allowNativeTouchScrolling || self.autoScroll) {
          _recordMaxScrolls(target.parentNode);
        }

        if (target.parentNode && self.autoScroll && !scrollProxy && !rotationMode && target.parentNode._gsMaxScrollX && !_placeholderDiv.parentNode && !target.getBBox) {
          _placeholderDiv.style.width = target.parentNode.scrollWidth + "px";
          target.parentNode.appendChild(_placeholderDiv);
        }

        recordStartPositions();
        self.tween && self.tween.kill();
        self.isThrowing = false;
        gsap.killTweensOf(scrollProxy || target, killProps, true);
        scrollProxy && gsap.killTweensOf(target, {
          scrollTo: 1
        }, true);
        self.tween = self.lockedAxis = null;

        if (vars.zIndexBoost || !rotationMode && !scrollProxy && vars.zIndexBoost !== false) {
          target.style.zIndex = Draggable.zIndex++;
        }

        self.isPressed = true;
        hasDragCallback = !!(vars.onDrag || self._listeners.drag);
        hasMoveCallback = !!(vars.onMove || self._listeners.move);

        if (vars.cursor !== false || vars.activeCursor) {
          i = triggers.length;

          while (--i > -1) {
            gsap.set(triggers[i], {
              cursor: vars.activeCursor || vars.cursor || (_defaultCursor === "grab" ? "grabbing" : _defaultCursor)
            });
          }
        }

        _dispatchEvent(self, "press", "onPress");

        InertiaPlugin && InertiaPlugin.track(scrollProxy || target, xyMode ? "x,y" : rotationMode ? "rotation" : "top,left");
      },
          onMove = function onMove(e) {
        var originalEvent = e,
            touches,
            pointerX,
            pointerY,
            i,
            dx,
            dy;

        if (!enabled || _isMultiTouching || !self.isPressed || !e) {
          isPreventingDefault && e && enabled && _preventDefault(e);
          return;
        }

        self.pointerEvent = e;
        touches = e.changedTouches;

        if (touches) {
          e = touches[0];

          if (e !== touch && e.identifier !== touchID) {
            i = touches.length;

            while (--i > -1 && (e = touches[i]).identifier !== touchID && e.target !== target) {}

            if (i < 0) {
              return;
            }
          }
        } else if (e.pointerId && touchID && e.pointerId !== touchID) {
          return;
        }

        if (touchEventTarget && allowNativeTouchScrolling && !touchDragAxis) {
          _point1.x = e.pageX - (isFixed ? _getDocScrollLeft$1(ownerDoc) : 0);
          _point1.y = e.pageY - (isFixed ? _getDocScrollTop$1(ownerDoc) : 0);
          matrix && matrix.apply(_point1, _point1);
          pointerX = _point1.x;
          pointerY = _point1.y;
          dx = Math.abs(pointerX - startPointerX);
          dy = Math.abs(pointerY - startPointerY);

          if (dx !== dy && (dx > minimumMovement || dy > minimumMovement) || _isAndroid && allowNativeTouchScrolling === touchDragAxis) {
            touchDragAxis = dx > dy && allowX ? "x" : "y";

            if (allowNativeTouchScrolling && touchDragAxis !== allowNativeTouchScrolling) {
              _addListener(_win$1, "touchforcechange", _preventDefault);
            }

            if (self.vars.lockAxisOnTouchScroll !== false && allowX && allowY) {
              self.lockedAxis = touchDragAxis === "x" ? "y" : "x";
              _isFunction(self.vars.onLockAxis) && self.vars.onLockAxis.call(self, originalEvent);
            }

            if (_isAndroid && allowNativeTouchScrolling === touchDragAxis) {
              onRelease(originalEvent);
              return;
            }
          }
        }

        if (!self.allowEventDefault && (!allowNativeTouchScrolling || touchDragAxis && allowNativeTouchScrolling !== touchDragAxis) && originalEvent.cancelable !== false) {
          _preventDefault(originalEvent);

          isPreventingDefault = true;
        } else if (isPreventingDefault) {
          isPreventingDefault = false;
        }

        if (self.autoScroll) {
          checkAutoScrollBounds = true;
        }

        setPointerPosition(e.pageX, e.pageY, hasMoveCallback);
      },
          setPointerPosition = function setPointerPosition(pointerX, pointerY, invokeOnMove) {
        var dragTolerance = 1 - self.dragResistance,
            edgeTolerance = 1 - self.edgeResistance,
            prevPointerX = self.pointerX,
            prevPointerY = self.pointerY,
            prevStartElementY = startElementY,
            prevX = self.x,
            prevY = self.y,
            prevEndX = self.endX,
            prevEndY = self.endY,
            prevEndRotation = self.endRotation,
            prevDirty = dirty,
            xChange,
            yChange,
            x,
            y,
            dif,
            temp;
        self.pointerX = pointerX;
        self.pointerY = pointerY;

        if (isFixed) {
          pointerX -= _getDocScrollLeft$1(ownerDoc);
          pointerY -= _getDocScrollTop$1(ownerDoc);
        }

        if (rotationMode) {
          y = _round(Math.atan2(rotationOrigin.y - pointerY, pointerX - rotationOrigin.x) * _RAD2DEG);
          dif = self.y - y;

          if (dif > 180) {
            startElementY -= 360;
            self.y = y;
          } else if (dif < -180) {
            startElementY += 360;
            self.y = y;
          }

          if (matrix) {
            temp = pointerX * matrix.a + pointerY * matrix.c + matrix.e;
            pointerY = pointerX * matrix.b + pointerY * matrix.d + matrix.f;
            pointerX = temp;
          }

          if (self.x !== startElementX || Math.max(Math.abs(startPointerX - pointerX), Math.abs(startPointerY - pointerY)) > minimumMovement) {
            self.y = y;
            x = _round(startElementX + (startElementY - y) * dragTolerance);
          } else {
            x = startElementX;
          }
        } else {
          if (matrix) {
            temp = pointerX * matrix.a + pointerY * matrix.c + matrix.e;
            pointerY = pointerX * matrix.b + pointerY * matrix.d + matrix.f;
            pointerX = temp;
          }

          yChange = pointerY - startPointerY;
          xChange = pointerX - startPointerX;

          if (yChange < minimumMovement && yChange > -minimumMovement) {
            yChange = 0;
          }

          if (xChange < minimumMovement && xChange > -minimumMovement) {
            xChange = 0;
          }

          if ((self.lockAxis || self.lockedAxis) && (xChange || yChange)) {
            temp = self.lockedAxis;

            if (!temp) {
              self.lockedAxis = temp = allowX && Math.abs(xChange) > Math.abs(yChange) ? "y" : allowY ? "x" : null;

              if (temp && _isFunction(self.vars.onLockAxis)) {
                self.vars.onLockAxis.call(self, self.pointerEvent);
              }
            }

            if (temp === "y") {
              yChange = 0;
            } else if (temp === "x") {
              xChange = 0;
            }
          }

          x = _round(startElementX + xChange * dragTolerance);
          y = _round(startElementY + yChange * dragTolerance);
        }

        if ((snapX || snapY || snapXY) && (self.x !== x || self.y !== y && !rotationMode)) {
          if (snapXY) {
            _temp1.x = x;
            _temp1.y = y;
            temp = snapXY(_temp1);
            x = _round(temp.x);
            y = _round(temp.y);
          }

          if (snapX) {
            x = _round(snapX(x));
          }

          if (snapY) {
            y = _round(snapY(y));
          }
        }

        if (hasBounds) {
          if (x > maxX) {
            x = maxX + Math.round((x - maxX) * edgeTolerance);
          } else if (x < minX) {
            x = minX + Math.round((x - minX) * edgeTolerance);
          }

          if (!rotationMode) {
            if (y > maxY) {
              y = Math.round(maxY + (y - maxY) * edgeTolerance);
            } else if (y < minY) {
              y = Math.round(minY + (y - minY) * edgeTolerance);
            }
          }
        }

        if (self.x !== x || self.y !== y && !rotationMode) {
          if (rotationMode) {
            self.endRotation = self.x = self.endX = _round(x);
            dirty = true;
          } else {
            if (allowY) {
              self.y = self.endY = y;
              dirty = true;
            }

            if (allowX) {
              self.x = self.endX = x;
              dirty = true;
            }
          }

          if (!invokeOnMove || _dispatchEvent(self, "move", "onMove") !== false) {
            if (!self.isDragging && self.isPressed) {
              self.isDragging = dragged = true;

              _dispatchEvent(self, "dragstart", "onDragStart");
            }
          } else {
            self.pointerX = prevPointerX;
            self.pointerY = prevPointerY;
            startElementY = prevStartElementY;
            self.x = prevX;
            self.y = prevY;
            self.endX = prevEndX;
            self.endY = prevEndY;
            self.endRotation = prevEndRotation;
            dirty = prevDirty;
          }
        }
      },
          onRelease = function onRelease(e, force) {
        if (!enabled || !self.isPressed || e && touchID != null && !force && (e.pointerId && e.pointerId !== touchID && e.target !== target || e.changedTouches && !_hasTouchID(e.changedTouches, touchID))) {
          isPreventingDefault && e && enabled && _preventDefault(e);
          return;
        }

        self.isPressed = false;
        var originalEvent = e,
            wasDragging = self.isDragging,
            isContextMenuRelease = self.vars.allowContextMenu && e && (e.ctrlKey || e.which > 2),
            placeholderDelayedCall = gsap.delayedCall(0.001, removePlaceholder),
            touches,
            i,
            syntheticEvent,
            eventTarget,
            syntheticClick;

        if (touchEventTarget) {
          _removeListener(touchEventTarget, "touchend", onRelease);

          _removeListener(touchEventTarget, "touchmove", onMove);

          _removeListener(touchEventTarget, "touchcancel", onRelease);

          _removeListener(ownerDoc, "touchstart", _onMultiTouchDocument);
        } else {
          _removeListener(ownerDoc, "mousemove", onMove);
        }

        _removeListener(_win$1, "touchforcechange", _preventDefault);

        if (!_supportsPointer || !touchEventTarget) {
          _removeListener(ownerDoc, "mouseup", onRelease);

          e && e.target && _removeListener(e.target, "mouseup", onRelease);
        }

        dirty = false;

        if (wasDragging) {
          dragEndTime = _lastDragTime = _getTime();
          self.isDragging = false;
        }

        _removeFromRenderQueue(render);

        if (isClicking && !isContextMenuRelease) {
          if (e) {
            _removeListener(e.target, "change", onRelease);

            self.pointerEvent = originalEvent;
          }

          _setSelectable(triggers, false);

          _dispatchEvent(self, "release", "onRelease");

          _dispatchEvent(self, "click", "onClick");

          isClicking = false;
          return;
        }

        i = triggers.length;

        while (--i > -1) {
          _setStyle(triggers[i], "cursor", vars.cursor || (vars.cursor !== false ? _defaultCursor : null));
        }

        _dragCount--;

        if (e) {
          touches = e.changedTouches;

          if (touches) {
            e = touches[0];

            if (e !== touch && e.identifier !== touchID) {
              i = touches.length;

              while (--i > -1 && (e = touches[i]).identifier !== touchID && e.target !== target) {}

              if (i < 0 && !force) {
                return;
              }
            }
          }

          self.pointerEvent = originalEvent;
          self.pointerX = e.pageX;
          self.pointerY = e.pageY;
        }

        if (isContextMenuRelease && originalEvent) {
          _preventDefault(originalEvent);

          isPreventingDefault = true;

          _dispatchEvent(self, "release", "onRelease");
        } else if (originalEvent && !wasDragging) {
          isPreventingDefault = false;

          if (interrupted && (vars.snap || vars.bounds)) {
            animate(vars.inertia || vars.throwProps);
          }

          _dispatchEvent(self, "release", "onRelease");

          if ((!_isAndroid || originalEvent.type !== "touchmove") && originalEvent.type.indexOf("cancel") === -1) {
            _dispatchEvent(self, "click", "onClick");

            if (_getTime() - clickTime < 300) {
              _dispatchEvent(self, "doubleclick", "onDoubleClick");
            }

            eventTarget = originalEvent.target || target;
            clickTime = _getTime();

            syntheticClick = function syntheticClick() {
              if (clickTime !== clickDispatch && self.enabled() && !self.isPressed && !originalEvent.defaultPrevented) {
                if (eventTarget.click) {
                  eventTarget.click();
                } else if (ownerDoc.createEvent) {
                  syntheticEvent = ownerDoc.createEvent("MouseEvents");
                  syntheticEvent.initMouseEvent("click", true, true, _win$1, 1, self.pointerEvent.screenX, self.pointerEvent.screenY, self.pointerX, self.pointerY, false, false, false, false, 0, null);
                  eventTarget.dispatchEvent(syntheticEvent);
                }
              }
            };

            if (!_isAndroid && !originalEvent.defaultPrevented) {
              gsap.delayedCall(0.05, syntheticClick);
            }
          }
        } else {
          animate(vars.inertia || vars.throwProps);

          if (!self.allowEventDefault && originalEvent && (vars.dragClickables !== false || !isClickable.call(self, originalEvent.target)) && wasDragging && (!allowNativeTouchScrolling || touchDragAxis && allowNativeTouchScrolling === touchDragAxis) && originalEvent.cancelable !== false) {
            isPreventingDefault = true;

            _preventDefault(originalEvent);
          } else {
            isPreventingDefault = false;
          }

          _dispatchEvent(self, "release", "onRelease");
        }

        isTweening() && placeholderDelayedCall.duration(self.tween.duration());
        wasDragging && _dispatchEvent(self, "dragend", "onDragEnd");
        return true;
      },
          updateScroll = function updateScroll(e) {
        if (e && self.isDragging && !scrollProxy) {
          var parent = e.target || target.parentNode,
              deltaX = parent.scrollLeft - parent._gsScrollX,
              deltaY = parent.scrollTop - parent._gsScrollY;

          if (deltaX || deltaY) {
            if (matrix) {
              startPointerX -= deltaX * matrix.a + deltaY * matrix.c;
              startPointerY -= deltaY * matrix.d + deltaX * matrix.b;
            } else {
              startPointerX -= deltaX;
              startPointerY -= deltaY;
            }

            parent._gsScrollX += deltaX;
            parent._gsScrollY += deltaY;
            setPointerPosition(self.pointerX, self.pointerY);
          }
        }
      },
          onClick = function onClick(e) {
        var time = _getTime(),
            recentlyClicked = time - clickTime < 100,
            recentlyDragged = time - dragEndTime < 50,
            alreadyDispatched = recentlyClicked && clickDispatch === clickTime,
            defaultPrevented = self.pointerEvent && self.pointerEvent.defaultPrevented,
            alreadyDispatchedTrusted = recentlyClicked && trustedClickDispatch === clickTime,
            trusted = e.isTrusted || e.isTrusted == null && recentlyClicked && alreadyDispatched;

        if ((alreadyDispatched || recentlyDragged && self.vars.suppressClickOnDrag !== false) && e.stopImmediatePropagation) {
          e.stopImmediatePropagation();
        }

        if (recentlyClicked && !(self.pointerEvent && self.pointerEvent.defaultPrevented) && (!alreadyDispatched || trusted && !alreadyDispatchedTrusted)) {
          if (trusted && alreadyDispatched) {
            trustedClickDispatch = clickTime;
          }

          clickDispatch = clickTime;
          return;
        }

        if (self.isPressed || recentlyDragged || recentlyClicked) {
          if (!trusted || !e.detail || !recentlyClicked || defaultPrevented) {
            _preventDefault(e);
          }
        }

        if (!recentlyClicked && !recentlyDragged && !dragged) {
          e && e.target && (self.pointerEvent = e);

          _dispatchEvent(self, "click", "onClick");
        }
      },
          localizePoint = function localizePoint(p) {
        return matrix ? {
          x: p.x * matrix.a + p.y * matrix.c + matrix.e,
          y: p.x * matrix.b + p.y * matrix.d + matrix.f
        } : {
          x: p.x,
          y: p.y
        };
      };

      old = Draggable.get(target);
      old && old.kill();

      _this2.startDrag = function (event, align) {
        var r1, r2, p1, p2;
        onPress(event || self.pointerEvent, true);

        if (align && !self.hitTest(event || self.pointerEvent)) {
          r1 = _parseRect(event || self.pointerEvent);
          r2 = _parseRect(target);
          p1 = localizePoint({
            x: r1.left + r1.width / 2,
            y: r1.top + r1.height / 2
          });
          p2 = localizePoint({
            x: r2.left + r2.width / 2,
            y: r2.top + r2.height / 2
          });
          startPointerX -= p1.x - p2.x;
          startPointerY -= p1.y - p2.y;
        }

        if (!self.isDragging) {
          self.isDragging = dragged = true;

          _dispatchEvent(self, "dragstart", "onDragStart");
        }
      };

      _this2.drag = onMove;

      _this2.endDrag = function (e) {
        return onRelease(e || self.pointerEvent, true);
      };

      _this2.timeSinceDrag = function () {
        return self.isDragging ? 0 : (_getTime() - dragEndTime) / 1000;
      };

      _this2.timeSinceClick = function () {
        return (_getTime() - clickTime) / 1000;
      };

      _this2.hitTest = function (target, threshold) {
        return Draggable.hitTest(self.target, target, threshold);
      };

      _this2.getDirection = function (from, diagonalThreshold) {
        var mode = from === "velocity" && InertiaPlugin ? from : _isObject(from) && !rotationMode ? "element" : "start",
            xChange,
            yChange,
            ratio,
            direction,
            r1,
            r2;

        if (mode === "element") {
          r1 = _parseRect(self.target);
          r2 = _parseRect(from);
        }

        xChange = mode === "start" ? self.x - startElementX : mode === "velocity" ? InertiaPlugin.getVelocity(target, xProp) : r1.left + r1.width / 2 - (r2.left + r2.width / 2);

        if (rotationMode) {
          return xChange < 0 ? "counter-clockwise" : "clockwise";
        } else {
          diagonalThreshold = diagonalThreshold || 2;
          yChange = mode === "start" ? self.y - startElementY : mode === "velocity" ? InertiaPlugin.getVelocity(target, yProp) : r1.top + r1.height / 2 - (r2.top + r2.height / 2);
          ratio = Math.abs(xChange / yChange);
          direction = ratio < 1 / diagonalThreshold ? "" : xChange < 0 ? "left" : "right";

          if (ratio < diagonalThreshold) {
            if (direction !== "") {
              direction += "-";
            }

            direction += yChange < 0 ? "up" : "down";
          }
        }

        return direction;
      };

      _this2.applyBounds = function (newBounds, sticky) {
        var x, y, forceZeroVelocity, e, parent, isRoot;

        if (newBounds && vars.bounds !== newBounds) {
          vars.bounds = newBounds;
          return self.update(true, sticky);
        }

        syncXY(true);
        calculateBounds();

        if (hasBounds && !isTweening()) {
          x = self.x;
          y = self.y;

          if (x > maxX) {
            x = maxX;
          } else if (x < minX) {
            x = minX;
          }

          if (y > maxY) {
            y = maxY;
          } else if (y < minY) {
            y = minY;
          }

          if (self.x !== x || self.y !== y) {
            forceZeroVelocity = true;
            self.x = self.endX = x;

            if (rotationMode) {
              self.endRotation = x;
            } else {
              self.y = self.endY = y;
            }

            dirty = true;
            render(true);

            if (self.autoScroll && !self.isDragging) {
              _recordMaxScrolls(target.parentNode);

              e = target;
              _windowProxy.scrollTop = _win$1.pageYOffset != null ? _win$1.pageYOffset : ownerDoc.documentElement.scrollTop != null ? ownerDoc.documentElement.scrollTop : ownerDoc.body.scrollTop;
              _windowProxy.scrollLeft = _win$1.pageXOffset != null ? _win$1.pageXOffset : ownerDoc.documentElement.scrollLeft != null ? ownerDoc.documentElement.scrollLeft : ownerDoc.body.scrollLeft;

              while (e && !isRoot) {
                isRoot = _isRoot(e.parentNode);
                parent = isRoot ? _windowProxy : e.parentNode;

                if (allowY && parent.scrollTop > parent._gsMaxScrollY) {
                  parent.scrollTop = parent._gsMaxScrollY;
                }

                if (allowX && parent.scrollLeft > parent._gsMaxScrollX) {
                  parent.scrollLeft = parent._gsMaxScrollX;
                }

                e = parent;
              }
            }
          }

          if (self.isThrowing && (forceZeroVelocity || self.endX > maxX || self.endX < minX || self.endY > maxY || self.endY < minY)) {
            animate(vars.inertia || vars.throwProps, forceZeroVelocity);
          }
        }

        return self;
      };

      _this2.update = function (applyBounds, sticky, ignoreExternalChanges) {
        if (sticky && self.isPressed) {
          if (rotationMode) {
            self.x = self.y = _round(parseFloat(gsCache.rotation));
          } else {
            var m = getGlobalMatrix(target),
                p = innerMatrix.apply({
              x: self.x - startElementX,
              y: self.y - startElementY
            }),
                m2 = getGlobalMatrix(target.parentNode, true);
            m2.apply({
              x: m.e - p.x,
              y: m.f - p.y
            }, p);
            self.x = _round(self.x - (p.x - m2.e));
            self.y = _round(self.y - (p.y - m2.f));
          }

          render(true);
          recordStartPositions();
        }

        var x = self.x,
            y = self.y;
        updateMatrix(!sticky);

        if (applyBounds) {
          self.applyBounds();
        } else {
          dirty && ignoreExternalChanges && render(true);
          syncXY(true);
        }

        if (sticky) {
          setPointerPosition(self.pointerX, self.pointerY);
          dirty && render(true);
        }

        if (self.isPressed && !sticky && (allowX && Math.abs(x - self.x) > 0.01 || allowY && Math.abs(y - self.y) > 0.01 && !rotationMode)) {
          recordStartPositions();
        }

        if (self.autoScroll) {
          _recordMaxScrolls(target.parentNode, self.isDragging);

          checkAutoScrollBounds = self.isDragging;
          render(true);

          _removeScrollListener(target, updateScroll);

          _addScrollListener(target, updateScroll);
        }

        return self;
      };

      _this2.enable = function (type) {
        var setVars = {
          lazy: true
        },
            id,
            i,
            trigger;

        if (vars.cursor !== false) {
          setVars.cursor = vars.cursor || _defaultCursor;
        }

        if (gsap.utils.checkPrefix("touchCallout")) {
          setVars.touchCallout = "none";
        }

        if (type !== "soft") {
          _setTouchActionForAllDescendants(triggers, allowX === allowY ? "none" : vars.allowNativeTouchScrolling && target.scrollHeight === target.clientHeight === (target.scrollWidth === target.clientHeight) || vars.allowEventDefault ? "manipulation" : allowX ? "pan-y" : "pan-x");

          i = triggers.length;

          while (--i > -1) {
            trigger = triggers[i];
            _supportsPointer || _addListener(trigger, "mousedown", onPress);

            _addListener(trigger, "touchstart", onPress);

            _addListener(trigger, "click", onClick, true);

            gsap.set(trigger, setVars);

            if (trigger.getBBox && trigger.ownerSVGElement && allowX !== allowY) {
              gsap.set(trigger.ownerSVGElement, {
                touchAction: vars.allowNativeTouchScrolling || vars.allowEventDefault ? "manipulation" : allowX ? "pan-y" : "pan-x"
              });
            }

            vars.allowContextMenu || _addListener(trigger, "contextmenu", onContextMenu);
          }

          _setSelectable(triggers, false);
        }

        _addScrollListener(target, updateScroll);

        enabled = true;

        if (InertiaPlugin && type !== "soft") {
          InertiaPlugin.track(scrollProxy || target, xyMode ? "x,y" : rotationMode ? "rotation" : "top,left");
        }

        target._gsDragID = id = target._gsDragID || "d" + _lookupCount++;
        _lookup[id] = self;

        if (scrollProxy) {
          scrollProxy.enable();
          scrollProxy.element._gsDragID = id;
        }

        (vars.bounds || rotationMode) && recordStartPositions();
        vars.bounds && self.applyBounds();
        return self;
      };

      _this2.disable = function (type) {
        var dragging = self.isDragging,
            i = triggers.length,
            trigger;

        while (--i > -1) {
          _setStyle(triggers[i], "cursor", null);
        }

        if (type !== "soft") {
          _setTouchActionForAllDescendants(triggers, null);

          i = triggers.length;

          while (--i > -1) {
            trigger = triggers[i];

            _setStyle(trigger, "touchCallout", null);

            _removeListener(trigger, "mousedown", onPress);

            _removeListener(trigger, "touchstart", onPress);

            _removeListener(trigger, "click", onClick, true);

            _removeListener(trigger, "contextmenu", onContextMenu);
          }

          _setSelectable(triggers, true);

          if (touchEventTarget) {
            _removeListener(touchEventTarget, "touchcancel", onRelease);

            _removeListener(touchEventTarget, "touchend", onRelease);

            _removeListener(touchEventTarget, "touchmove", onMove);
          }

          _removeListener(ownerDoc, "mouseup", onRelease);

          _removeListener(ownerDoc, "mousemove", onMove);
        }

        _removeScrollListener(target, updateScroll);

        enabled = false;

        if (InertiaPlugin && type !== "soft") {
          InertiaPlugin.untrack(scrollProxy || target, xyMode ? "x,y" : rotationMode ? "rotation" : "top,left");
          self.tween && self.tween.kill();
        }

        scrollProxy && scrollProxy.disable();

        _removeFromRenderQueue(render);

        self.isDragging = self.isPressed = isClicking = false;
        dragging && _dispatchEvent(self, "dragend", "onDragEnd");
        return self;
      };

      _this2.enabled = function (value, type) {
        return arguments.length ? value ? self.enable(type) : self.disable(type) : enabled;
      };

      _this2.kill = function () {
        self.isThrowing = false;
        self.tween && self.tween.kill();
        self.disable();
        gsap.set(triggers, {
          clearProps: "userSelect"
        });
        delete _lookup[target._gsDragID];
        return self;
      };

      _this2.revert = function () {
        this.kill();
        this.styles && this.styles.revert();
      };

      if (~type.indexOf("scroll")) {
        scrollProxy = _this2.scrollProxy = new ScrollProxy(target, _extend({
          onKill: function onKill() {
            self.isPressed && onRelease(null);
          }
        }, vars));
        target.style.overflowY = allowY && !_isTouchDevice ? "auto" : "hidden";
        target.style.overflowX = allowX && !_isTouchDevice ? "auto" : "hidden";
        target = scrollProxy.content;
      }

      if (rotationMode) {
        killProps.rotation = 1;
      } else {
        if (allowX) {
          killProps[xProp] = 1;
        }

        if (allowY) {
          killProps[yProp] = 1;
        }
      }

      gsCache.force3D = "force3D" in vars ? vars.force3D : true;

      _context(_assertThisInitialized(_this2));

      _this2.enable();

      return _this2;
    }

    Draggable.register = function register(core) {
      gsap = core;

      _initCore();
    };

    Draggable.create = function create(targets, vars) {
      _coreInitted || _initCore(true);
      return _toArray(targets).map(function (target) {
        return new Draggable(target, vars);
      });
    };

    Draggable.get = function get(target) {
      return _lookup[(_toArray(target)[0] || {})._gsDragID];
    };

    Draggable.timeSinceDrag = function timeSinceDrag() {
      return (_getTime() - _lastDragTime) / 1000;
    };

    Draggable.hitTest = function hitTest(obj1, obj2, threshold) {
      if (obj1 === obj2) {
        return false;
      }

      var r1 = _parseRect(obj1),
          r2 = _parseRect(obj2),
          top = r1.top,
          left = r1.left,
          right = r1.right,
          bottom = r1.bottom,
          width = r1.width,
          height = r1.height,
          isOutside = r2.left > right || r2.right < left || r2.top > bottom || r2.bottom < top,
          overlap,
          area,
          isRatio;

      if (isOutside || !threshold) {
        return !isOutside;
      }

      isRatio = (threshold + "").indexOf("%") !== -1;
      threshold = parseFloat(threshold) || 0;
      overlap = {
        left: Math.max(left, r2.left),
        top: Math.max(top, r2.top)
      };
      overlap.width = Math.min(right, r2.right) - overlap.left;
      overlap.height = Math.min(bottom, r2.bottom) - overlap.top;

      if (overlap.width < 0 || overlap.height < 0) {
        return false;
      }

      if (isRatio) {
        threshold *= 0.01;
        area = overlap.width * overlap.height;
        return area >= width * height * threshold || area >= r2.width * r2.height * threshold;
      }

      return overlap.width > threshold && overlap.height > threshold;
    };

    return Draggable;
  }(EventDispatcher);

  _setDefaults(Draggable.prototype, {
    pointerX: 0,
    pointerY: 0,
    startX: 0,
    startY: 0,
    deltaX: 0,
    deltaY: 0,
    isDragging: false,
    isPressed: false
  });

  Draggable.zIndex = 1000;
  Draggable.version = "3.15.0";
  _getGSAP() && gsap.registerPlugin(Draggable);

  /*!
   * GSDevTools 3.15.0
   * https://gsap.com
   *
   * @license Copyright 2008-2026, GreenSock. All rights reserved.
   * Subject to the terms at https://gsap.com/standard-license
   * @author: Jack Doyle, jack@greensock.com
  */

  var gsap$1,
      _coreInitted$1,
      _doc$2,
      _docEl,
      _win$2,
      _recordedRoot,
      Animation,
      _rootTween,
      _rootInstance,
      _keyboardInstance,
      _globalTimeline,
      _independentRoot,
      _delayedCall,
      _context$1,
      _startupPhase = true,
      _globalStartTime = 0,
      _windowExists$1 = function _windowExists() {
    return typeof window !== "undefined";
  },
      _getGSAP$1 = function _getGSAP() {
    return gsap$1 || _windowExists$1() && (gsap$1 = window.gsap) && gsap$1.registerPlugin && gsap$1;
  },
      _isString = function _isString(value) {
    return typeof value === "string";
  },
      _isFunction$1 = function _isFunction(value) {
    return typeof value === "function";
  },
      _isObject$1 = function _isObject(value) {
    return typeof value === "object";
  },
      _isUndefined$1 = function _isUndefined(value) {
    return typeof value === "undefined";
  },
      _svgNS = "http://www.w3.org/2000/svg",
      _domNS = "http://www.w3.org/1999/xhtml",
      _idSeed = 0,
      _lookup$1 = {},
      _supportsStorage = function () {
    try {
      sessionStorage.setItem("gsTest", "1");
      sessionStorage.removeItem("gsTest");
      return true;
    } catch (e) {
      return false;
    }
  }(),
      _parseAnimation = function _parseAnimation(animationOrId) {
    return animationOrId instanceof Animation ? animationOrId : animationOrId ? gsap$1.getById(animationOrId) : null;
  },
      _createElement$1 = function _createElement(type, container, cssText) {
    var element = _doc$2.createElementNS ? _doc$2.createElementNS(type === "svg" ? _svgNS : _domNS, type) : _doc$2.createElement(type);

    if (container) {
      if (_isString(container)) {
        container = _doc$2.querySelector(container);
      }

      container.appendChild(element);
    }

    if (type === "svg") {
      element.setAttribute("xmlns", _svgNS);
      element.setAttribute("xmlns:xlink", _domNS);
    }

    cssText && (element.style.cssText = cssText);
    return element;
  },
      _clearSelection = function _clearSelection() {
    if (_doc$2.selection) {
      _doc$2.selection.empty();
    } else if (_win$2.getSelection) {
      _win$2.getSelection().removeAllRanges();
    }
  },
      _getChildrenOf = function _getChildrenOf(timeline, includeTimelines) {
    var a = [],
        cnt = 0,
        Tween = gsap$1.core.Tween,
        tween = timeline._first;

    while (tween) {
      if (tween instanceof Tween) {
        if (tween.vars.id) {
          a[cnt++] = tween;
        }
      } else {
        if (includeTimelines && tween.vars.id) {
          a[cnt++] = tween;
        }

        a = a.concat(_getChildrenOf(tween, includeTimelines));
        cnt = a.length;
      }

      tween = tween._next;
    }

    return a;
  },
      _getClippedDuration = function _getClippedDuration(animation, excludeRootRepeats) {
    var max = 0,
        repeat = Math.max(0, animation._repeat),
        t = animation._first;

    if (!t) {
      max = animation.duration();
    }

    while (t) {
      max = Math.max(max, t.totalDuration() > 999 ? t.endTime(false) : t._start + t._tDur / t._ts);
      t = t._next;
    }

    return !excludeRootRepeats && repeat ? max * (repeat + 1) + animation._rDelay * repeat : max;
  },
      _globalizeTime = function _globalizeTime(animation, rawTime) {
    var a = animation,
        time = arguments.length > 1 ? +rawTime : a.rawTime();

    while (a) {
      time = a._start + time / (a._ts || 1);
      a = a.parent;
    }

    return time;
  },
      _timeToProgress = function _timeToProgress(time, animation, defaultValue, relativeProgress) {
    var add, i, a;

    if (_isString(time)) {
      if (time.charAt(1) === "=") {
        add = parseInt(time.charAt(0) + "1", 10) * parseFloat(time.substr(2));

        if (add < 0 && relativeProgress === 0) {
          relativeProgress = 100;
        }

        time = relativeProgress / 100 * animation.duration() + add;
      } else if (isNaN(time) && animation.labels && animation.labels[time] !== -1) {
        time = animation.labels[time];
      } else if (animation === _recordedRoot) {
        i = time.indexOf("=");

        if (i > 0) {
          add = parseInt(time.charAt(i - 1) + "1", 10) * parseFloat(time.substr(i + 1));
          time = time.substr(0, i - 1);
        } else {
          add = 0;
        }

        a = gsap$1.getById(time);

        if (a) {
          time = _globalizeTime(a, defaultValue / 100 * a.duration()) + add;
        }
      }
    }

    time = isNaN(time) ? defaultValue : parseFloat(time);
    return Math.min(100, Math.max(0, time / animation.duration() * 100));
  },
      _addedCSS,
      _createRootElement = function _createRootElement(element, minimal, css) {
    if (!_addedCSS) {
      _createElement$1("style", _docEl).innerHTML = '.gs-dev-tools{height:51px;bottom:0;left:0;right:0;display:block;position:fixed;overflow:visible;padding:0;font-size:15px;font-family:-apple-system,BlinkMacSystemFont,avenir next,sans-serif;color:#bbbaa6}.gs-dev-tools *{box-sizing:content-box;visibility:visible}.gs-dev-tools .gs-top{position:relative;z-index:499}.gs-dev-tools .gs-bottom{display:flex;align-items:center;justify-content:space-between;gap:1rem;background-color:#0e100f;height:42px;position:relative}.gs-dev-tools .timeline{position:relative;height:8px;margin-left:15px;margin-right:15px;overflow:visible}.gs-dev-tools .progress-bar,.gs-dev-tools .timeline-track{height:8px;width:100%;position:absolute;top:0;left:0;}.gs-dev-tools .timeline-track{background-color:#222}.gs-dev-tools .progress-bar{background:linear-gradient(114.41deg,#0ae448 20.74%,#abff84 65.5%);height:8px;top:0;width:0;pointer-events:none}.gs-dev-tools .seek-bar{width:100%;position:absolute;height:24px;top:-12px;left:0;background-color:transparent}.gs-dev-tools .in-point,.gs-dev-tools .out-point{width:15px;height:26px;position:absolute;top:-18px}.gs-dev-tools .in-point-shape{fill:#0ae448;transform:translateX(1px)}.gs-dev-tools .out-point-shape{fill:#ff8709}.gs-dev-tools .in-point{transform:translateX(-100%)}.gs-dev-tools .out-point{left:100%}.gs-dev-tools .playhead{position:absolute;top:-5px;transform:translate(-50%,0);left:0;border-radius:50%;width:16px;height:16px;background:linear-gradient(114.41deg,#0ae448 20.74%,#abff84 65.5%)}.gs-dev-tools .gs-btn-white{fill:#fffce1}.gs-dev-tools .pause{opacity:0}.gs-dev-tools .select-animation{vertical-align:middle;position:relative;padding:6px 10px}.gs-dev-tools .select-animation-container{flex-grow:4;width:40%}.gs-dev-tools .select-arrow{display:inline-block;width:12px;height:7px;margin:0 7px;transform:translate(0,-2px)}.gs-dev-tools .select-arrow-shape{stroke:currentcolor;stroke-width:2px;fill:none}.gs-dev-tools .rewind{height:14px}.gs-dev-tools .ease-border,.gs-dev-tools .rewind-path{fill:currentColor}.gs-dev-tools .play-pause{width:18px;height:18px}.gs-dev-tools .ease{width:20px;height:20px;min-width:30px;display:none}.gs-dev-tools .ease-path{fill:none;stroke:#abff84;stroke-width:2px}.gs-dev-tools .time-scale{text-align:center;min-width:30px}.gs-dev-tools .loop{width:15px}.gs-dev-tools label span{text-decoration:none}.gs-dev-tools button:focus,.gs-dev-tools select:focus{outline:0}.gs-dev-tools label{position:relative;cursor:pointer}.gs-dev-tools label.locked{text-decoration:none;cursor:auto}.gs-dev-tools label input,.gs-dev-tools label select{position:absolute;left:0;top:0;z-index:1;font:inherit;font-size:inherit;line-height:inherit;height:100%;width:100%;color:#000!important;opacity:0;background:0 0;border:none;padding:0;margin:0;-webkit-appearance:none;-moz-appearance:none;appearance:none;cursor:pointer}.gs-dev-tools label input+.display{position:relative;z-index:2}.gs-dev-tools .gs-bottom-right{vertical-align:middle;display:flex;align-items:center;flex-grow:4;width:40%;justify-content:flex-end}.gs-dev-tools .time-container{margin:0 5px}.gs-dev-tools .logo{width:32px;height:32px;position:relative;top:2px;margin:0 12px}.gs-dev-tools .gs-hit-area{background-color:transparent;width:100%;height:100%;top:0;position:absolute}.gs-dev-tools.minimal{border-top: 1px #222222 solid;height:auto;display:flex;align-items:stretch}.gs-dev-tools.minimal .gs-top{order:2;flex-grow:4;background-color:#0e100f}.gs-dev-tools.minimal .gs-bottom{background-color:#0e100f;border-top:none}.gs-dev-tools.minimal .timeline{top:50%;transform:translate(0,-50%)}.gs-dev-tools.minimal .gs-bottom-right,.gs-dev-tools.minimal .in-point,.gs-dev-tools.minimal .out-point,.gs-dev-tools.minimal .rewind,.gs-dev-tools.minimal .select-animation-container{display:none}.gs-dev-tools.minimal .play-pause{width:20px;height:20px;padding:4px 6px;margin-left:14px}.gs-dev-tools.minimal .time-scale{min-width:26px}.gs-dev-tools.minimal .loop{width:18px;min-width:18px;display:none}@media only screen and (max-width:600px){.gs-dev-tools{height:auto;display:flex;align-items:stretch}.gs-dev-tools .gs-top{order:2;flex-grow:4;background-color:#0e100f;height:42px}.gs-dev-tools .gs-bottom{background-color:#0e100f;border-top:none}.gs-dev-tools .timeline{top:50%;transform:translate(0,-50%)}.gs-dev-tools .gs-bottom-right,.gs-dev-tools .in-point,.gs-dev-tools .out-point,.gs-dev-tools .rewind,.gs-dev-tools .select-animation-container{display:none}.gs-dev-tools .play-pause{width:18px;height:18px;padding:4px 6px;margin-left:14px}.gs-dev-tools .time-scale{min-width:26px}.gs-dev-tools .loop{width:18px;min-width:18px;display:none}.gs-dev-tools .progress-bar,.gs-dev-tools .timeline-track{right:0}}';
      _addedCSS = true;
    }

    if (_isString(element)) {
      element = _doc$2.querySelector(element);
    }

    var root = _createElement$1("div", element || _docEl.getElementsByTagName("body")[0] || _docEl);

    root.setAttribute("class", "gs-dev-tools" + (minimal ? " minimal" : ""));
    root.innerHTML = '<div class=gs-hit-area></div><div class=gs-top><div class=timeline><div class=timeline-track></div><div class=progress-bar></div><div class=seek-bar></div><svg class=in-point viewBox="0 0 15 26" xmlns=http://www.w3.org/2000/svg><path class=in-point-shape d="M0.5,2.283c0,-0.985 0.798,-1.783 1.783,-1.783c2.679,0 7.717,0 10.41,0c0.48,-0 0.939,0.19 1.278,0.529c0.339,0.339 0.529,0.798 0.529,1.277c-0,4.821 -0,17.897 0,21.968c0,0.253 -0.135,0.488 -0.354,0.615c-0.22,0.128 -0.49,0.128 -0.711,0.003c-2.653,-1.517 -9.526,-5.444 -12.016,-6.867c-0.568,-0.325 -0.919,-0.929 -0.919,-1.583c-0,-2.835 -0,-10.627 -0,-14.159Z" style="fill:#00ff52;fill-rule:nonzero;"/></svg><svg class=out-point viewBox="0 0 15 26" xmlns=http://www.w3.org/2000/svg><path class=out-point-shape d="M0.5,2.251c0,-0.465 0.184,-0.91 0.513,-1.238c0.328,-0.329 0.773,-0.513 1.238,-0.513c2.669,0 7.733,0 10.439,0c0.48,-0 0.94,0.191 1.28,0.53c0.339,0.34 0.53,0.8 0.53,1.28l0,14.17c-0,0.631 -0.338,1.213 -0.886,1.526c-2.44,1.395 -9.262,5.293 -11.977,6.845c-0.236,0.134 -0.524,0.133 -0.759,-0.003c-0.234,-0.136 -0.378,-0.386 -0.378,-0.657c0,-4.178 0,-17.198 0,-21.94Z" style="fill-rule:nonzero;"/></svg><div class=playhead></div></div></div><div class=gs-bottom><div class=select-animation-container><label class=select-animation><select class=animation-list><option>Global Timeline<option>myTimeline</select><nobr><span class="display animation-label">Global Timeline</span><svg class=select-arrow viewBox="0 0 12.05 6.73" xmlns=http://www.w3.org/2000/svg><polyline class=select-arrow-shape points="0.35 0.35 6.03 6.03 11.7 0.35"/></svg></nobr></label></div><svg class=rewind viewBox="0 0 12 15.38" xmlns=http://www.w3.org/2000/svg><path d=M0,.38H2v15H0Zm2,7,10,7.36V0Z class="gs-btn-white rewind-path"/></svg><svg class=play-pause viewBox="0 0 20.97 25.67" xmlns=http://www.w3.org/2000/svg><g class=play><path d="M8,4.88 C8,10.18 8,15.48 8,20.79 5.33,22.41 2.66,24.04 0,25.67 0,17.11 0,8.55 0,0 2.66,1.62 5.33,3.25 8,4.88" class="gs-btn-white play-1" style=stroke:#fffce1;stroke-width:.6px /><path d="M14.485,8.855 C16.64,10.18 18.8,11.5 20.97,12.83 16.64,15.48 12.32,18.13 8,20.79 8,15.48 8,10.18 8,4.88 10.16,6.2 12.32,7.53 14.48,8.85" class="gs-btn-white play-2" style=stroke:#fffce1;stroke-width:.6px /></g></svg> <svg class=loop viewBox="0 0 29 25.38" xmlns=http://www.w3.org/2000/svg fill="currentcolor"><path d=M27.44,5.44,20.19,0V3.06H9.06A9.31,9.31,0,0,0,0,12.41,9.74,9.74,0,0,0,.69,16l3.06-2.23a6,6,0,0,1-.12-1.22,5.49,5.49,0,0,1,5.43-5.5H20.19v3.81Z class=loop-path /><path d=M25.25,11.54a5.18,5.18,0,0,1,.12,1.12,5.41,5.41,0,0,1-5.43,5.41H9.19V14.5L1.94,19.94l7.25,5.44V22.06H19.94A9.2,9.2,0,0,0,29,12.84a9.42,9.42,0,0,0-.68-3.53Z class=loop-path /></svg> <svg class=ease viewBox="0 0 25.67 25.67" xmlns=http://www.w3.org/2000/svg><path d=M.48,25.12c1.74-3.57,4.28-12.6,8.8-10.7s4.75,1.43,6.5-1.11S19.89,1.19,25.2.55 class=ease-path /><path d=M24.67,1V24.67H1V1H24.67m1-1H0V25.67H25.67V0Z class=ease-border /></svg><label class=time-scale><select><option value=10>10x<option value=5>5x<option value=2>2x<option value=1 selected>1x<option value=0.5>0.5x<option value=0.25>0.25x<option value=0.1>0.1x</select><span class="display time-scale-label">1x</span></label><div class=gs-bottom-right><div class=time-container><span class=time>0.00</span> / <span class=duration>0.00</span></div><a href="https://gsap.com/docs/v3/Plugins/GSDevTools?source=GSDevTools" target=_blank title=Docs><svg class="logo" viewBox="0 0 1080 1080" fill="none" xmlns="http://www.w3.org/2000/svg"><path d="M793 518.654C793 518.654 793 518.829 793 518.934L770.197 618.386C768.967 624.012 763.38 628.135 756.915 628.135H729.404C727.366 628.135 725.539 629.498 724.977 631.455C699.573 718.082 665.175 777.628 619.745 813.376C581.095 843.813 533.451 858 469.819 858C412.618 858 374.074 839.514 341.362 803.032C298.145 754.809 280.261 675.869 291.083 580.75C310.618 402.184 402.745 222.01 580.217 222.01C634.185 221.521 676.559 238.26 706.039 271.667C737.204 306.995 753.05 360.216 753.121 429.86C753.015 436.185 747.78 441.287 741.491 441.287H611.488C606.885 441.287 602.774 437.129 602.844 432.551C601.79 384.397 587.56 360.915 559.311 360.915C509.488 360.915 480.079 428.917 464.479 466.622C442.694 519.283 431.627 576.452 433.805 633.412C434.824 659.935 439.075 697.22 464.127 712.666C486.333 726.364 518.026 717.279 537.21 702.113C556.394 686.947 571.819 660.703 578.319 636.766C579.233 633.447 579.303 630.861 578.425 629.708C577.511 628.52 574.981 628.24 573.049 628.24H539.67C536.086 628.24 532.537 626.598 530.394 623.942C528.602 621.705 527.899 618.945 528.532 616.219L551.37 516.592C552.494 511.49 557.097 507.647 562.754 506.948V506.703H781.827C782.354 506.703 782.881 506.703 783.373 506.808C789.065 507.542 793.07 512.853 792.965 518.654H793Z" fill="#0AE448"/></svg></a></div></div>';

    if (element) {
      root.style.position = "absolute";
      root.style.top = minimal ? "calc(100% - 42px)" : "calc(100% - 51px)";
    }

    if (css) {
      if (_isString(css)) {
        root.style.cssText = css;
      } else if (_isObject$1(css)) {
        css.data = "root";
        gsap$1.set(root, css).kill();
      }

      if (root.style.top) {
        root.style.bottom = "auto";
      }

      if (root.style.width) {
        gsap$1.set(root, {
          xPercent: -50,
          left: "50%",
          right: "auto",
          data: "root"
        }).kill();
      }
    }

    if (!minimal && root.offsetWidth < 600) {
      root.setAttribute("class", "gs-dev-tools minimal");

      if (element) {
        root.style.top = "calc(100% - 42px)";
      }
    }

    return root;
  },
      _clickedOnce = true,
      _addListener$1 = function _addListener(e, type, callback, capture) {
    var handler, altType;

    if (type === "mousedown" || type === "mouseup") {
      e.style.cursor = "pointer";
    }

    if (type === "mousedown") {
      altType = !_isUndefined$1(e.onpointerdown) ? "pointerdown" : !_isUndefined$1(e.ontouchstart) ? "touchstart" : null;

      if (altType) {
        handler = function handler(event) {
          if (event.target.nodeName.toLowerCase() !== "select" && event.type === altType) {
            event.stopPropagation();

            if (_clickedOnce) {
              event.preventDefault();
              callback.call(e, event);
            }
          } else if (event.type !== altType) {
            callback.call(e, event);
          }

          _clickedOnce = true;
        };

        e.addEventListener(altType, handler, capture);

        if (altType !== "pointerdown") {
          e.addEventListener(type, handler, capture);
        }

        return;
      }
    }

    e.addEventListener(type, callback, capture);
  },
      _removeListener$1 = function _removeListener(e, type, callback) {
    e.removeEventListener(type, callback);
    type = type !== "mousedown" ? null : !_isUndefined$1(e.onpointerdown) ? "pointerdown" : !_isUndefined$1(e.ontouchstart) ? "touchstart" : null;

    if (type) {
      e.removeEventListener(type, callback);
    }
  },
      _selectValue = function _selectValue(element, value, label, insertIfAbsent) {
    var options = element.options,
        i = options.length,
        option;
    value += "";

    while (--i > -1) {
      if (options[i].innerHTML === value || options[i].value === value) {
        element.selectedIndex = i;
        label.innerHTML = options[i].innerHTML;
        return options[i];
      }
    }

    if (insertIfAbsent) {
      option = _createElement$1("option", element);
      option.setAttribute("value", value);
      option.innerHTML = label.innerHTML = _isString(insertIfAbsent) ? insertIfAbsent : value;
      element.selectedIndex = options.length - 1;
    }
  },
      _shiftSelectedValue = function _shiftSelectedValue(element, amount, label) {
    var options = element.options,
        i = Math.min(options.length - 1, Math.max(0, element.selectedIndex + amount));
    element.selectedIndex = i;

    if (label) {
      label.innerHTML = options[i].innerHTML;
    }

    return options[i].value;
  },
      _merge = function _merge() {
    var t = _globalTimeline._first,
        duration,
        next,
        target;

    if (_rootInstance) {
      duration = _recordedRoot._dur;

      while (t) {
        next = t._next;
        target = t._targets && t._targets[0];

        if (!(_isFunction$1(target) && target === t.vars.onComplete && !t._dur) && !(target && target._gsIgnore)) {
          _recordedRoot.add(t, t._start - t._delay);
        }

        t = next;
      }

      return duration !== _recordedRoot.duration();
    }
  },
      _buildPlayPauseMorph = function _buildPlayPauseMorph(svg) {
    var tl = gsap$1.timeline({
      data: "root",
      parent: _independentRoot,
      onComplete: function onComplete() {
        return tl.kill();
      }
    }, _independentRoot._time);
    tl.to(svg.querySelector(".play-1"), {
      duration: 0.4,
      attr: {
        d: "M5.75,3.13 C5.75,9.79 5.75,16.46 5.75,23.13 4.08,23.13 2.41,23.13 0.75,23.13 0.75,16.46 0.75,9.79 0.75,3.12 2.41,3.12 4.08,3.12 5.75,3.12"
      },
      ease: "power2.inOut",
      rotation: 360,
      transformOrigin: "50% 50%"
    }).to(svg.querySelector(".play-2"), {
      duration: 0.4,
      attr: {
        d: "M16.38,3.13 C16.38,9.79 16.38,16.46 16.38,23.13 14.71,23.13 13.04,23.13 11.38,23.13 11.38,16.46 11.38,9.79 11.38,3.12 13.04,3.12 14.71,3.12 16.38,3.12"
      },
      ease: "power2.inOut",
      rotation: 360,
      transformOrigin: "50% 50%"
    }, 0.05);
    return tl;
  },
      _buildLoopAnimation = function _buildLoopAnimation(svg) {
    var tl = gsap$1.timeline({
      data: "root",
      id: "loop",
      parent: _independentRoot,
      paused: true,
      onComplete: function onComplete() {
        return tl.kill();
      }
    }, _independentRoot._time);
    tl.to(svg, {
      duration: 0.5,
      rotation: 360,
      ease: "power3.inOut",
      transformOrigin: "50% 50%"
    }).to(svg.querySelectorAll(".loop-path"), {
      duration: 0.5,
      fill: "#91e600",
      ease: "none"
    }, 0);
    return tl;
  },
      _getAnimationById = function _getAnimationById(id) {
    return gsap$1.getById(id) || _independentRoot.getById(id) || id === _recordedRoot.vars.id && _recordedRoot;
  },
      _initCore$1 = function _initCore(core) {
    gsap$1 = core || _getGSAP$1();

    if (!_coreInitted$1) {
      if (gsap$1 && _windowExists$1()) {
        _doc$2 = document;
        _docEl = _doc$2.documentElement;
        _win$2 = window;

        _context$1 = gsap$1.core.context || function () {};

        gsap$1.registerPlugin(Draggable);
        _globalTimeline = gsap$1.globalTimeline;
        _globalTimeline._sort = true;
        _globalTimeline.autoRemoveChildren = false;
        Animation = gsap$1.core.Animation;
        _independentRoot = gsap$1.timeline({
          data: "indy",
          autoRemoveChildren: true,
          smoothChildTiming: true
        });

        _independentRoot.kill();

        _independentRoot._dp = 0;

        _independentRoot.to({}, {
          duration: 1e12
        });

        _recordedRoot = gsap$1.timeline({
          data: "root",
          id: "Global Timeline",
          autoRemoveChildren: false,
          smoothChildTiming: true,
          parent: _independentRoot
        }, 0);
        _rootTween = gsap$1.to(_recordedRoot, {
          duration: 1,
          time: 1,
          ease: "none",
          data: "root",
          id: "_rootTween",
          paused: true,
          immediateRender: false,
          parent: _independentRoot
        }, 0);

        _globalTimeline.killTweensOf = function (targets, props, onlyActive) {
          _recordedRoot.killTweensOf(targets, props, onlyActive);

          _recordedRoot.killTweensOf.call(_globalTimeline, targets, props, onlyActive);
        };

        _independentRoot._start = gsap$1.ticker.time;
        gsap$1.ticker.add(function (time) {
          return _independentRoot.render(time - _independentRoot._start);
        });
        _globalTimeline._start += _globalTimeline._time;
        _recordedRoot._start = _globalTimeline._time = _globalTimeline._tTime = 0;

        _delayedCall = function _delayedCall(delay, callback, params, scope) {
          return gsap$1.to(callback, {
            delay: delay,
            duration: 0,
            onComplete: callback,
            onReverseComplete: callback,
            onCompleteParams: params,
            onReverseCompleteParams: params,
            callbackScope: scope,
            parent: _independentRoot
          }, _independentRoot._time);
        };

        _delayedCall(0.01, function () {
          return _rootInstance ? _rootInstance.update() : _merge();
        });

        _delayedCall(2, function () {
          var t, next, offset;

          if (!_rootInstance) {
            _merge();

            t = _recordedRoot._first;
            offset = _recordedRoot._start;

            while (t) {
              next = t._next;

              if (t._tDur !== t._tTime || !t._dur && t.progress() !== 1) {
                _globalTimeline.add(t, t._start - t._delay + offset);
              } else {
                t.kill();
              }

              t = next;
            }
          }

          if (GSDevTools.globalRecordingTime > 2) {
            _delayedCall(GSDevTools.globalRecordingTime - 2, function () {
              _rootInstance && _rootInstance.update();
              _globalTimeline.autoRemoveChildren = true;
            });
          } else {
            _globalTimeline.autoRemoveChildren = true;
          }

          _startupPhase = false;
        });

        _coreInitted$1 = 1;
      }
    }
  },
      _checkIndependence = function _checkIndependence(animation, vars) {
    if (!vars.globalSync && animation.parent !== _globalTimeline) {
      _globalTimeline.add(animation, _globalTimeline.time());
    }
  },
      GSDevTools = function GSDevTools(vars) {
    if (!_coreInitted$1) {
      _initCore$1();

      gsap$1 || console.warn("Please gsap.registerPlugin(GSDevTools)");
    }

    this.vars = vars = vars || {};

    if (vars.animation) {
      (GSDevTools.getByAnimation(vars.animation) || {
        kill: function kill() {
          return 0;
        }
      }).kill();
    }

    vars.id = vars.id || (_isString(vars.animation) ? vars.animation : _idSeed++);
    _lookup$1[vars.id + ""] = this;
    "globalSync" in vars || (vars.globalSync = !vars.animation);

    var _self = this,
        root = _createRootElement(vars.container, vars.minimal, vars.css),
        find = function find(s) {
      return root.querySelector(s);
    },
        record = function record(key, value) {
      if (vars.persist !== false && _supportsStorage) {
        sessionStorage.setItem("gs-dev-" + key + vars.id, value);
      }

      return value;
    },
        recall = function recall(key) {
      var value;

      if (vars.persist !== false && _supportsStorage) {
        value = sessionStorage.getItem("gs-dev-" + key + vars.id);
        return key === "animation" ? value : key === "loop" ? value === "true" : parseFloat(value);
      }
    },
        playhead = find(".playhead"),
        timelineTrack = find(".timeline-track"),
        progressBar = find(".progress-bar"),
        timeLabel = find(".time"),
        durationLabel = find(".duration"),
        pixelToTimeRatio,
        timeAtDragStart,
        dragged,
        skipDragUpdates,
        progress = 0,
        inPoint = find(".in-point"),
        outPoint = find(".out-point"),
        inProgress = 0,
        outProgress = 100,
        pausedWhenDragStarted,
        list = find(".animation-list"),
        animationLabel = find(".animation-label"),
        selectedAnimation,
        linkedAnimation,
        declaredAnimation,
        startTime,
        endTime,
        _fullyInitialized,
        keyboardHandler,
        playPauseButton = find(".play-pause"),
        playPauseMorph = _buildPlayPauseMorph(playPauseButton),
        paused = false,
        loopButton = find(".loop"),
        loopAnimation = _buildLoopAnimation(loopButton),
        loopEnabled,
        timeScale = find(".time-scale select"),
        timeScaleLabel = find(".time-scale-label"),
        onPressTimeline = function onPressTimeline(element, originRatio, limitToInOut) {
      return function (e) {
        var trackBounds = timelineTrack.getBoundingClientRect(),
            elementBounds = element.getBoundingClientRect(),
            left = elementBounds.width * originRatio,
            x = gsap$1.getProperty(element, "x"),
            minX = trackBounds.left - elementBounds.left - left + x,
            maxX = trackBounds.right - elementBounds.right + (elementBounds.width - left) + x,
            unlimitedMinX = minX,
            limitBounds;

        if (limitToInOut) {
          if (element !== inPoint) {
            limitBounds = inPoint.getBoundingClientRect();

            if (limitBounds.left) {
              minX += limitBounds.left + limitBounds.width - trackBounds.left;
            }
          }

          if (element !== outPoint) {
            limitBounds = outPoint.getBoundingClientRect();

            if (limitBounds.left) {
              maxX -= trackBounds.left + trackBounds.width - limitBounds.left;
            }
          }
        }

        pausedWhenDragStarted = paused;
        this.applyBounds({
          minX: minX,
          maxX: maxX
        });
        pixelToTimeRatio = linkedAnimation.duration() / trackBounds.width;
        timeAtDragStart = -unlimitedMinX * pixelToTimeRatio;

        if (!skipDragUpdates) {
          linkedAnimation.pause(timeAtDragStart + pixelToTimeRatio * this.x);
        } else {
          linkedAnimation.pause();
        }

        if (this.target === playhead) {
          if (this.activated) {
            this.allowEventDefault = false;
          }

          this.activated = true;
        }

        dragged = true;
      };
    },
        progressDrag = Draggable.create(playhead, {
      type: "x",
      cursor: "ew-resize",
      allowNativeTouchScrolling: false,
      allowEventDefault: true,
      onPress: onPressTimeline(playhead, 0.5, true),
      onDrag: function onDrag() {
        var time = timeAtDragStart + pixelToTimeRatio * this.x;

        if (time < 0) {
          time = 0;
        } else if (time > linkedAnimation._dur) {
          time = linkedAnimation._dur;
        }

        if (!skipDragUpdates) {
          linkedAnimation.time(time);
        }

        progressBar.style.width = Math.min(outProgress - inProgress, Math.max(0, time / linkedAnimation._dur * 100 - inProgress)) + "%";
        timeLabel.innerHTML = time.toFixed(2);
      },
      onRelease: function onRelease() {
        paused || linkedAnimation.resume();
      }
    })[0],
        resetInOut = function resetInOut() {
      inProgress = 0;
      outProgress = 100;
      inPoint.style.left = "0%";
      outPoint.style.left = "100%";
      record("in", inProgress);
      record("out", outProgress);
      updateProgress(true);
    },
        inDrag = Draggable.create(inPoint, {
      type: "x",
      cursor: "ew-resize",
      zIndexBoost: false,
      allowNativeTouchScrolling: false,
      allowEventDefault: true,
      onPress: onPressTimeline(inPoint, 1, true),
      onDoubleClick: resetInOut,
      onDrag: function onDrag() {
        inProgress = (timeAtDragStart + pixelToTimeRatio * this.x) / linkedAnimation.duration() * 100;
        linkedAnimation.progress(inProgress / 100);
        updateProgress(true);
      },
      onRelease: function onRelease() {
        if (inProgress < 0) {
          inProgress = 0;
        }

        _clearSelection();

        inPoint.style.left = inProgress + "%";
        record("in", inProgress);
        gsap$1.set(inPoint, {
          x: 0,
          data: "root",
          display: "block"
        });

        if (!paused) {
          linkedAnimation.resume();
        }
      }
    })[0],
        outDrag = Draggable.create(outPoint, {
      type: "x",
      cursor: "ew-resize",
      allowNativeTouchScrolling: false,
      allowEventDefault: true,
      zIndexBoost: false,
      onPress: onPressTimeline(outPoint, 0, true),
      onDoubleClick: resetInOut,
      onDrag: function onDrag() {
        outProgress = (timeAtDragStart + pixelToTimeRatio * this.x) / linkedAnimation.duration() * 100;
        linkedAnimation.progress(outProgress / 100);
        updateProgress(true);
      },
      onRelease: function onRelease() {
        if (outProgress > 100) {
          outProgress = 100;
        }

        _clearSelection();

        outPoint.style.left = outProgress + "%";
        record("out", outProgress);
        gsap$1.set(outPoint, {
          x: 0,
          data: "root",
          display: "block"
        });

        if (!pausedWhenDragStarted) {
          play();
          linkedAnimation.resume();
        }
      }
    })[0],
        updateProgress = function updateProgress(force) {
      if (progressDrag.isPressed && force !== true) {
        return;
      }

      var p = !loopEnabled && selectedAnimation._repeat === -1 ? selectedAnimation.totalTime() / selectedAnimation.duration() * 100 : linkedAnimation.progress() * 100 || 0,
          repeatDelayPhase = selectedAnimation._repeat && selectedAnimation._rDelay && selectedAnimation.totalTime() % (selectedAnimation.duration() + selectedAnimation._rDelay) > selectedAnimation.duration(),
          target;

      if (p > 100) {
        p = 100;
      }

      if (p >= outProgress) {
        if (loopEnabled && !linkedAnimation.paused() && !progressDrag.isDragging) {
          if (!repeatDelayPhase) {
            p = inProgress;
            target = linkedAnimation._targets && linkedAnimation._targets[0];

            if (target === selectedAnimation) {
              target.seek(startTime + (endTime - startTime) * inProgress / 100);
            }

            if (selectedAnimation._repeat > 0 && !inProgress && outProgress === 100) {
              if (selectedAnimation.totalProgress() === 1) {
                linkedAnimation.totalProgress(0, true).resume();
              }
            } else {
              linkedAnimation.progress(p / 100, true).resume();
            }
          }
        } else {
          if (p !== outProgress || selectedAnimation._repeat === -1) {
            p = outProgress;
            linkedAnimation.progress(p / 100);
          }

          if (!paused && (outProgress < 100 || selectedAnimation.totalProgress() === 1 || selectedAnimation._repeat === -1)) {
            pause();
          }
        }
      } else if (p < inProgress) {
        p = inProgress;
        linkedAnimation.progress(p / 100, true);
      }

      if (p !== progress || force === true) {
        progressBar.style.left = inProgress + "%";
        progressBar.style.width = Math.max(0, p - inProgress) + "%";
        playhead.style.left = p + "%";
        timeLabel.innerHTML = linkedAnimation._time.toFixed(2);
        durationLabel.innerHTML = linkedAnimation._dur.toFixed(2);

        if (dragged) {
          playhead.style.transform = "translate(-50%,0)";
          playhead._gsap.x = "0px";
          playhead._gsap.xPercent = -50;
          dragged = false;
        }

        progress = p;
      }

      linkedAnimation.paused() !== paused && togglePlayPause();
    },
        onPressSeekBar = function onPressSeekBar(e) {
      if (progressDrag.isPressed) {
        return;
      }

      var bounds = e.target.getBoundingClientRect(),
          x = (e.changedTouches ? e.changedTouches[0] : e).clientX,
          p = (x - bounds.left) / bounds.width * 100;

      if (p < inProgress) {
        inProgress = p = Math.max(0, p);
        inPoint.style.left = inProgress + "%";
        inDrag.startDrag(e);
        return;
      } else if (p > outProgress) {
        outProgress = p = Math.min(100, p);
        outPoint.style.left = outProgress + "%";
        outDrag.startDrag(e);
        return;
      }

      linkedAnimation.progress(p / 100).pause();
      updateProgress(true);
      progressDrag.startDrag(e);
    },
        play = function play() {
      if (linkedAnimation.progress() >= outProgress / 100) {
        _checkIndependence(linkedAnimation, vars);

        var target = linkedAnimation._targets && linkedAnimation._targets[0];

        if (target === selectedAnimation) {
          target.seek(startTime + (endTime - startTime) * inProgress / 100);
        }

        if (linkedAnimation._repeat && !inProgress) {
          linkedAnimation.totalProgress(0, true);
        } else if (!linkedAnimation.reversed()) {
          linkedAnimation.progress(inProgress / 100, true);
        }
      }

      playPauseMorph.play();
      linkedAnimation.resume();

      if (paused) {
        _self.update();
      }

      paused = false;
    },
        pause = function pause() {
      playPauseMorph.reverse();

      if (linkedAnimation) {
        linkedAnimation.pause();
      }

      paused = true;
    },
        togglePlayPause = function togglePlayPause() {
      if (paused) {
        play();
      } else {
        pause();
      }
    },
        onPressRewind = function onPressRewind(e) {
      if (progressDrag.isPressed) {
        return;
      }

      _checkIndependence(linkedAnimation, vars);

      var target = linkedAnimation._targets && linkedAnimation._targets[0];

      if (target === selectedAnimation) {
        target.seek(startTime + (endTime - startTime) * inProgress / 100);
      }

      linkedAnimation.progress(inProgress / 100, true);

      if (!paused) {
        linkedAnimation.resume();
      }
    },
        loop = function loop(value) {
      loopEnabled = value;
      record("loop", loopEnabled);

      if (loopEnabled) {
        loopAnimation.play();

        if (linkedAnimation.progress() >= outProgress / 100) {
          var target = linkedAnimation._targets && linkedAnimation._targets[0];

          if (target === selectedAnimation) {
            target.seek(startTime + (endTime - startTime) * inProgress / 100);
          }

          if (selectedAnimation._repeat && !inProgress && outProgress === 100) {
            linkedAnimation.totalProgress(0, true);
          } else {
            linkedAnimation.progress(inProgress / 100, true);
          }

          play();
        }
      } else {
        loopAnimation.reverse();
      }
    },
        toggleLoop = function toggleLoop() {
      return loop(!loopEnabled);
    },
        updateList = function updateList() {
      var animations = _getChildrenOf(declaredAnimation && !vars.globalSync ? declaredAnimation : _recordedRoot, true),
          options = list.children,
          matches = 0,
          option,
          i;

      if (declaredAnimation && !vars.globalSync) {
        animations.unshift(declaredAnimation);
      } else if (!vars.hideGlobalTimeline) {
        animations.unshift(_recordedRoot);
      }

      for (i = 0; i < animations.length; i++) {
        option = options[i] || _createElement$1("option", list);
        option.animation = animations[i];
        matches = i && animations[i].vars.id === animations[i - 1].vars.id ? matches + 1 : 0;
        option.setAttribute("value", option.innerHTML = animations[i].vars.id + (matches ? " [" + matches + "]" : animations[i + 1] && animations[i + 1].vars.id === animations[i].vars.id ? " [0]" : ""));
      }

      for (; i < options.length; i++) {
        list.removeChild(options[i]);
      }
    },
        animation = function animation(anim) {
      var ts = parseFloat(timeScale.options[timeScale.selectedIndex].value) || 1,
          tl,
          maxDuration;

      if (!arguments.length) {
        return selectedAnimation;
      }

      if (_isString(anim)) {
        anim = _getAnimationById(anim);
      }

      if (!(anim instanceof Animation)) {
        console.warn("GSDevTools error: invalid animation.");
      }

      if (anim.scrollTrigger) {
        console.warn("GSDevTools can't work with ScrollTrigger-based animations; either the scrollbar -OR- the GSDevTools scrubber can control the animation.");
      }

      if (anim === selectedAnimation) {
        return;
      }

      if (selectedAnimation) {
        selectedAnimation._inProgress = inProgress;
        selectedAnimation._outProgress = outProgress;
      }

      selectedAnimation = anim;

      if (linkedAnimation) {
        ts = linkedAnimation.timeScale();

        if (linkedAnimation._targets && linkedAnimation._targets[0] === declaredAnimation) {
          declaredAnimation.resume();
          linkedAnimation.kill();
        }
      }

      inProgress = selectedAnimation._inProgress || 0;
      outProgress = selectedAnimation._outProgress || 100;
      inPoint.style.left = inProgress + "%";
      outPoint.style.left = outProgress + "%";

      if (_fullyInitialized) {
        record("animation", selectedAnimation.vars.id);
        record("in", inProgress);
        record("out", outProgress);
      }

      startTime = 0;
      maxDuration = vars.maxDuration || Math.min(1000, _getClippedDuration(selectedAnimation));

      if (selectedAnimation === _recordedRoot || vars.globalSync) {
        _merge();

        linkedAnimation = _rootTween;
        _rootInstance && _rootInstance !== _self && console.warn("Error: GSDevTools can only have one instance that's globally synchronized.");
        _rootInstance = _self;

        if (selectedAnimation !== _recordedRoot) {
          tl = selectedAnimation;
          endTime = tl.totalDuration();

          if (endTime > 99999999) {
            endTime = tl.duration();
          }

          while (tl.parent) {
            startTime = startTime / tl._ts + tl._start;
            endTime = endTime / tl._ts + tl._start;
            tl = tl.parent;
          }
        } else {
          endTime = _recordedRoot.duration();
        }

        if (endTime - startTime > maxDuration) {
          endTime = startTime + maxDuration;
        }

        _recordedRoot.pause(startTime);

        _rootTween.vars.time = endTime;

        _rootTween.invalidate();

        _rootTween.duration(endTime - startTime).timeScale(ts);

        if (paused) {
          _rootTween.progress(1, true).pause(0, true);
        } else {
          _delayedCall(0.01, function () {
            _rootTween.resume().progress(inProgress / 100);

            paused && play();
          });
        }
      } else {
        if (_rootInstance === _self) {
          _rootInstance = null;
        }

        startTime = Math.min(inProgress * selectedAnimation.duration(), selectedAnimation.time());

        if (selectedAnimation === declaredAnimation || !declaredAnimation) {
          linkedAnimation = selectedAnimation;

          if (!loopEnabled && linkedAnimation._repeat) {
            loop(true);
          }
        } else {
          tl = selectedAnimation;
          endTime = tl.totalDuration();

          if (endTime > 99999999) {
            endTime = tl.duration();
          }

          while (tl.parent && tl !== declaredAnimation) {
            startTime = startTime / (tl._ts || tl._rts) + tl._start;
            endTime = endTime / (tl._ts || tl._rts) + tl._start;
            tl = tl.parent;
          }

          if (endTime - startTime > maxDuration) {
            endTime = startTime + maxDuration;
          }

          declaredAnimation.pause(startTime);
          linkedAnimation = gsap$1.to(declaredAnimation, {
            duration: endTime - startTime,
            time: endTime,
            ease: "none",
            data: "root",
            parent: _independentRoot
          }, _independentRoot._time);
        }

        linkedAnimation.timeScale(ts);

        _rootTween.pause();

        _recordedRoot.resume();

        linkedAnimation.seek(0);
      }

      durationLabel.innerHTML = linkedAnimation.duration().toFixed(2);

      _selectValue(list, selectedAnimation.vars.id, animationLabel);
    },
        updateRootDuration = function updateRootDuration() {
      var time, ratio, duration;

      if (selectedAnimation === _recordedRoot) {
        time = _recordedRoot._time;

        _recordedRoot.progress(1, true).time(time, true);

        time = (_rootTween._dp._time - _rootTween._start) * _rootTween._ts;
        duration = Math.min(1000, _recordedRoot.duration());

        if (duration === 1000) {
          duration = Math.min(1000, _getClippedDuration(_recordedRoot));
        }

        ratio = _rootTween.duration() / duration;

        if (ratio !== 1 && duration) {
          inProgress *= ratio;

          if (outProgress < 100) {
            outProgress *= ratio;
          }

          _rootTween.seek(0);

          _rootTween.vars.time = duration;

          _rootTween.invalidate();

          _rootTween.duration(duration);

          _rootTween.time(time);

          durationLabel.innerHTML = duration.toFixed(2);
          inPoint.style.left = inProgress + "%";
          outPoint.style.left = outProgress + "%";
          updateProgress(true);
        }
      }
    },
        onChangeAnimation = function onChangeAnimation(e) {
      animation(list.options[list.selectedIndex].animation);

      if (e.target && e.target.blur) {
        e.target.blur();
      }

      paused && play();
    },
        onChangeTimeScale = function onChangeTimeScale(e) {
      var ts = parseFloat(timeScale.options[timeScale.selectedIndex].value) || 1,
          target;
      linkedAnimation.timeScale(ts);
      record("timeScale", ts);

      if (!paused) {
        if (linkedAnimation.progress() >= outProgress / 100) {
          target = linkedAnimation._targets && linkedAnimation._targets[0];

          if (target === selectedAnimation) {
            target.seek(startTime + (endTime - startTime) * inProgress / 100);
          }

          linkedAnimation.progress(inProgress / 100, true).pause();
        } else {
          linkedAnimation.pause();
        }

        _delayedCall(0.01, function () {
          return linkedAnimation.resume();
        });
      }

      timeScaleLabel.innerHTML = ts + "x";

      if (timeScale.blur) {
        timeScale.blur();
      }
    },
        autoHideTween = gsap$1.to([find(".gs-bottom"), find(".gs-top")], {
      duration: 0.3,
      autoAlpha: 0,
      y: 50,
      ease: "power2.in",
      data: "root",
      paused: true,
      parent: _independentRoot
    }, _independentRoot._time),
        hidden = false,
        onMouseOut = function onMouseOut(e) {
      if (!Draggable.hitTest(e, root) && !progressDrag.isDragging && !inDrag.isDragging && !outDrag.isDragging) {
        autoHideDelayedCall.restart(true);
      }
    },
        hide = function hide() {
      if (!hidden) {
        autoHideTween.play();
        autoHideDelayedCall.pause();
        hidden = true;
      }
    },
        show = function show() {
      autoHideDelayedCall.pause();

      if (hidden) {
        autoHideTween.reverse();
        hidden = false;
      }
    },
        toggleHide = function toggleHide() {
      if (hidden) {
        show();
      } else {
        hide();
      }
    },
        autoHideDelayedCall = _delayedCall(1.3, hide).pause(),
        initialize = function initialize(preliminary) {
      if (_startupPhase && !_globalStartTime) {
        _globalStartTime = _recordedRoot._start;
      }

      _fullyInitialized = !preliminary;
      declaredAnimation = _parseAnimation(vars.animation);

      if (declaredAnimation && !declaredAnimation.vars.id) {
        declaredAnimation.vars.id = "[no id]";
      }

      _merge();

      updateList();

      var savedAnimation = _getAnimationById(recall("animation"));

      if (savedAnimation) {
        savedAnimation._inProgress = recall("in") || 0;
        savedAnimation._outProgress = recall("out") || 100;
      }

      vars.paused && pause();
      selectedAnimation = null;
      animation(declaredAnimation || savedAnimation || _recordedRoot);
      var ts = vars.timeScale || recall("timeScale"),
          savedInOut = savedAnimation === selectedAnimation;

      if (ts) {
        _selectValue(timeScale, ts, timeScaleLabel, ts + "x");

        linkedAnimation.timeScale(ts);
      }

      inProgress = ("inTime" in vars ? _timeToProgress(vars.inTime, selectedAnimation, 0, 0) : savedInOut ? savedAnimation._inProgress : 0) || 0;

      if (inProgress === 100 && !vars.animation && savedAnimation) {
        animation(_recordedRoot);
        inProgress = _timeToProgress(vars.inTime, selectedAnimation, 0, 0) || 0;
      }

      if (inProgress) {
        inPoint.style.left = inProgress + "%";
        inPoint.style.display = outPoint.style.display = "block";
      }

      outProgress = ("outTime" in vars ? _timeToProgress(vars.outTime, selectedAnimation, 100, inProgress) : savedInOut ? savedAnimation._outProgress : 0) || 100;

      if (outProgress < inProgress) {
        outProgress = 100;
      }

      if (outProgress !== 100) {
        outPoint.style.left = outProgress + "%";
        inPoint.style.display = outPoint.style.display = "block";
      }

      loopEnabled = "loop" in vars ? vars.loop : recall("loop");
      loopEnabled && loop(true);
      vars.paused && linkedAnimation.progress(inProgress / 100, true).pause();

      if (_startupPhase && selectedAnimation === _recordedRoot && _globalStartTime && vars.globalSync && !paused) {
        linkedAnimation.time(-_globalStartTime, true);
      }

      updateProgress(true);
    };

    _addListener$1(list, "change", onChangeAnimation);

    _addListener$1(list, "mousedown", updateList);

    _addListener$1(playPauseButton, "mousedown", togglePlayPause);

    _addListener$1(find(".seek-bar"), "mousedown", onPressSeekBar);

    _addListener$1(find(".rewind"), "mousedown", onPressRewind);

    _addListener$1(loopButton, "mousedown", toggleLoop);

    _addListener$1(timeScale, "change", onChangeTimeScale);

    if (vars.visibility === "auto") {
      _addListener$1(root, "mouseout", onMouseOut);

      _addListener$1(root, "mouseover", show);
    } else if (vars.visibility === "hidden") {
      hidden = true;
      autoHideTween.progress(1);
    }

    if (vars.keyboard !== false) {
      if (_keyboardInstance && vars.keyboard) {
        console.warn("[GSDevTools warning] only one instance can be affected by keyboard shortcuts. There is already one active.");
      } else {
        _keyboardInstance = _self;

        keyboardHandler = function keyboardHandler(e) {
          var key = e.keyCode ? e.keyCode : e.which,
              ts;

          if (key === 32) {
            togglePlayPause();
          } else if (key === 38) {
            ts = parseFloat(_shiftSelectedValue(timeScale, -1, timeScaleLabel));
            linkedAnimation.timeScale(ts);
            record("timeScale", ts);
          } else if (key === 40) {
            ts = parseFloat(_shiftSelectedValue(timeScale, 1, timeScaleLabel));
            linkedAnimation.timeScale(ts);
            record("timeScale", ts);
          } else if (key === 37) {
            onPressRewind();
          } else if (key === 39) {
            linkedAnimation.progress(outProgress / 100);
          } else if (key === 76) {
            toggleLoop();
          } else if (key === 72) {
            toggleHide();
          } else if (key === 73) {
            inProgress = linkedAnimation.progress() * 100;
            record("in", inProgress);
            inPoint.style.left = inProgress + "%";
            updateProgress(true);
          } else if (key === 79) {
            outProgress = linkedAnimation.progress() * 100;
            record("out", outProgress);
            outPoint.style.left = outProgress + "%";
            updateProgress(true);
          }
        };

        _addListener$1(_docEl, "keydown", keyboardHandler);
      }
    }

    gsap$1.set(playhead, {
      xPercent: -50,
      x: 0,
      data: "root"
    });
    gsap$1.set(inPoint, {
      xPercent: -100,
      x: 0,
      data: "root"
    });
    inPoint._gsIgnore = outPoint._gsIgnore = playhead._gsIgnore = playPauseButton._gsIgnore = loopButton._gsIgnore = true;
    gsap$1.killTweensOf([inPoint, outPoint, playhead]);
    initialize(_startupPhase);

    if (_startupPhase) {
      _delayedCall(0.0001, initialize, [false], this);
    }

    gsap$1.ticker.add(updateProgress);

    this.update = function (forceMerge) {
      if (_rootInstance === _self) {
        if (!_rootTween.paused() || forceMerge) {
          _merge();
        }

        updateRootDuration();
      }
    };

    this.kill = this.revert = function () {
      _removeListener$1(list, "change", onChangeAnimation);

      _removeListener$1(list, "mousedown", updateList);

      _removeListener$1(playPauseButton, "mousedown", togglePlayPause);

      _removeListener$1(find(".seek-bar"), "mousedown", onPressSeekBar);

      _removeListener$1(find(".rewind"), "mousedown", onPressRewind);

      _removeListener$1(loopButton, "mousedown", toggleLoop);

      _removeListener$1(timeScale, "change", onChangeTimeScale);

      progressDrag.disable();
      inDrag.disable();
      outDrag.disable();
      gsap$1.ticker.remove(updateProgress);

      _removeListener$1(root, "mouseout", onMouseOut);

      _removeListener$1(root, "mouseover", show);

      root.parentNode.removeChild(root);

      if (_rootInstance === _self) {
        _rootInstance = null;
      }

      if (_keyboardInstance === _self) {
        _keyboardInstance = null;

        _removeListener$1(_docEl, "keydown", keyboardHandler);
      }

      delete _lookup$1[vars.id + ""];
    };

    this.minimal = function (value) {
      var isMinimal = root.classList.contains("minimal"),
          p;

      if (!arguments.length || isMinimal === value) {
        return isMinimal;
      }

      if (value) {
        root.classList.add("minimal");
      } else {
        root.classList.remove("minimal");
      }

      if (vars.container) {
        root.style.top = value ? "calc(100% - 42px)" : "calc(100% - 51px)";
      }

      if (progressDrag.isPressed) {
        skipDragUpdates = true;
        progressDrag.endDrag(progressDrag.pointerEvent);
        skipDragUpdates = false;
        p = linkedAnimation.progress() * 100;
        progressBar.style.width = Math.max(0, p - inProgress) + "%";
        playhead.style.left = p + "%";
        playhead.style.transform = "translate(-50%,0)";
        playhead._gsap.x = "0px";
        playhead._gsap.xPercent = -50;
        progressDrag.startDrag(progressDrag.pointerEvent, true);
      }
    };

    this.animation = animation;
    this.updateList = updateList;

    _context$1(this);
  };

  GSDevTools.version = "3.15.0";
  GSDevTools.globalRecordingTime = 2;

  GSDevTools.getById = function (id) {
    return id ? _lookup$1[id] : _rootInstance;
  };

  GSDevTools.getByAnimation = function (animation) {
    if (_isString(animation)) {
      animation = gsap$1.getById(animation);
    }

    for (var p in _lookup$1) {
      if (_lookup$1[p].animation() === animation) {
        return _lookup$1[p];
      }
    }
  };

  GSDevTools.create = function (vars) {
    return new GSDevTools(vars);
  };

  GSDevTools.register = _initCore$1;
  _getGSAP$1() && gsap$1.registerPlugin(GSDevTools);

  exports.GSDevTools = GSDevTools;
  exports.default = GSDevTools;

  if (typeof(window) === 'undefined' || window !== exports) {Object.defineProperty(exports, '__esModule', { value: true });} else {delete window.default;}

})));
