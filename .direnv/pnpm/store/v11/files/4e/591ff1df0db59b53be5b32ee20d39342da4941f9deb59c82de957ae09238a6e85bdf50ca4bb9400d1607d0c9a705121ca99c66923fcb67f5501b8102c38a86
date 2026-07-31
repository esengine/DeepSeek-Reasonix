/*!
 * VelocityTracker: 3.15.0
 * https://gsap.com
 *
 * Copyright 2008-2026, GreenSock. All rights reserved.
 * Subject to the terms at https://gsap.com/standard-license
 * @author: Jack Doyle, jack@greensock.com
*/

/* eslint-disable */
// @ts-nocheck
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
      val; //if the frame rate is too high, we won't be able to track the velocity as well, so only update the values about 20 times per second

  if (time - _time1 >= 0.05) {
    _time2 = _time1;
    _time1 = time;

    while (pt) {
      val = pt.g(pt.t, pt.p);

      if (val !== pt.v1 || time - pt.t1 > 0.2) {
        //use a threshold of 0.2 seconds for zeroing-out velocity. If we only use 0.05 and things update slightly slower, like some Android devices dispatch "touchmove" events sluggishly so 2 or 3 ticks of the gsap.ticker may elapse inbetween, thus it may appear like the object is not moving but it actually is but it's not updating as frequently. A threshold of 0.2 seconds seems to be a good balance. We want to update things frequently (0.05 seconds) when they're moving so that we can respond to fast motions accurately, but we want to be more resistant to go back to a zero velocity.
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
  this.rCap = _types[type || _getUnit(this.g(target, property))]; //rotational cap (for degrees, "deg", it's 360 and for radians, "rad", it's Math.PI * 2)

  this.v1 = this.v2 = this.g(target, property);
  this.t1 = this.t2 = _ticker.time;

  if (next) {
    this._next = next;
    next._prev = this;
  }
};

export var VelocityTracker = /*#__PURE__*/function () {
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
      //rotational values need special interpretation so that if, for example, they go from 179 to -178 degrees it is interpreted as a change of 3 instead of -357.
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
      // reset
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
export { VelocityTracker as default };