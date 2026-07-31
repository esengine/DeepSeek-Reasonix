/*!
 * InertiaPlugin 3.15.0
 * https://gsap.com
 *
 * @license Copyright 2008-2026, GreenSock. All rights reserved.
 * Subject to the terms at https://gsap.com/standard-license
 * @author: Jack Doyle, jack@greensock.com
*/

/* eslint-disable */
import { VelocityTracker } from "./utils/VelocityTracker.js";

var gsap,
    _coreInitted,
    _parseEase,
    _toArray,
    _power3,
    _config,
    _getUnit,
    PropTween,
    _getCache,
    _checkPointRatio,
    _clamp,
    _processingVars,
    _getStyleSaver,
    _reverting,
    _getTracker = VelocityTracker.getByTarget,
    _getGSAP = function _getGSAP() {
  return gsap || typeof window !== "undefined" && (gsap = window.gsap) && gsap.registerPlugin && gsap;
},
    _isString = function _isString(value) {
  return typeof value === "string";
},
    _isNumber = function _isNumber(value) {
  return typeof value === "number";
},
    _isObject = function _isObject(value) {
  return typeof value === "object";
},
    _isFunction = function _isFunction(value) {
  return typeof value === "function";
},
    _bonusValidated = 1,
    //<name>InertiaPlugin</name>
