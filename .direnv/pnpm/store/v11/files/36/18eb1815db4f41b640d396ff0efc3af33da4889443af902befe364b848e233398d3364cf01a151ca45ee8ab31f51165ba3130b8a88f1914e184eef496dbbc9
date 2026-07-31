(function (global, factory) {
	typeof exports === 'object' && typeof module !== 'undefined' ? factory(exports) :
	typeof define === 'function' && define.amd ? define(['exports'], factory) :
	(global = global || self, factory(global.window = global.window || {}));
}(this, (function (exports) { 'use strict';

	var gsap,
	    _coreInitted,
	    _toArray,
	    _getUnit,
	    _first,
	    _ticker,
	    _time1,
	    _time2,
	    _getCache,
	    _getGSAP = function _getGSAP() {
	  return gsap || typeof window !== "undefined" && (gsap = window.gsap);
	},
	    _lookup = {},
	    _round = function _round(value) {
	  return Math.round(value * 10000) / 10000;
	},
	    _getID = function _getID(target) {
	  return _getCache(target).id;
	},
	    _getByTarget = function _getByTarget(target) {
	  return _lookup[_getID(typeof target === "string" ? _toArray(target)[0] : target)];
	},
	    _onTick = function _onTick(time) {
	  var pt = _first,
	      val;

	  if (time - _time1 >= 0.05) {
	    _time2 = _time1;
	    _time1 = time;

	    while (pt) {
	      val = pt.g(pt.t, pt.p);

	      if (val !== pt.v1 || time - pt.t1 > 0.2) {
	        pt.v2 = pt.v1;
	        pt.v1 = val;
	        pt.t2 = pt.t1;
	        pt.t1 = time;
	      }

	      pt = pt._next;
	    }
	  }
	},
	    _types = {
	  deg: 360,
	  rad: Math.PI * 2
	},
	    _initCore = function _initCore() {
	  gsap = _getGSAP();

	  if (gsap) {
	    _toArray = gsap.utils.toArray;
	    _getUnit = gsap.utils.getUnit;
	    _getCache = gsap.core.getCache;
	    _ticker = gsap.ticker;
	    _coreInitted = 1;
	  }
	};

	var PropTracker = function PropTracker(target, property, type, next) {
	  this.t = target;
	  this.p = property;
	  this.g = target._gsap.get;
	  this.rCap = _types[type || _getUnit(this.g(target, property))];
	  this.v1 = this.v2 = this.g(target, property);
	  this.t1 = this.t2 = _ticker.time;

	  if (next) {
	    this._next = next;
	    next._prev = this;
	  }
	};

	var VelocityTracker = function () {
	  function VelocityTracker(target, property) {
	    _coreInitted || _initCore();
	    this.target = _toArray(target)[0];
	    _lookup[_getID(this.target)] = this;
	    this._props = {};
	    property && this.add(property);
	  }

	  VelocityTracker.register = function register(core) {
	    gsap = core;

	    _initCore();
	  };

	  var _proto = VelocityTracker.prototype;

	  _proto.get = function get(property, skipRecentTick) {
	    var pt = this._props[property] || console.warn("Not tracking " + property + " velocity."),
	        val,
	        dif,
	        rotationCap;
	    val = parseFloat(skipRecentTick ? pt.v1 : pt.g(pt.t, pt.p));
	    dif = val - parseFloat(pt.v2);
	    rotationCap = pt.rCap;

	    if (rotationCap) {
	      dif = dif % rotationCap;

	      if (dif !== dif % (rotationCap / 2)) {
	        dif = dif < 0 ? dif + rotationCap : dif - rotationCap;
	      }
	    }

	    return _round(dif / ((skipRecentTick ? pt.t1 : _ticker.time) - pt.t2));
	  };

	  _proto.getAll = function getAll() {
	    var result = {},
	        props = this._props,
	        p;

	    for (p in props) {
	      result[p] = this.get(p);
	    }

	    return result;
	  };

	  _proto.isTracking = function isTracking(property) {
	    return property in this._props;
	  };

	  _proto.add = function add(property, type) {
	    var pt = this._props[property];

	    if (pt) {
	      pt.v1 = pt.v2 = pt.g(pt.t, pt.p);
	      pt.t1 = pt.t2 = _ticker.time;
	    } else {
	      if (!_first) {
	        _ticker.add(_onTick);

	        _time1 = _time2 = _ticker.time;
	      }

	      _first = this._props[property] = new PropTracker(this.target, property, type, _first);
	    }
	  };

	  _proto.remove = function remove(property) {
	    var pt = this._props[property],
	        prev,
	        next;

	    if (pt) {
	      prev = pt._prev;
	      next = pt._next;

	      if (prev) {
	        prev._next = next;
	      }

	      if (next) {
	        next._prev = prev;
	      } else if (_first === pt) {
	        _ticker.remove(_onTick);

	        _first = 0;
	      }

	      delete this._props[property];
	    }
	  };

	  _proto.kill = function kill(shallow) {
	    for (var p in this._props) {
	      this.remove(p);
	    }

	    if (!shallow) {
	      delete _lookup[_getID(this.target)];
	    }
	  };

	  VelocityTracker.track = function track(targets, properties, types) {
	    _coreInitted || _initCore();

	    var result = [],
	        targs = _toArray(targets),
	        a = properties.split(","),
	        t = (types || "").split(","),
	        i = targs.length,
	        tracker,
	        j;

	    while (i--) {
	      tracker = _getByTarget(targs[i]) || new VelocityTracker(targs[i]);
	      j = a.length;

	      while (j--) {
	        tracker.add(a[j], t[j] || t[0]);
	      }

	      result.push(tracker);
	    }

	    return result;
	  };

	  VelocityTracker.untrack = function untrack(targets, properties) {
	    var props = properties && properties.split(",");

	    _toArray(targets).forEach(function (target) {
	      var tracker = _getByTarget(target);

	      if (tracker) {
	        props ? props.forEach(function (p) {
	          return tracker.remove(p);
	        }) : tracker.kill(1);
	      }
	    });
	  };

	  VelocityTracker.isTracking = function isTracking(target, property) {
	    var tracker = _getByTarget(target);

	    return tracker && tracker.isTracking(property);
	  };

	  VelocityTracker.getVelocity = function getVelocity(target, property) {
	    var tracker = _getByTarget(target);

	    return !tracker || !tracker.isTracking(property) ? console.warn("Not tracking velocity of " + property) : tracker.get(property);
	  };

	  return VelocityTracker;
	}();
	VelocityTracker.getByTarget = _getByTarget;
	_getGSAP() && gsap.registerPlugin(VelocityTracker);

	/*!
	 * InertiaPlugin 3.15.0
	 * https://gsap.com
	 *
	 * @license Copyright 2008-2026, GreenSock. All rights reserved.
	 * Subject to the terms at https://gsap.com/standard-license
	 * @author: Jack Doyle, jack@greensock.com
	*/

	var gsap$1,
	    _coreInitted$1,
	    _parseEase,
	    _toArray$1,
	    _power3,
	    _config,
	    _getUnit$1,
	    PropTween,
	    _getCache$1,
	    _checkPointRatio,
	    _clamp,
	    _processingVars,
	    _getStyleSaver,
	    _reverting,
	    _getTracker = VelocityTracker.getByTarget,
	    _getGSAP$1 = function _getGSAP() {
	  return gsap$1 || typeof window !== "undefined" && (gsap$1 = window.gsap) && gsap$1.registerPlugin && gsap$1;
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
	    _isArray = Array.isArray,
	    _emptyFunc = function _emptyFunc(p) {
	  return p;
	},
	    _bigNum = 1e10,
	    _tinyNum = 1 / _bigNum,
	    _checkPoint = 0.05,
	    _round$1 = function _round(value) {
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

	  _isString(target) && (target = _toArray$1(target)[0]);

	  if (!target) {
	    return 0;
	  }

	  var duration = 0,
	      clippedDuration = _bigNum,
	      inertiaVars = vars.inertia || vars,
	      getVal = _getCache$1(target).get,
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
	      linkedProps;

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
	          };
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
	          unitFactor = curProp.unitFactor || _config.unitFactors[p] || 1;
	          curClippedDuration = curVal > curProp.max && curProp.min !== curProp.max || curVelocity * unitFactor > -15 && curVelocity * unitFactor < 45 ? minDuration + (maxDuration - minDuration) * 0.1 : _calculateDuration(curVal, curProp.max, curVelocity);

	          if (curClippedDuration + overshootTolerance < clippedDuration) {
	            clippedDuration = curClippedDuration + overshootTolerance;
	          }
	        } else if ("min" in curProp && end < +curProp.min - _tinyNum) {
	          unitFactor = curProp.unitFactor || _config.unitFactors[p] || 1;
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
	    _initCore$1 = function _initCore() {
	  gsap$1 = _getGSAP$1();

	  if (gsap$1) {
	    _parseEase = gsap$1.parseEase;
	    _toArray$1 = gsap$1.utils.toArray;
	    _getUnit$1 = gsap$1.utils.getUnit;
	    _getCache$1 = gsap$1.core.getCache;
	    _clamp = gsap$1.utils.clamp;
	    _getStyleSaver = gsap$1.core.getStyleSaver;

	    _reverting = gsap$1.core.reverting || function () {};

	    _power3 = _parseEase("power3");
	    _checkPointRatio = _power3(0.05);
	    PropTween = gsap$1.core.PropTween;
	    gsap$1.config({
	      resistance: 100,
	      unitFactors: {
	        time: 1000,
	        totalTime: 1000,
	        progress: 1000,
	        totalProgress: 1000
	      }
	    });
	    _config = gsap$1.config();
	    gsap$1.registerPlugin(VelocityTracker);
	    _coreInitted$1 = 1;
	  }
	};

	var InertiaPlugin = {
	  version: "3.15.0",
	  name: "inertia",
	  register: function register(core) {
	    gsap$1 = core;

	    _initCore$1();
	  },
	  init: function init(target, vars, tween, index, targets) {
	    _coreInitted$1 || _initCore$1();

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
	    _processingVars = vars;

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
	    _processingVars = 0;
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
	        unit = _getUnit$1(curVal);
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
	        pt.set(pt.t, pt.p, _round$1(pt.s + pt.c1 * ratio + pt.c2 * ratio * ratio) + pt.u, pt.d, ratio);
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
	_getGSAP$1() && gsap$1.registerPlugin(InertiaPlugin);

	exports.InertiaPlugin = InertiaPlugin;
	exports.VelocityTracker = VelocityTracker;
	exports.default = InertiaPlugin;

	Object.defineProperty(exports, '__esModule', { value: true });

})));
