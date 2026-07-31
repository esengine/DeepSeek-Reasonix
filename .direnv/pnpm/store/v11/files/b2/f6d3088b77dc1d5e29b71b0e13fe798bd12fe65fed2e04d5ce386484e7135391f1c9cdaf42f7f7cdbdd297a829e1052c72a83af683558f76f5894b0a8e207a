/*!
 * Flip 3.15.0
 * https://gsap.com
 *
 * @license Copyright 2008-2026, GreenSock. All rights reserved.
 * Subject to the terms at https://gsap.com/standard-license
 * @author: Jack Doyle, jack@greensock.com
*/

/* eslint-disable */
import { getGlobalMatrix, _getDocScrollTop, _getDocScrollLeft, Matrix2D, _setDoc, _getCTM } from "./utils/matrix.js";

var _id = 1,
    _toArray,
    gsap,
    _batch,
    _batchAction,
    _body,
    _closestTenth,
    _getStyleSaver,
    _forEachBatch = function _forEachBatch(batch, name) {
  return batch.actions.forEach(function (a) {
    return a.vars[name] && a.vars[name](a);
  });
},
    _batchLookup = {},
    _RAD2DEG = 180 / Math.PI,
    _DEG2RAD = Math.PI / 180,
    _emptyObj = {},
    _dashedNameLookup = {},
    _memoizedRemoveProps = {},
    _listToArray = function _listToArray(list) {
  return typeof list === "string" ? list.split(" ").join("").split(",") : list;
},
    // removes extra spaces contaminating the names, returns an Array.
_callbacks = _listToArray("onStart,onUpdate,onComplete,onReverseComplete,onInterrupt"),
    _removeProps = _listToArray("transform,transformOrigin,width,height,position,top,left,opacity,zIndex,maxWidth,maxHeight,minWidth,minHeight"),
    _getEl = function _getEl(target) {
  return _toArray(target)[0] || console.warn("Element not found:", target);
},
    _round = function _round(value) {
  return Math.round(value * 10000) / 10000 || 0;
},
    _toggleClass = function _toggleClass(targets, className, action) {
  return targets.forEach(function (el) {
    return el.classList[action](className);
  });
},
    _reserved = {
  zIndex: 1,
  kill: 1,
  simple: 1,
  spin: 1,
  clearProps: 1,
  targets: 1,
  toggleClass: 1,
  onComplete: 1,
  onUpdate: 1,
  onInterrupt: 1,
  onStart: 1,
  delay: 1,
  repeat: 1,
  repeatDelay: 1,
  yoyo: 1,
  scale: 1,
  fade: 1,
  absolute: 1,
  props: 1,
  onEnter: 1,
  onLeave: 1,
  custom: 1,
  paused: 1,
  nested: 1,
  prune: 1,
  absoluteOnLeave: 1
},
    _fitReserved = {
  zIndex: 1,
  simple: 1,
  clearProps: 1,
  scale: 1,
  absolute: 1,
  fitChild: 1,
  getVars: 1,
  props: 1
},
    _camelToDashed = function _camelToDashed(p) {
  return p.replace(/([A-Z])/g, "-$1").toLowerCase();
},
    _copy = function _copy(obj, exclude) {
  var result = {},
      p;

  for (p in obj) {
    exclude[p] || (result[p] = obj[p]);
  }

  return result;
},
    _memoizedProps = {},
    _memoizeProps = function _memoizeProps(props) {
  var p = _memoizedProps[props] = _listToArray(props);

  _memoizedRemoveProps[props] = p.concat(_removeProps);
  return p;
},
    _getInverseGlobalMatrix = function _getInverseGlobalMatrix(el) {
  // integrates caching for improved performance
  var cache = el._gsap || gsap.core.getCache(el);

  if (cache.gmCache === gsap.ticker.frame) {
    return cache.gMatrix;
  }

  cache.gmCache = gsap.ticker.frame;
  return cache.gMatrix = getGlobalMatrix(el, true, false, true);
},
    _getDOMDepth = function _getDOMDepth(el, invert, level) {
  if (level === void 0) {
    level = 0;
  }

  // In invert is true, the sibling depth is increments of 1, and parent/nesting depth is increments of 1000. This lets us order elements in an Array to reflect document flow.
  var parent = el.parentNode,
      inc = 1000 * Math.pow(10, level) * (invert ? -1 : 1),
      l = invert ? -inc * 900 : 0;

  while (el) {
    l += inc;
    el = el.previousSibling;
  }

  return parent ? l + _getDOMDepth(parent, invert, level + 1) : l;
},
    _orderByDOMDepth = function _orderByDOMDepth(comps, invert, isElStates) {
  comps.forEach(function (comp) {
    return comp.d = _getDOMDepth(isElStates ? comp.element : comp.t, invert);
  });
  comps.sort(function (c1, c2) {
    return c1.d - c2.d;
  });
  return comps;
},
    _recordInlineStyles = function _recordInlineStyles(elState, props) {
  // records the current inline CSS properties into an Array in alternating name/value pairs that's stored in a "css" property on the state object so that we can revert later.
  var style = elState.element.style,
      a = elState.css = elState.css || [],
      i = props.length,
      p,
      v;

  while (i--) {
    p = props[i];
    v = style[p] || style.getPropertyValue(p);
    a.push(v ? p : _dashedNameLookup[p] || (_dashedNameLookup[p] = _camelToDashed(p)), v);
  }

  return style;
},
    _applyInlineStyles = function _applyInlineStyles(state) {
  var css = state.css,
      style = state.element.style,
      i = 0;
  state.cache.uncache = 1;

  for (; i < css.length; i += 2) {
    css[i + 1] ? style[css[i]] = css[i + 1] : style.removeProperty(css[i]);
  }

  if (!css[css.indexOf("transform") + 1] && style.translate) {
    // CSSPlugin adds scale, translate, and rotate inline CSS as "none" in order to keep CSS rules from contaminating transforms.
    style.removeProperty("translate");
    style.removeProperty("scale");
    style.removeProperty("rotate");
  }
},
    _setFinalStates = function _setFinalStates(comps, onlyTransforms) {
  comps.forEach(function (c) {
    return c.a.cache.uncache = 1;
  });
  onlyTransforms || comps.finalStates.forEach(_applyInlineStyles);
},
    _absoluteProps = "paddingTop,paddingRight,paddingBottom,paddingLeft,gridArea,transition".split(","),
    // properties that we must record just