_isArray = Array.isArray,
    _emptyFunc = function _emptyFunc(p) {
  return p;
},
    _bigNum = 1e10,
    _tinyNum = 1 / _bigNum,
    _checkPoint = 0.05,
    _round = function _round(value) {
  return Math.round(value * 10000) / 10000;
},
    _extend = function _extend(obj, defaults, exclude) {
  for (var p in defaults) {
    if (!(p in obj) && p !== exclude) {
      obj[p] = defaults[p];
    }
  }

  return obj;
},
    _deepClone = function _deepClone(obj) {
  var copy = {},
      p,
      v;

  for (p in obj) {
    copy[p] = _isObject(v = obj[p]) && !_isArray(v) ? _deepClone(v) : v;
  }

  return copy;
},
    _getClosest = function _getClosest(n, values, max, min, radius) {
  var i = values.length,
      closest = 0,
      absDif = _bigNum,
      val,
      dif,
      p,
      dist;

  if (_isObject(n)) {
    while (i--) {
      val = values[i];
      dif = 0;

      for (p in n) {
        dist = val[p] - n[p];
        dif += dist * dist;
      }

      if (dif < absDif) {
        closest = i;
        absDif = dif;
      }
    }

    if ((radius || _bigNum) < _bigNum && radius < Math.sqrt(absDif)) {
      return n;
    }
  } else {
    while (i--) {
      val = values[i];
      dif = val - n;

      if (dif < 0) {
        dif = -dif;
      }

      if (dif < absDif && val >= min && val <= max) {
        closest = i;
        absDif = dif;
      }
    }
  }

  return values[closest];
},
    _parseEnd = function _parseEnd(curProp, end, max, min, name, radius, velocity) {
  if (curProp.end === "auto") {
    return curProp;
  }

  var endVar = curProp.end,
      adjustedEnd,
      p;
  max = isNaN(max) ? _bigNum : max;
  min = isNaN(min) ? -_bigNum : min;

  if (_isObject(end)) {
    //for objects, like {x, y} where they're linked and we must pass an object to the function or find the closest value in an array.
    adjustedEnd = end.calculated ? end : (_isFunction(endVar) ? endVar(end, velocity) : _getClosest(end, endVar, max, min, radius)) || end;

    if (!end.calculated) {
      for (p in adjustedEnd) {
        end[p] = adjustedEnd[p];
      }

      end.calculated = true;
    }

    adjustedEnd = adjustedEnd[name];
  } else {
    adjustedEnd = _isFunction(endVar) ? endVar(end, velocity) : _isArray(endVar) ? _getClosest(end, endVar, max, min, radius) : parseFloat(endVar);
  }

  if (adjustedEnd > max) {
    adjustedEnd = max;
  } else if (adjustedEnd < min) {
    adjustedEnd = min;
  }

  return {
    max: adjustedEnd,
    min: adjustedEnd,
    unitFactor: curProp.unitFactor
  };
},
    _getNumOrDefault = function _getNumOrDefault(vars, property, defaultValue) {
  return isNaN(vars[property]) ? defaultValue : +vars[property];
},
    _calculateChange = function _calculateChange(velocity, duration) {
  return duration * _checkPoint * velocity / _checkPointRatio;
},
    _calculateDuration = function _calculateDuration(start, end, velocity) {
  return Math.abs((end - start) * _checkPointRatio / velocity / _checkPoint);
},
    _reservedProps = {
  resistance: 1,
  checkpoint: 1,
  preventOvershoot: 1,
  linkedProps: 1,
  radius: 1,
  duration: 1
},
    _processLinkedProps = function _processLinkedProps(target, vars, getVal, resistance) {
  if (vars.linkedProps) {
    //when there are linkedProps (typically "x,y" where snapping has to factor in multiple properties, we must first populate an object with all of those end values, then feed it to the function that make any necessary alterations. So the point of this first loop is to simply build an object (like {x:100, y:204.5}) for feeding into that function which we'll do later in the "real" loop.
    var linkedPropNames = vars.linkedProps.split(","),
        linkedProps = {},
        i,
        p,
        curProp,
        curVelocity,
        tracker,
        curDuration;

    for (i = 0; i < linkedPropNames.length; i++) {
      p = linkedPropNames[i];
      curProp = vars[p];

      if (curProp) {
        if (_isNumber(curProp.velocity)) {
          curVelocity = curProp.velocity;
        } else {
          tracker = tracker || _getTracker(target);
          curVelocity = tracker && tracker.isTracking(p) ? tracker.get(p) : 0;
        }

        curDuration = Math.abs(curVelocity / _getNumOrDefault(curProp, "resistance", resistance));
        linkedProps[p] = parseFloat(getVal(target, p)) + _calculateChange(curVelocity, curDuration);
      }
    }

    return linkedProps;
  }
},
    _calculateTweenDuration = function _calculateTweenDuration(target, vars, maxDuration, minDuration, overshootTolerance, recordEnd) {
  if (maxDuration === void 0) {
    maxDuration = 10;
  }

  if (minDuration === void 0) {
    minDuration = 0.2;
  }

  if (overshootTolerance === void 0) {
    overshootTolerance = 1;
  }

  if (recordEnd === void 0) {
    recordEnd = 0;
  }

  _isString(target) && (target = _toArray(target)[0]);

  if (!target) {
    return 0;
  }

  var duration = 0,
      clippedDuration = _bigNum,
      inertiaVars = vars.inertia || vars,
      getVal = _getCache(target).get,
      resistance = _getNumOrDefault(inertiaVars, "resistance", _config.resistance),
      p,
      curProp,
      curDuration,
      curVelocity,
      curVal,
      end,
      curClippedDuration,
      tracker,
      unitFactor,
      linkedProps; //when there are linkedProps (typically "x,y" where snapping has to factor in multiple properties, we must first populate an object with all of those end values, then feed it to the function that make any necessary alterations. So the point of this first loop is to simply build an object (like {x:100, y:204.5}) for feeding into that function which we'll do later in the "real" loop.


  linkedProps = _processLinkedProps(target, inertiaVars, getVal, resistance);

  for (p in inertiaVars) {
    if (!_reservedProps[p]) {
      curProp = inertiaVars[p];

      if (!_isObject(curProp)) {
        tracker = tracker || _getTracker(target);

        if (tracker && tracker.isTracking(p)) {
          curProp = _isNumber(curProp) ? {
            velocity: curProp
          } : {
            velocity: tracker.get(p)
          }; //if we're tracking this property, we should use the tracking velocity and then use the numeric value that was passed in as the min and max so that it tweens exactly there.
        } else {
          curVelocity = +curProp || 0;
          curDuration = Math.abs(curVelocity / resistance);
        }
      }

      if (_isObject(curProp)) {
        if (_isNumber(curProp.velocity)) {
          curVelocity = curProp.velocity;
        } else {
          tracker = tracker || _getTracker(target);
          curVelocity = tracker && tracker.isTracking(p) ? tracker.get(p) : 0;
        }

        curDuration = _clamp(minDuration, maxDuration, Math.abs(curVelocity / _getNumOrDefault(curProp, "resistance", resistance)));
        curVal = parseFloat(getVal(target, p)) || 0;
        end = curVal + _calculateChange(curVelocity, curDuration);

        if ("end" in curProp) {
          curProp = _parseEnd(curProp, linkedProps && p in linkedProps ? linkedProps : end, curProp.max, curProp.min, p, inertiaVars.radius, curVelocity);

          if (recordEnd) {
            _processingVars === vars && (_processingVars = inertiaVars = _deepClone(vars));
            inertiaVars[p] = _extend(curProp, inertiaVars[p], "end");
          }
        }

        if ("max" in curProp && end > +curProp.max + _tinyNum) {
          unitFactor = curProp.unitFactor || _config.unitFactors[p] || 1; //some values are measured in special units like radians in which case our thresholds need to be adjusted accordingly.
          //if the value is already exceeding the max or the velocity is too low, the duration can end up being uncomfortably long but in most situations, users want the snapping to occur relatively quickly (0.75 seconds), so we implement a cap here to make things more intuitive. If the max and min match, it means we're animating to a particular value and we don't want to shorten the time unless the velocity is really slow. Example: a rotation where the start and natural end value are less than the snapping spot, but the natural end is pretty close to the snap.

          curClippedDuration = curVal > curProp.max && curProp.min !== curProp.max || curVelocity * unitFactor > -15 && curVelocity * unitFactor < 45 ? minDuration + (maxDuration - minDuration) * 0.1 : _calculateDuration(curVal, curProp.max, curVelocity);

          if (curClippedDuration + overshootTolerance < clippedDuration) {
            clippedDuration = curClippedDuration + overshootTolerance;
          }
        } else if ("min" in curProp && end < +curProp.min - _tinyNum) {
          unitFactor = curProp.unitFactor || _config.unitFactors[p] || 1; //some values are measured in special units like radians in which case our thresholds need to be adjusted accordingly.
          //if the value is already exceeding the min or if the velocity is too low, the duration can end up being uncomfortably long but in most situations, users want the snapping to occur relatively quickly (0.75 seconds), so we implement a cap here to make things more intuitive.

          curClippedDuration = curVal < curProp.min && curProp.min !== curProp.max || curVelocity * unitFactor > -45 && curVelocity * unitFactor < 15 ? minDuration + (maxDuration - minDuration) * 0.1 : _calculateDuration(curVal, curProp.min, curVelocity);

          if (curClippedDuration + overshootTolerance < clippedDuration) {
            clippedDuration = curClippedDuration + overshootTolerance;
          }
        }

        curClippedDuration > duration && (duration = curClippedDuration);
      }

      curDuration > duration && (duration = curDuration);
    }
  }

  duration > clippedDuration && (duration = clippedDuration);
  return duration > maxDuration ? maxDuration : duration < minDuration ? minDuration : duration;
},
    _initCore = function _initCore() {
  gsap = _getGSAP();

  if (gsap) {
    _parseEase = gsap.parseEase;
    _toArray = gsap.utils.toArray;
    _getUnit = gsap.utils.getUnit;
    _getCache = gsap.core.getCache;
    _clamp = gsap.utils.clamp;
    _getStyleSaver = gsap.core.getStyleSaver;

    _reverting = gsap.core.reverting || function () {};

    _power3 = _parseEase("power3");
    _checkPointRatio = _power3(0.05);
    PropTween = gsap.core.PropTween;
    gsap.config({
      resistance: 100,
      unitFactors: {
        time: 1000,
        totalTime: 1000,
        progress: 1000,
        totalProgress: 1000
      }
    });
    _config = gsap.config();
    gsap.registerPlugin(VelocityTracker);
    _coreInitted = 1;
  }
};

