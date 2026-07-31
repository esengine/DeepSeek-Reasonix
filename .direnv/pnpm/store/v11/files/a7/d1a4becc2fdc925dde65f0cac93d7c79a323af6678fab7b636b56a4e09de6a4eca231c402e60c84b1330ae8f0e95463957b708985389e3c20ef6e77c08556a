(function (global, factory) {
	typeof exports === 'object' && typeof module !== 'undefined' ? factory(exports) :
	typeof define === 'function' && define.amd ? define(['exports'], factory) :
	(global = global || self, factory(global.window = global.window || {}));
}(this, (function (exports) { 'use strict';

	/*!
	 * PhysicsPropsPlugin 3.15.0
	 * https://gsap.com
	 *
	 * @license Copyright 2008-2026, GreenSock. All rights reserved.
	 * Subject to the terms at https://gsap.com/standard-license
	 * @author: Jack Doyle, jack@greensock.com
	*/
	var gsap,
	    _coreInitted,
	    _getUnit,
	    _getStyleSaver,
	    _reverting,
	    _getGSAP = function _getGSAP() {
	  return gsap || typeof window !== "undefined" && (gsap = window.gsap) && gsap.registerPlugin && gsap;
	},
	    _round = function _round(value) {
	  return Math.round(value * 10000) / 10000;
	},
	    _initCore = function _initCore(core) {
	  gsap = core || _getGSAP();

	  if (!_coreInitted) {
	    _getUnit = gsap.utils.getUnit;
	    _getStyleSaver = gsap.core.getStyleSaver;

	    _reverting = gsap.core.reverting || function () {};

	    _coreInitted = 1;
	  }
	};

	var PhysicsProp = function PhysicsProp(target, p, velocity, acceleration, friction, stepsPerTimeUnit) {
	  var cache = target._gsap,
	      curVal = cache.get(target, p);
	  this.p = p;
	  this.set = cache.set(target, p);
	  this.s = this.val = parseFloat(curVal);
	  this.u = _getUnit(curVal) || 0;
	  this.vel = velocity || 0;
	  this.v = this.vel / stepsPerTimeUnit;

	  if (acceleration || acceleration === 0) {
	    this.acc = acceleration;
	    this.a = this.acc / (stepsPerTimeUnit * stepsPerTimeUnit);
	  } else {
	    this.acc = this.a = 0;
	  }

	  this.fr = 1 - (friction || 0);
	};

	var PhysicsPropsPlugin = {
	  version: "3.15.0",
	  name: "physicsProps",
	  register: _initCore,
	  init: function init(target, value, tween) {
	    _coreInitted || _initCore();
	    var data = this,
	        p;
	    data.styles = _getStyleSaver && _getStyleSaver(target);
	    data.target = target;
	    data.tween = tween;
	    data.step = 0;
	    data.sps = 30;
	    data.vProps = [];

	    for (p in value) {
	      var _value$p = value[p],
	          velocity = _value$p.velocity,
	          acceleration = _value$p.acceleration,
	          friction = _value$p.friction;

	      if (velocity || acceleration) {
	        data.vProps.push(new PhysicsProp(target, p, velocity, acceleration, friction, data.sps));

	        data._props.push(p);

	        _getStyleSaver && data.styles.save(p);
	        friction && (data.hasFr = 1);
	      }
	    }
	  },
	  render: function render(ratio, data) {
	    var vProps = data.vProps,
	        tween = data.tween,
	        target = data.target,
	        step = data.step,
	        hasFr = data.hasFr,
	        sps = data.sps,
	        i = vProps.length,
	        time = tween._from ? tween._dur - tween._time : tween._time,
	        curProp,
	        steps,
	        remainder,
	        j,
	        tt;

	    if (tween._time || !_reverting()) {
	      if (hasFr) {
	        time *= sps;
	        steps = (time | 0) - step;

	        if (steps < 0) {
	          while (i--) {
	            curProp = vProps[i];
	            curProp.v = curProp.vel / sps;
	            curProp.val = curProp.s;
	          }

	          i = vProps.length;
	          data.step = step = 0;
	          steps = time | 0;
	        }

	        remainder = time % 1;

	        while (i--) {
	          curProp = vProps[i];
	          j = steps;

	          while (j--) {
	            curProp.v += curProp.a;
	            curProp.v *= curProp.fr;
	            curProp.val += curProp.v;
	          }

	          curProp.set(target, curProp.p, _round(curProp.val + curProp.v * remainder * curProp.fr) + curProp.u);
	        }

	        data.step += steps;
	      } else {
	        tt = time * time * 0.5;

	        while (i--) {
	          curProp = vProps[i];
	          curProp.set(target, curProp.p, _round(curProp.s + curProp.vel * time + curProp.acc * tt) + curProp.u);
	        }
	      }
	    } else {
	      data.styles.revert();
	    }
	  },
	  kill: function kill(property) {
	    var vProps = this.vProps,
	        i = vProps.length;

	    while (i--) {
	      vProps[i].p === property && vProps.splice(i, 1);
	    }
	  }
	};
	_getGSAP() && gsap.registerPlugin(PhysicsPropsPlugin);

	exports.PhysicsPropsPlugin = PhysicsPropsPlugin;
	exports.default = PhysicsPropsPlugin;

	Object.defineProperty(exports, '__esModule', { value: true });

})));