_makeAbsolute = function _makeAbsolute(elState, fallbackNode, ignoreBatch) {
  var element = elState.element,
      width = elState.width,
      height = elState.height,
      uncache = elState.uncache,
      getProp = elState.getProp,
      style = element.style,
      i = 4,
      result,
      displayIsNone,
      cs;
  typeof fallbackNode !== "object" && (fallbackNode = elState);

  if (_batch && ignoreBatch !== 1) {
    _batch._abs.push({
      t: element,
      b: elState,
      a: elState,
      sd: 0
    });

    _batch._final.push(function () {
      return (elState.cache.uncache = 1) && _applyInlineStyles(elState);
    });

    return element;
  }

  displayIsNone = getProp("display") === "none";

  if (!elState.isVisible || displayIsNone) {
    displayIsNone && (_recordInlineStyles(elState, ["display"]).display = fallbackNode.display);
    elState.matrix = fallbackNode.matrix;
    elState.width = width = elState.width || fallbackNode.width;
    elState.height = height = elState.height || fallbackNode.height;
  }

  _recordInlineStyles(elState, _absoluteProps);

  cs = window.getComputedStyle(element);

  while (i--) {
    style[_absoluteProps[i]] = cs[_absoluteProps[i]]; // record paddings as px-based because if removed from grid, percentage-based ones could be altered.
  }

  style.gridArea = "1 / 1 / 1 / 1";
  style.transition = "none";
  style.position = "absolute";
  style.width = width + "px";
  style.height = height + "px";
  style.top || (style.top = "0px");
  style.left || (style.left = "0px");

  if (uncache) {
    result = new ElementState(element);
  } else {
    // better performance
    result = _copy(elState, _emptyObj);
    result.position = "absolute";

    if (elState.simple) {
      var bounds = element.getBoundingClientRect();
      result.matrix = new Matrix2D(1, 0, 0, 1, bounds.left + _getDocScrollLeft(), bounds.top + _getDocScrollTop());
    } else {
      result.matrix = getGlobalMatrix(element, false, false, true);
    }
  }

  result = _fit(result, elState, true);
  elState.x = _closestTenth(result.x, 0.01);
  elState.y = _closestTenth(result.y, 0.01);
  return element;
},
    _filterComps = function _filterComps(comps, targets) {
  if (targets !== true) {
    targets = _toArray(targets);
    comps = comps.filter(function (c) {
      if (targets.indexOf((c.sd < 0 ? c.b : c.a).element) !== -1) {
        return true;
      } else {
        c.t._gsap.renderTransform(1); // we must force transforms to render on anything that isn't being made position: absolute, otherwise the absolute position happens and then when animation begins it applies transforms which can create a new stacking context, throwing off positioning!


        if (c.b.isVisible) {
          c.t.style.width = c.b.width + "px"; // otherwise things can collapse when contents are made position: absolute.

          c.t.style.height = c.b.height + "px";
        }
      }
    });
  }

  return comps;
},
    _makeCompsAbsolute = function _makeCompsAbsolute(comps) {
  return _orderByDOMDepth(comps, true).forEach(function (c) {
    return (c.a.isVisible || c.b.isVisible) && _makeAbsolute(c.sd < 0 ? c.b : c.a, c.b, 1);
  });
},
    _findElStateInState = function _findElStateInState(state, other) {
  return other && state.idLookup[_parseElementState(other).id] || state.elementStates[0];
},
    _parseElementState = function _parseElementState(elOrNode, props, simple, other) {
  return elOrNode instanceof ElementState ? elOrNode : elOrNode instanceof FlipState ? _findElStateInState(elOrNode, other) : new ElementState(typeof elOrNode === "string" ? _getEl(elOrNode) || console.warn(elOrNode + " not found") : elOrNode, props, simple);
},
    _recordProps = function _recordProps(elState, props) {
  var getProp = gsap.getProperty(elState.element, null, "native"),
      obj = elState.props = {},
      i = props.length;

  while (i--) {
    obj[props[i]] = (getProp(props[i]) + "").trim();
  }

  obj.zIndex && (obj.zIndex = parseFloat(obj.zIndex) || 0);
  return elState;
},
    _applyProps = function _applyProps(element, props) {
  var style = element.style || element,
      // could pass in a vars object.
  p;

  for (p in props) {
    style[p] = props[p];
  }
},
    _getID = function _getID(el) {
  var id = el.getAttribute("data-flip-id");
  id || el.setAttribute("data-flip-id", id = "auto-" + _id++);
  return id;
},
    _elementsFromElementStates = function _elementsFromElementStates(elStates) {
  return elStates.map(function (elState) {
    return elState.element;
  });
},
    _handleCallback = function _handleCallback(callback, elStates, tl) {
  return callback && elStates.length && tl.add(callback(_elementsFromElementStates(elStates), tl, new FlipState(elStates, 0, true)), 0);
},
    _fit = function _fit(fromState, toState, scale, applyProps, fitChild, vars) {
  var element = fromState.element,
      cache = fromState.cache,
      parent = fromState.parent,
      x = fromState.x,
      y = fromState.y,
      width = toState.width,
      height = toState.height,
      scaleX = toState.scaleX,
      scaleY = toState.scaleY,
      rotation = toState.rotation,
      bounds = toState.bounds,
      styles = vars && _getStyleSaver && _getStyleSaver(element, "transform,width,height"),
      dimensionState = fromState,
      _toState$matrix = toState.matrix,
      e = _toState$matrix.e,
      f = _toState$matrix.f,
      deep = fromState.bounds.width !== bounds.width || fromState.bounds.height !== bounds.height || fromState.scaleX !== scaleX || fromState.scaleY !== scaleY || fromState.rotation !== rotation,
      simple = !deep && fromState.simple && toState.simple && !fitChild,
      skewX,
      fromPoint,
      toPoint,
      getProp,
      parentMatrix,
      matrix,
      bbox;

  if (simple || !parent) {
    scaleX = scaleY = 1;
    rotation = skewX = 0;
  } else {
    parentMatrix = _getInverseGlobalMatrix(parent);
    matrix = parentMatrix.clone().multiply(toState.ctm ? toState.matrix.clone().multiply(toState.ctm) : toState.matrix); // root SVG elements have a ctm that we must factor out (for example, viewBox:"0 0 94 94" with a width of 200px would scale the internals by 2.127 but when we're matching the size of the root <svg> element itself, that scaling shouldn't factor in!)

    rotation = _round(Math.atan2(matrix.b, matrix.a) * _RAD2DEG);
    skewX = _round(Math.atan2(matrix.c, matrix.d) * _RAD2DEG + rotation) % 360; // in very rare cases, minor rounding might end up with 360 which should be 0.

    scaleX = Math.sqrt(Math.pow(matrix.a, 2) + Math.pow(matrix.b, 2));
    scaleY = Math.sqrt(Math.pow(matrix.c, 2) + Math.pow(matrix.d, 2)) * Math.cos(skewX * _DEG2RAD);

    if (fitChild) {
      fitChild = _toArray(fitChild)[0];
      getProp = gsap.getProperty(fitChild);
      bbox = fitChild.getBBox && typeof fitChild.getBBox === "function" && fitChild.getBBox();
      dimensionState = {
        scaleX: getProp("scaleX"),
        scaleY: getProp("scaleY"),
        width: bbox ? bbox.width : Math.ceil(parseFloat(getProp("width", "px"))),
        height: bbox ? bbox.height : parseFloat(getProp("height", "px"))
      };
    }

    cache.rotation = rotation + "deg";
    cache.skewX = skewX + "deg";
  }

  if (scale) {
    scaleX *= width === dimensionState.width || !dimensionState.width ? 1 : width / dimensionState.width; // note if widths are both 0, we should make scaleX 1 - some elements have box-sizing that incorporates padding, etc. and we don't want it to collapse in that case.

    scaleY *= height === dimensionState.height || !dimensionState.height ? 1 : height / dimensionState.height;
    cache.scaleX = scaleX;
    cache.scaleY = scaleY;
  } else {
    width = _closestTenth(width * scaleX / dimensionState.scaleX, 0);
    height = _closestTenth(height * scaleY / dimensionState.scaleY, 0);
    element.style.width = width + "px";
    element.style.height = height + "px";
  } // if (fromState.isFixed) { // commented out because it's now taken care of in getGlobalMatrix() with a flag at the end.
  // 	e -= _getDocScrollLeft();
  // 	f -= _getDocScrollTop();
  // }


  applyProps && _applyProps(element, toState.props);

  if (simple || !parent) {
    x += e - fromState.matrix.e;
    y += f - fromState.matrix.f;
  } else if (deep || parent !== toState.parent) {
    cache.x = x + "px";
    cache.y = y + "px";
    cache.renderTransform(1, cache);
    matrix = getGlobalMatrix(fitChild || element, false, false, true);
    fromPoint = parentMatrix.apply({
      x: matrix.e,
      y: matrix.f
    });
    toPoint = parentMatrix.apply({
      x: e,
      y: f
    });
    x += toPoint.x - fromPoint.x;
    y += toPoint.y - fromPoint.y;
  } else {
    // use a faster/cheaper algorithm if we're just moving x/y
    parentMatrix.e = parentMatrix.f = 0;
    toPoint = parentMatrix.apply({
      x: e - fromState.matrix.e,
      y: f - fromState.matrix.f
    });
    x += toPoint.x;
    y += toPoint.y;
  }

  x = _closestTenth(x, 0.02);
  y = _closestTenth(y, 0.02);

  if (vars && !(vars instanceof ElementState)) {
    // revert
    styles && styles.revert();
  } else {
    // or apply the transform immediately
    cache.x = x + "px";
    cache.y = y + "px";
    cache.renderTransform(1, cache);
  }

  if (vars) {
    vars.x = x;
    vars.y = y;
    vars.rotation = rotation;
    vars.skewX = skewX;

    if (scale) {
      vars.scaleX = scaleX;
      vars.scaleY = scaleY;
    } else {
      vars.width = width;
      vars.height = height;
    }
  }

  return vars || cache;
},
    _parseState = function _parseState(targetsOrState, vars) {
  return targetsOrState instanceof FlipState ? targetsOrState : new FlipState(targetsOrState, vars);
},
    _getChangingElState = function _getChangingElState(toState, fromState, id) {
  var to1 = toState.idLookup[id],
      to2 = toState.alt[id];
  return to2.isVisible && (!(fromState.getElementState(to2.element) || to2).isVisible || !to1.isVisible) ? to2 : to1;
},
    _bodyMetrics = [],
    _bodyProps = "width,height,overflowX,overflowY".split(","),
    _bodyLocked,
    _lockBodyScroll = function _lockBodyScroll(lock) {
  // if there's no scrollbar, we should lock that so that measurements don't get affected by temporary repositioning, like if something is centered in the window.
  if (lock !== _bodyLocked) {
    var s = _body.style,
        w = _body.clientWidth === window.outerWidth,
        h = _body.clientHeight === window.outerHeight,
        i = 4;

    if (lock && (w || h)) {
      while (i--) {
        _bodyMetrics[i] = s[_bodyProps[i]];
      }

      if (w) {
        s.width = _body.clientWidth + "px";
        s.overflowY = "hidden";
      }

      if (h) {
        s.height = _body.clientHeight + "px";
        s.overflowX = "hidden";
      }

      _bodyLocked = lock;
    } else if (_bodyLocked) {
      while (i--) {
        _bodyMetrics[i] ? s[_bodyProps[i]] = _bodyMetrics[i] : s.removeProperty(_camelToDashed(_bodyProps[i]));
      }

      _bodyLocked = lock;
    }
  }
},
    _revertTempStyles = function _revertTempStyles(temps, stateIndex) {
  // in _fromTo(), we store the inline styles temporarily when nested is true, and the Array is like [element, styles1, styles2, element, styles1, styles2, ...] where styles1 is one state and styles2 is another state (the element.getAttribute("style")).
  for (var i = 0; i < temps.length; i += 3) {
    gsap.set(temps[i], {
      clearProps: true
    }); // to clear cached transforms too

    temps[i].setAttribute("style", temps[i + stateIndex]);
    temps[i]._gsap.gmCache = -1; // bust the globalMatrix cache
  }
},
    _fromTo = function _fromTo(fromState, toState, vars, relative) {
  // relative is -1 if "from()", and 1 if "to()"
  fromState instanceof FlipState && toState instanceof FlipState || console.warn("Not a valid state object.");
  vars = vars || {};

  var _vars = vars,
      clearProps = _vars.clearProps,
      onEnter = _vars.onEnter,
      onLeave = _vars.onLeave,
      absolute = _vars.absolute,
      absoluteOnLeave = _vars.absoluteOnLeave,
      custom = _vars.custom,
      delay = _vars.delay,
      paused = _vars.paused,
      repeat = _vars.repeat,
      repeatDelay = _vars.repeatDelay,
      yoyo = _vars.yoyo,
      toggleClass = _vars.toggleClass,
      nested = _vars.nested,
      _zIndex = _vars.zIndex,
      scale = _vars.scale,
      fade = _vars.fade,
      stagger = _vars.stagger,
      spin = _vars.spin,
      prune = _vars.prune,
      props = ("props" in vars ? vars : fromState).props,
      tweenVars = _copy(vars, _reserved),
      animation = gsap.timeline({
    delay: delay,
    paused: paused,
    repeat: repeat,
    repeatDelay: repeatDelay,
    yoyo: yoyo,
    data: "isFlip"
  }),
      remainingProps = tweenVars,
      entering = [],
      leaving = [],
      comps = [],
      swapOutTargets = [],
      spinNum = spin === true ? 1 : spin || 0,
      spinFunc = typeof spin === "function" ? spin : function () {
    return spinNum;
  },
      interrupted = fromState.interrupted || toState.interrupted,
      addFunc = animation[relative !== 1 ? "to" : "from"],
      v,
      p,
      endTime,
      i,
      el,
      comp,
      state,
      targets,
      finalStates,
      fromNode,
      toNode,
      run,
      a,
      b; //relative || (toState = (new FlipState(toState.targets, {props: props})).fit(toState, scale));


  for (p in toState.idLookup) {
    toNode = !toState.alt[p] ? toState.idLookup[p] : _getChangingElState(toState, fromState, p);
    el = toNode.element;
    fromNode = fromState.idLookup[p];
    fromState.alt[p] && el === fromNode.element && (fromState.alt[p].isVisible || !toNode.isVisible) && (fromNode = fromState.alt[p]);

    if (fromNode) {
      comp = {
        t: el,
        b: fromNode,
        a: toNode,
        sd: fromNode.element === el ? 0 : toNode.isVisible ? 1 : -1
      };
      comps.push(comp);

      if (comp.sd) {
        // from and to elements are different, so we need to swap them.
        if (comp.sd < 0) {
          comp.b = toNode;
          comp.a = fromNode;
        } // for swapping elements that got interrupted, we must re-record the inline styles to ensure they're not tainted. Remember, .batch() permits getState() not to force in-progress flips to their end state.


        interrupted && _recordInlineStyles(comp.b, props ? _memoizedRemoveProps[props] : _removeProps);
        fade && comps.push(comp.swap = {
          t: fromNode.element,
          b: comp.b,
          a: comp.a,
          sd: -comp.sd,
          swap: comp
        });
      }

      el._flip = fromNode.element._flip = _batch ? _batch.timeline : animation;
    } else if (toNode.isVisible) {
      comps.push({
        t: el,
        b: _copy(toNode, {
          isVisible: 1
        }),
        a: toNode,
        sd: 0,
        entering: 1
      }); // to include it in the "entering" Array and do absolute positioning if necessary

      el._flip = _batch ? _batch.timeline : animation;
    }
  }

  props && (_memoizedProps[props] || _memoizeProps(props)).forEach(function (p) {
    return tweenVars[p] = function (i) {
      return comps[i].a.props[p];
    };
  });
  comps.finalStates = finalStates = [];

  run = function run() {
    _orderByDOMDepth(comps);

    _lockBodyScroll(true); // otherwise, measurements may get thrown off when things get fit.


    var recordedStyles = [];

    for (i = 0; i < comps.length; i++) {
      comp = comps[i];
      a = comp.a;
      b = comp.b;

      if (prune && !a.isDifferent(b) && !comp.entering) {
        // only flip if things changed! Don't omit it from comps initially because that'd prevent the element from being positioned absolutely (if necessary)
        comps.splice(i--, 1);
      } else {
        el = comp.t;

        if (nested && !(comp.sd < 0) && i) {
          // moving a parent affects the position of children
          a = comp.a = a.clone({
            matrix: getGlobalMatrix(el, false, false, true)
          });
        }

        if (b.isVisible && a.isVisible) {
          if (comp.sd < 0) {
            // swapping OUT (swap direction of -1 is out)
            nested && _revertTempStyles(recordedStyles, 1); // get the ancestor elements into the final state for proper measuring

            state = new ElementState(el, props, fromState.simple);

            _fit(state, a, scale, 0, 0, state);

            state.matrix = getGlobalMatrix(el, false, false, true);
            state.bounds = el.getBoundingClientRect();
            state.css = comp.b.css;
            comp.a = a = state;
            fade && (el.style.opacity = interrupted ? b.opacity : a.opacity);
            stagger && swapOutTargets.push(el);

            if (nested) {
              _revertTempStyles(recordedStyles, 2); // now return the ancestor elements back to the before state


              recordedStyles.push(el, el.getAttribute("style"));
            }
          } else if (comp.sd > 0 && fade) {
            // swapping IN (swap direction of 1 is in)
            el.style.opacity = interrupted ? a.opacity - b.opacity : "0";
          }

          _fit(a, b, scale, props);

          nested && comp.sd < 0 && recordedStyles.push(el.getAttribute("style"));
        } else if (b.isVisible !== a.isVisible) {
          // either entering or leaving (one side is invisible)
          if (!b.isVisible) {
            // entering
            a.isVisible && entering.push(a);
            comps.splice(i--, 1);
          } else if (!a.isVisible) {
            // leaving
            b.css = a.css;
            leaving.push(b);
            comps.splice(i--, 1);
            absolute && nested && _fit(a, b, scale, props);
          }
        }

        if (!scale) {
          el.style.maxWidth = Math.max(a.width, b.width) + "px";
          el.style.maxHeight = Math.max(a.height, b.height) + "px";
          el.style.minWidth = Math.min(a.width, b.width) + "px";
          el.style.minHeight = Math.min(a.height, b.height) + "px";
        }

        nested && toggleClass && el.classList.add(toggleClass);
      }

      finalStates.push(a);
    }

    var classTargets;

    if (toggleClass) {
      classTargets = finalStates.map(function (s) {
        return s.element;
      });
      nested && classTargets.forEach(function (e) {
        return e.classList.remove(toggleClass);
      }); // there could be a delay, so don't leave the classes applied (we'll do it in a timeline callback)
    }

    _lockBodyScroll(false);

    if (scale) {
      tweenVars.scaleX = function (i) {
        return comps[i].a.scaleX;
      };

      tweenVars.scaleY = function (i) {
        return comps[i].a.scaleY;
      };
    } else {
      tweenVars.width = function (i) {
        return comps[i].a.width + "px";
      };

      tweenVars.height = function (i) {
        return comps[i].a.height + "px";
      };

      tweenVars.autoRound = vars.autoRound || false;
    }

    tweenVars.x = function (i) {
      return comps[i].a.x + "px";
    };

    tweenVars.y = function (i) {
      return comps[i].a.y + "px";
    };

    tweenVars.rotation = function (i) {
      return comps[i].a.rotation + (spin ? spinFunc(i, targets[i], targets) * 360 : 0);
    };

    tweenVars.skewX = function (i) {
      return comps[i].a.skewX;
    };

    targets = comps.map(function (c) {
      return c.t;
    });

    if (_zIndex || _zIndex === 0) {
      tweenVars.modifiers = {
        zIndex: function zIndex() {
          return _zIndex;
        }
      };
      tweenVars.zIndex = _zIndex;
      tweenVars.immediateRender = vars.immediateRender !== false;
    }

    fade && (tweenVars.opacity = function (i) {
      return comps[i].sd < 0 ? 0 : comps[i].sd > 0 ? comps[i].a.opacity : "+=0";
    });

    if (swapOutTargets.length) {
      stagger = gsap.utils.distribute(stagger);
      var dummyArray = targets.slice(swapOutTargets.length);

      tweenVars.stagger = function (i, el) {
        return stagger(~swapOutTargets.indexOf(el) ? targets.indexOf(comps[i].swap.t) : i, el, dummyArray);
      };
    } // // for testing...
    // gsap.delayedCall(vars.data ? 50 : 1, function() {
    // 	animation.eventCallback("onComplete", () => _setFinalStates(comps, !clearProps));
    // 	addFunc.call(animation, targets, tweenVars, 0).play();
    // });
    // return;


    _callbacks.forEach(function (name) {
      return vars[name] && animation.eventCallback(name, vars[name], vars[name + "Params"]);
    }); // apply callbacks to the timeline, not tweens (because "custom" timing can make multiple tweens)


    if (custom && targets.length) {
      // bust out the custom properties as their own tweens so they can use different eases, durations, etc.
      remainingProps = _copy(tweenVars, _reserved);

      if ("scale" in custom) {
        custom.scaleX = custom.scaleY = custom.scale;
        delete custom.scale;
      }

      for (p in custom) {
        v = _copy(custom[p], _fitReserved);
        v[p] = tweenVars[p];
        !("duration" in v) && "duration" in tweenVars && (v.duration = tweenVars.duration);
        v.stagger = tweenVars.stagger;
        addFunc.call(animation, targets, v, 0);
        delete remainingProps[p];
      }
    }

    if (targets.length || leaving.length || entering.length) {
      toggleClass && animation.add(function () {
        return _toggleClass(classTargets, toggleClass, animation._zTime < 0 ? "remove" : "add");
      }, 0) && !paused && _toggleClass(classTargets, toggleClass, "add");
      targets.length && addFunc.call(animation, targets, remainingProps, 0);
    }

    _handleCallback(onEnter, entering, animation);

    _handleCallback(onLeave, leaving, animation);

    var batchTl = _batch && _batch.timeline;

    if (batchTl) {
      batchTl.add(animation, 0);

      _batch._final.push(function () {
        return _setFinalStates(comps, !clearProps);
      });
    }

    endTime = animation.duration();
    animation.call(function () {
      var forward = animation.time() >= endTime;
      forward && !batchTl && _setFinalStates(comps, !clearProps);
      toggleClass && _toggleClass(classTargets, toggleClass, forward ? "remove" : "add");
    });
  };

  absoluteOnLeave && (absolute = comps.filter(function (comp) {
    return !comp.sd && !comp.a.isVisible && comp.b.isVisible;
  }).map(function (comp) {
    return comp.a.element;
  }));

  if (_batch) {
    var _batch$_abs;

    absolute && (_batch$_abs = _batch._abs).push.apply(_batch$_abs, _filterComps(comps, absolute));

    _batch._run.push(run);
  } else {
    absolute && _makeCompsAbsolute(_filterComps(comps, absolute)); // when making absolute, we must go in a very particular order so that document flow changes don't affect things. Don't make it visible if both the before and after states are invisible! There's no point, and it could make things appear visible during the flip that shouldn't be.

    run();
  }

  var anim = _batch ? _batch.timeline : animation;

  anim.revert = function () {
    return _killFlip(anim, 1, 1);
  }; // a Flip timeline should behave very different when reverting - it should actually jump to the end so that styles get cleared out.


  return anim;
},
    _interrupt = function _interrupt(tl) {
  tl.vars.onInterrupt && tl.vars.onInterrupt.apply(tl, tl.vars.onInterruptParams || []);
  tl.getChildren(true, false, true).forEach(_interrupt);
},
    _killFlip = function _killFlip(tl, action, force) {
  // action: 0 = nothing, 1 = complete, 2 = only kill (don't complete)
  if (tl && tl.progress() < 1 && (!tl.paused() || force)) {
    if (action) {
      _interrupt(tl);

      action < 2 && tl.progress(1); // we should also kill it in case it was added to a parent timeline.

      tl.kill();
    }

    return true;
  }
},
    _createLookup = function _createLookup(state) {
  var lookup = state.idLookup = {},
      alt = state.alt = {},
      elStates = state.elementStates,
      i = elStates.length,
      elState;

  while (i--) {
    elState = elStates[i];
    lookup[elState.id] ? alt[elState.id] = elState : lookup[elState.id] = elState;
  }
};