export var InertiaPlugin = {
  version: "3.15.0",
  name: "inertia",
  register: function register(core) {
    gsap = core;

    _initCore();
  },
  init: function init(target, vars, tween, index, targets) {
    _coreInitted || _initCore();

    var tracker = _getTracker(target);

    if (vars === "auto") {
      if (!tracker) {
        console.warn("No inertia tracking on " + target + ". InertiaPlugin.track(target) first.");
        return;
      }

      vars = tracker.getAll();
    }

    this.styles = _getStyleSaver && typeof target.style === "object" && _getStyleSaver(target);
    this.target = target;
    this.tween = tween;
    _processingVars = vars; // gets swapped inside _calculateTweenDuration() if there's a function-based value encountered (to avoid double-calling it)

    var cache = target._gsap,
        getVal = cache.get,
        dur = vars.duration,
        durIsObj = _isObject(dur),
        preventOvershoot = vars.preventOvershoot || durIsObj && dur.overshoot === 0,
        resistance = _getNumOrDefault(vars, "resistance", _config.resistance),
        duration = _isNumber(dur) ? dur : _calculateTweenDuration(target, vars, durIsObj && dur.max || 10, durIsObj && dur.min || 0.2, durIsObj && "overshoot" in dur ? +dur.overshoot : preventOvershoot ? 0 : 1, true),
        p,
        curProp,
        curVal,
        unit,
        velocity,
        change1,
        end,
        change2,
        linkedProps;

    vars = _processingVars;
    _processingVars = 0; //when there are linkedProps (typically "x,y" where snapping has to factor in multiple properties, we must first populate an object with all of those end values, then feed it to the function that make any necessary alterations. So the point of this first loop is to simply build an object (like {x:100, y:204.5}) for feeding into that function which we'll do later in the "real" loop.

    linkedProps = _processLinkedProps(target, vars, getVal, resistance);

    for (p in vars) {
      if (!_reservedProps[p]) {
        curProp = vars[p];
        _isFunction(curProp) && (curProp = curProp(index, target, targets));

        if (_isNumber(curProp)) {
          velocity = curProp;
        } else if (_isObject(curProp) && !isNaN(curProp.velocity)) {
          velocity = +curProp.velocity;
        } else {
          if (tracker && tracker.isTracking(p)) {
            velocity = tracker.get(p);
          } else {
            console.warn("ERROR: No velocity was defined for " + target + " property: " + p);
          }
        }

        change1 = _calculateChange(velocity, duration);
        change2 = 0;
        curVal = getVal(target, p);
        unit = _getUnit(curVal);
        curVal = parseFloat(curVal);

        if (_isObject(curProp)) {
          end = curVal + change1;

          if ("end" in curProp) {
            curProp = _parseEnd(curProp, linkedProps && p in linkedProps ? linkedProps : end, curProp.max, curProp.min, p, vars.radius, velocity);
          }

          if ("max" in curProp && +curProp.max < end) {
            if (preventOvershoot || curProp.preventOvershoot) {
              change1 = curProp.max - curVal;
            } else {
              change2 = curProp.max - curVal - change1;
            }
          } else if ("min" in curProp && +curProp.min > end) {
            if (preventOvershoot || curProp.preventOvershoot) {
              change1 = curProp.min - curVal;
            } else {
              change2 = curProp.min - curVal - change1;
            }
          }
        }

        this._props.push(p);

        this.styles && this.styles.save(p);
        this._pt = new PropTween(this._pt, target, p, curVal, 0, _emptyFunc, 0, cache.set(target, p, this));
        this._pt.u = unit || 0;
        this._pt.c1 = change1;
        this._pt.c2 = change2;
      }
    }

    tween.duration(duration);
    return _bonusValidated;
  },
  render: function render(ratio, data) {
    var pt = data._pt;
    ratio = _power3(data.tween._time / data.tween._dur);

    if (ratio || !_reverting()) {
      while (pt) {
        pt.set(pt.t, pt.p, _round(pt.s + pt.c1 * ratio + pt.c2 * ratio * ratio) + pt.u, pt.d, ratio);
        pt = pt._next;
      }
    } else {
      data.styles.revert();
    }
  }
};
"track,untrack,isTracking,getVelocity,getByTarget".split(",").forEach(function (name) {
  return InertiaPlugin[name] = VelocityTracker[name];
});
_getGSAP() && gsap.registerPlugin(InertiaPlugin);
export { InertiaPlugin as default, VelocityTracker };