var FlipState = /*#__PURE__*/function () {
  function FlipState(targets, vars, targetsAreElementStates) {
    this.props = vars && vars.props;
    this.simple = !!(vars && vars.simple);

    if (targetsAreElementStates) {
      this.targets = _elementsFromElementStates(targets);
      this.elementStates = targets;

      _createLookup(this);
    } else {
      this.targets = _toArray(targets);
      var soft = vars && (vars.kill === false || vars.batch && !vars.kill);
      _batch && !soft && _batch._kill.push(this);
      this.update(soft || !!_batch); // when batching, don't force in-progress flips to their end; we need to do that AFTER all getStates() are called.
    }
  }

  var _proto = FlipState.prototype;

  _proto.update = function update(soft) {
    var _this = this;

    this.elementStates = this.targets.map(function (el) {
      return new ElementState(el, _this.props, _this.simple);
    });

    _createLookup(this);

    this.interrupt(soft);
    this.recordInlineStyles();
    return this;
  };

  _proto.clear = function clear() {
    this.targets.length = this.elementStates.length = 0;

    _createLookup(this);

    return this;
  };

  _proto.fit = function fit(state, scale, nested) {
    var elStatesInOrder = _orderByDOMDepth(this.elementStates.slice(0), false, true),
        toElStates = (state || this).idLookup,
        i = 0,
        fromNode,
        toNode;

    for (; i < elStatesInOrder.length; i++) {
      fromNode = elStatesInOrder[i];
      nested && (fromNode.matrix = getGlobalMatrix(fromNode.element, false, false, true)); // moving a parent affects the position of children

      toNode = toElStates[fromNode.id];
      toNode && _fit(fromNode, toNode, scale, true, 0, fromNode);
      fromNode.matrix = getGlobalMatrix(fromNode.element, false, false, true);
    }

    return this;
  };

  _proto.getProperty = function getProperty(element, property) {
    var es = this.getElementState(element) || _emptyObj;

    return (property in es ? es : es.props || _emptyObj)[property];
  };

  _proto.add = function add(state) {
    var i = state.targets.length,
        lookup = this.idLookup,
        alt = this.alt,
        index,
        es,
        es2;

    while (i--) {
      es = state.elementStates[i];
      es2 = lookup[es.id];

      if (es2 && (es.element === es2.element || alt[es.id] && alt[es.id].element === es.element)) {
        // if the flip id is already in this FlipState, replace it!
        index = this.elementStates.indexOf(es.element === es2.element ? es2 : alt[es.id]);
        this.targets.splice(index, 1, state.targets[i]);
        this.elementStates.splice(index, 1, es);
      } else {
        this.targets.push(state.targets[i]);
        this.elementStates.push(es);
      }
    }

    state.interrupted && (this.interrupted = true);
    state.simple || (this.simple = false);

    _createLookup(this);

    return this;
  };

  _proto.compare = function compare(state) {
    var l1 = state.idLookup,
        l2 = this.idLookup,
        unchanged = [],
        changed = [],
        enter = [],
        leave = [],
        targets = [],
        a1 = state.alt,
        a2 = this.alt,
        place = function place(s1, s2, el) {
      return (s1.isVisible !== s2.isVisible ? s1.isVisible ? enter : leave : s1.isVisible ? changed : unchanged).push(el) && targets.push(el);
    },
        placeIfDoesNotExist = function placeIfDoesNotExist(s1, s2, el) {
      return targets.indexOf(el) < 0 && place(s1, s2, el);
    },
        s1,
        s2,
        p,
        el,
        s1Alt,
        s2Alt,
        c1,
        c2;

    for (p in l1) {
      s1Alt = a1[p];
      s2Alt = a2[p];
      s1 = !s1Alt ? l1[p] : _getChangingElState(state, this, p);
      el = s1.element;
      s2 = l2[p];

      if (s2Alt) {
        c2 = s2.isVisible || !s2Alt.isVisible && el === s2.element ? s2 : s2Alt;
        c1 = s1Alt && !s1.isVisible && !s1Alt.isVisible && c2.element === s1Alt.element ? s1Alt : s1; //c1.element !== c2.element && c1.element === s2.element && (c2 = s2);

        if (c1.isVisible && c2.isVisible && c1.element !== c2.element) {
          // swapping, so force into "changed" array
          (c1.isDifferent(c2) ? changed : unchanged).push(c1.element, c2.element);
          targets.push(c1.element, c2.element);
        } else {
          place(c1, c2, c1.element);
        }

        s1Alt && c1.element === s1Alt.element && (s1Alt = l1[p]);
        placeIfDoesNotExist(c1.element !== s2.element && s1Alt ? s1Alt : c1, s2, s2.element);
        placeIfDoesNotExist(s1Alt && s1Alt.element === s2Alt.element ? s1Alt : c1, s2Alt, s2Alt.element);
        s1Alt && placeIfDoesNotExist(s1Alt, s2Alt.element === s1Alt.element ? s2Alt : s2, s1Alt.element);
      } else {
        !s2 ? enter.push(el) : !s2.isDifferent(s1) ? unchanged.push(el) : place(s1, s2, el);
        s1Alt && placeIfDoesNotExist(s1Alt, s2, s1Alt.element);
      }
    }

    for (p in l2) {
      if (!l1[p]) {
        leave.push(l2[p].element);
        a2[p] && leave.push(a2[p].element);
      }
    }

    return {
      changed: changed,
      unchanged: unchanged,
      enter: enter,
      leave: leave
    };
  };

  _proto.recordInlineStyles = function recordInlineStyles() {
    var props = _memoizedRemoveProps[this.props] || _removeProps,
        i = this.elementStates.length;

    while (i--) {
      _recordInlineStyles(this.elementStates[i], props);
    }
  };

  _proto.interrupt = function interrupt(soft) {
    var _this2 = this;

    // soft = DON'T force in-progress flip animations to completion (like when running a batch, we can't immediately kill flips when getting states because it could contaminate positioning and other .getState() calls that will run in the batch (we kill AFTER all the .getState() calls complete).
    var timelines = [];
    this.targets.forEach(function (t) {
      var tl = t._flip,
          foundInProgress = _killFlip(tl, soft ? 0 : 1);

      soft && foundInProgress && timelines.indexOf(tl) < 0 && tl.add(function () {
        return _this2.updateVisibility();
      });
      foundInProgress && timelines.push(tl);
    });
    !soft && timelines.length && this.updateVisibility(); // if we found an in-progress Flip animation, we must record all the values in their current state at that point BUT we should update the isVisible value AFTER pushing that flip to completion so that elements that are entering or leaving will populate those Arrays properly.

    this.interrupted || (this.interrupted = !!timelines.length);
  };

  _proto.updateVisibility = function updateVisibility() {
    this.elementStates.forEach(function (es) {
      var b = es.element.getBoundingClientRect();
      es.isVisible = !!(b.width || b.height || b.top || b.left);
      es.uncache = 1;
    });
  };

  _proto.getElementState = function getElementState(element) {
    return this.elementStates[this.targets.indexOf(_getEl(element))];
  };

  _proto.makeAbsolute = function makeAbsolute() {
    return _orderByDOMDepth(this.elementStates.slice(0), true, true).map(_makeAbsolute);
  };

  return FlipState;
}();

var ElementState = /*#__PURE__*/function () {
  function ElementState(element, props, simple) {
    if (element instanceof ElementState) {
      // allows us to essentially clone
      Object.assign(this, element, props || {});
    } else {
      this.element = element;
      this.update(props, simple);
    }
  }

  var _proto2 = ElementState.prototype;

  _proto2.isDifferent = function isDifferent(state) {
    var b1 = this.bounds,
        b2 = state.bounds;
    return b1.top !== b2.top || b1.left !== b2.left || b1.width !== b2.width || b1.height !== b2.height || !this.matrix.equals(state.matrix) || this.opacity !== state.opacity || this.props && state.props && JSON.stringify(this.props) !== JSON.stringify(state.props);
  };

  _proto2.clone = function clone(overrides) {
    return new ElementState(this, overrides);
  };

  _proto2.update = function update(props, simple) {
    var self = this,
        element = self.element,
        getProp = gsap.getProperty(element),
        cache = gsap.core.getCache(element),
        bounds = element.getBoundingClientRect(),
        bbox = element.getBBox && typeof element.getBBox === "function" && element.nodeName.toLowerCase() !== "svg" && element.getBBox(),
        m = simple ? new Matrix2D(1, 0, 0, 1, bounds.left + _getDocScrollLeft(), bounds.top + _getDocScrollTop()) : getGlobalMatrix(element, false, false, true);
    cache.uncache = 1; // in case there are CSS rules that affect the element. Example: https://gsap.com/community/forums/topic/44321-bug-on-fixed-position-using-flip/

    self.getProp = getProp;
    self.element = element;
    self.id = _getID(element);
    self.matrix = m;
    self.cache = cache;
    self.bounds = bounds;
    self.isVisible = !!(bounds.width || bounds.height || bounds.left || bounds.top);
    self.display = getProp("display");
    self.position = getProp("position");
    self.parent = element.parentNode;
    self.x = getProp("x", "px");
    self.y = getProp("y", "px");
    self.scaleX = cache.scaleX;
    self.scaleY = cache.scaleY;
    self.rotation = getProp("rotation");
    self.skewX = getProp("skewX");
    self.opacity = getProp("opacity");
    self.width = bbox ? bbox.width : _closestTenth(getProp("width", "px"), 0.04); // round up to the closest 0.1 so that text doesn't wrap.

    self.height = bbox ? bbox.height : _closestTenth(getProp("height", "px"), 0.04);
    props && _recordProps(self, _memoizedProps[props] || _memoizeProps(props));
    self.ctm = element.getCTM && element.nodeName.toLowerCase() === "svg" && _getCTM(element).inverse();
    self.simple = simple || _round(m.a) === 1 && !_round(m.b) && !_round(m.c) && _round(m.d) === 1; // allows us to speed through some other tasks if it's not scale/rotated

    self.uncache = 0;
  };

  return ElementState;
}();

var FlipAction = /*#__PURE__*/function () {
  function FlipAction(vars, batch) {
    this.vars = vars;
    this.batch = batch;
    this.states = [];
    this.timeline = batch.timeline;
  }

  var _proto3 = FlipAction.prototype;

  _proto3.getStateById = function getStateById(id) {
    var i = this.states.length;

    while (i--) {
      if (this.states[i].idLookup[id]) {
        return this.states[i];
      }
    }
  };

  _proto3.kill = function kill() {
    this.batch.remove(this);
  };

  return FlipAction;
}();

var FlipBatch = /*#__PURE__*/function () {
  function FlipBatch(id) {
    this.id = id;
    this.actions = [];
    this._kill = [];
    this._final = [];
    this._abs = [];
    this._run = [];
    this.data = {};
    this.state = new FlipState();
    this.timeline = gsap.timeline();
  }

  var _proto4 = FlipBatch.prototype;

  _proto4.add = function add(config) {
    var result = this.actions.filter(function (action) {
      return action.vars === config;
    });

    if (result.length) {
      return result[0];
    }

    result = new FlipAction(typeof config === "function" ? {
      animate: config
    } : config, this);
    this.actions.push(result);
    return result;
  };

  _proto4.remove = function remove(action) {
    var i = this.actions.indexOf(action);
    i >= 0 && this.actions.splice(i, 1);
    return this;
  };

  _proto4.getState = function getState(merge) {
    var _this3 = this;

    var prevBatch = _batch,
        prevAction = _batchAction;
    _batch = this;
    this.state.clear();
    this._kill.length = 0;
    this.actions.forEach(function (action) {
      if (action.vars.getState) {
        action.states.length = 0;
        _batchAction = action;
        action.state = action.vars.getState(action);
      }

      merge && action.states.forEach(function (s) {
        return _this3.state.add(s);
      });
    });
    _batchAction = prevAction;
    _batch = prevBatch;
    this.killConflicts();
    return this;
  };

  _proto4.animate = function animate() {
    var _this4 = this;

    var prevBatch = _batch,
        tl = this.timeline,
        i = this.actions.length,
        finalStates,
        endTime;
    _batch = this;
    tl.clear();
    this._abs.length = this._final.length = this._run.length = 0;
    this.actions.forEach(function (a) {
      a.vars.animate && a.vars.animate(a);
      var onEnter = a.vars.onEnter,
          onLeave = a.vars.onLeave,
          targets = a.targets,
          s,
          result;

      if (targets && targets.length && (onEnter || onLeave)) {
        s = new FlipState();
        a.states.forEach(function (state) {
          return s.add(state);
        });
        result = s.compare(Flip.getState(targets));
        result.enter.length && onEnter && onEnter(result.enter);
        result.leave.length && onLeave && onLeave(result.leave);
      }
    });

    _makeCompsAbsolute(this._abs);

    this._run.forEach(function (f) {
      return f();
    });

    endTime = tl.duration();
    finalStates = this._final.slice(0);
    tl.add(function () {
      if (endTime <= tl.time()) {
        // only call if moving forward in the timeline (in case it's nested in a timeline that gets reversed)
        finalStates.forEach(function (f) {
          return f();
        });

        _forEachBatch(_this4, "onComplete");
      }
    });
    _batch = prevBatch;

    while (i--) {
      this.actions[i].vars.once && this.actions[i].kill();
    }

    _forEachBatch(this, "onStart");

    tl.restart();
    return this;
  };

  _proto4.loadState = function loadState(done) {
    done || (done = function done() {
      return 0;
    });
    var queue = [];
    this.actions.forEach(function (c) {
      if (c.vars.loadState) {
        var i,
            f = function f(targets) {
          targets && (c.targets = targets);
          i = queue.indexOf(f);

          if (~i) {
            queue.splice(i, 1);
            queue.length || done();
          }
        };

        queue.push(f);
        c.vars.loadState(f);
      }
    });
    queue.length || done();
    return this;
  };

  _proto4.setState = function setState() {
    this.actions.forEach(function (c) {
      return c.targets = c.vars.setState && c.vars.setState(c);
    });
    return this;
  };

  _proto4.killConflicts = function killConflicts(soft) {
    this.state.interrupt(soft);

    this._kill.forEach(function (state) {
      return state.interrupt(soft);
    });

    return this;
  };

  _proto4.run = function run(skipGetState, merge) {
    var _this5 = this;

    if (this !== _batch) {
      skipGetState || this.getState(merge);
      this.loadState(function () {
        if (!_this5._killed) {
          _this5.setState();

          _this5.animate();
        }
      });
    }

    return this;
  };

  _proto4.clear = function clear(stateOnly) {
    this.state.clear();
    stateOnly || (this.actions.length = 0);
  };

  _proto4.getStateById = function getStateById(id) {
    var i = this.actions.length,
        s;

    while (i--) {
      s = this.actions[i].getStateById(id);

      if (s) {
        return s;
      }
    }

    return this.state.idLookup[id] && this.state;
  };

  _proto4.kill = function kill() {
    this._killed = 1;
    this.clear();
    delete _batchLookup[this.id];
  };

  return FlipBatch;
}();

export var Flip = /*#__PURE__*/function () {
  function Flip() {}

  Flip.getState = function getState(targets, vars) {
    var state = _parseState(targets, vars);

    _batchAction && _batchAction.states.push(state);
    vars && vars.batch && Flip.batch(vars.batch).state.add(state);
    return state;
  };

  Flip.from = function from(state, vars) {
    vars = vars || {};
    "clearProps" in vars || (vars.clearProps = true);
    return _fromTo(state, _parseState(vars.targets || state.targets, {
      props: vars.props || state.props,
      simple: vars.simple,
      kill: !!vars.kill
    }), vars, -1);
  };

  Flip.to = function to(state, vars) {
    return _fromTo(state, _parseState(vars.targets || state.targets, {
      props: vars.props || state.props,
      simple: vars.simple,
      kill: !!vars.kill
    }), vars, 1);
  };

  Flip.fromTo = function fromTo(fromState, toState, vars) {
    return _fromTo(fromState, toState, vars);
  };

  Flip.fit = function fit(fromEl, toEl, vars) {
    var v = vars ? _copy(vars, _fitReserved) : {},
        _ref = vars || v,
        absolute = _ref.absolute,
        scale = _ref.scale,
        getVars = _ref.getVars,
        props = _ref.props,
        runBackwards = _ref.runBackwards,
        onComplete = _ref.onComplete,
        simple = _ref.simple,
        fitChild = vars && vars.fitChild && _getEl(vars.fitChild),
        before = _parseElementState(toEl, props, simple, fromEl),
        after = _parseElementState(fromEl, 0, simple, before),
        inlineProps = props ? _memoizedRemoveProps[props] : _removeProps,
        ctx = gsap.context();

    props && _applyProps(v, before.props);

    _recordInlineStyles(after, inlineProps);

    if (runBackwards) {
      "immediateRender" in v || (v.immediateRender = true);

      v.onComplete = function () {
        _applyInlineStyles(after);

        onComplete && onComplete.apply(this, arguments);
      };
    }

    absolute && _makeAbsolute(after, before);
    v = _fit(after, before, scale || fitChild, !v.duration && props, fitChild, v.duration || getVars ? v : 0);
    typeof vars === "object" && "zIndex" in vars && (v.zIndex = vars.zIndex);
    ctx && !getVars && ctx.add(function () {
      return function () {
        return _applyInlineStyles(after);
      };
    });
    return getVars ? v : v.duration ? gsap.to(after.element, v) : null;
  };

  Flip.makeAbsolute = function makeAbsolute(targetsOrStates, vars) {
    return (targetsOrStates instanceof FlipState ? targetsOrStates : new FlipState(targetsOrStates, vars)).makeAbsolute();
  };

  Flip.batch = function batch(id) {
    id || (id = "default");
    return _batchLookup[id] || (_batchLookup[id] = new FlipBatch(id));
  };

  Flip.killFlipsOf = function killFlipsOf(targets, complete) {
    (targets instanceof FlipState ? targets.targets : _toArray(targets)).forEach(function (t) {
      return t && _killFlip(t._flip, complete !== false ? 1 : 2);
    });
  };

  Flip.isFlipping = function isFlipping(target) {
    var f = Flip.getByTarget(target);
    return !!f && f.isActive();
  };

  Flip.getByTarget = function getByTarget(target) {
    return (_getEl(target) || _emptyObj)._flip;
  };

  Flip.getElementState = function getElementState(target, props) {
    return new ElementState(_getEl(target), props);
  };

  Flip.convertCoordinates = function convertCoordinates(fromElement, toElement, point) {
    var m = getGlobalMatrix(toElement, true, true).multiply(getGlobalMatrix(fromElement));
    return point ? m.apply(point) : m;
  };

  Flip.register = function register(core) {
    _body = typeof document !== "undefined" && document.body;

    if (_body) {
      gsap = core;

      _setDoc(_body);

      _toArray = gsap.utils.toArray;
      _getStyleSaver = gsap.core.getStyleSaver;
      var snap = gsap.utils.snap(0.1);

      _closestTenth = function _closestTenth(value, add) {
        return snap(parseFloat(value) + add);
      };
    }
  };

  return Flip;
}();
Flip.version = "3.15.0"; // function whenImagesLoad(el, func) {
// 	let pending = [],
// 		onLoad = e => {
// 			pending.splice(pending.indexOf(e.target), 1);
// 			e.target.removeEventListener("load", onLoad);
// 			pending.length || func();
// 		};
// 	gsap.utils.toArray(el.tagName.toLowerCase() === "img" ? el : el.querySelectorAll("img")).forEach(img => img.complete || img.addEventListener("load", onLoad) || pending.push(img));
// 	pending.length || func();
// }

typeof window !== "undefined" && window.gsap && window.gsap.registerPlugin(Flip);
export { Flip as default };