(function (global, factory) {
	typeof exports === 'object' && typeof module !== 'undefined' ? factory(exports) :
	typeof define === 'function' && define.amd ? define(['exports'], factory) :
	(global = global || self, factory(global.window = global.window || {}));
}(this, (function (exports) { 'use strict';

	/*!
	 * CustomBounce 3.15.0
	 * https://gsap.com
	 *
	 * @license Copyright 2008-2026, GreenSock. All rights reserved.
	 * Subject to the terms at https://gsap.com/standard-license
	 * @author: Jack Doyle, jack@greensock.com
	*/
	var gsap,
	    _coreInitted,
	    createCustomEase,
	    _getGSAP = function _getGSAP() {
	  return gsap || typeof window !== "undefined" && (gsap = window.gsap) && gsap.registerPlugin && gsap;
	},
	    _initCore = function _initCore(required) {
	  gsap = _getGSAP();
	  createCustomEase = gsap && gsap.parseEase("_CE");

	  if (createCustomEase) {
	    _coreInitted = 1;

	    gsap.parseEase("bounce").config = function (vars) {
	      return typeof vars === "object" ? _create("", vars) : _create("bounce(" + vars + ")", {
	        strength: +vars
	      });
	    };
	  } else {
	    required && console.warn("Please gsap.registerPlugin(CustomEase, CustomBounce)");
	  }
	},
	    _normalizeX = function _normalizeX(a) {
	  var l = a.length,
	      s = 1 / a[l - 2],
	      rnd = 1000,
	      i;

	  for (i = 2; i < l; i += 2) {
	    a[i] = ~~(a[i] * s * rnd) / rnd;
	  }

	  a[l - 2] = 1;
	},
	    _create = function _create(id, vars) {
	  if (!_coreInitted) {
	    _initCore(1);
	  }

	  vars = vars || {};

	  {
	    var max = 0.999,
	        decay = Math.min(max, vars.strength || 0.7),
	        decayX = decay,
	        gap = (vars.squash || 0) / 100,
	        originalGap = gap,
	        slope = 1 / 0.03,
	        w = 0.2,
	        h = 1,
	        prevX = 0.1,
	        path = [0, 0, 0.07, 0, 0.1, 1, 0.1, 1],
	        squashPath = [0, 0, 0, 0, 0.1, 0, 0.1, 0],
	        cp1,
	        cp2,
	        x,
	        y,
	        i,
	        nextX,
	        squishMagnitude;

	    for (i = 0; i < 200; i++) {
	      w *= decayX * ((decayX + 1) / 2);
	      h *= decay * decay;
	      nextX = prevX + w;
	      x = prevX + w * 0.49;
	      y = 1 - h;
	      cp1 = prevX + h / slope;
	      cp2 = x + (x - cp1) * 0.8;

	      if (gap) {
	        prevX += gap;
	        cp1 += gap;
	        x += gap;
	        cp2 += gap;
	        nextX += gap;
	        squishMagnitude = gap / originalGap;
	        squashPath.push(prevX - gap, 0, prevX - gap, squishMagnitude, prevX - gap / 2, squishMagnitude, prevX, squishMagnitude, prevX, 0, prevX, 0, prevX, squishMagnitude * -0.6, prevX + (nextX - prevX) / 6, 0, nextX, 0);
	        path.push(prevX - gap, 1, prevX, 1, prevX, 1);
	        gap *= decay * decay;
	      }

	      path.push(prevX, 1, cp1, y, x, y, cp2, y, nextX, 1, nextX, 1);
	      decay *= 0.95;
	      slope = h / (nextX - cp2);
	      prevX = nextX;

	      if (y > max) {
	        break;
	      }
	    }

	    if (vars.endAtStart && vars.endAtStart !== "false") {
	      x = -0.1;
	      path.unshift(x, 1, x, 1, -0.07, 0);

	      if (originalGap) {
	        gap = originalGap * 2.5;
	        x -= gap;
	        path.unshift(x, 1, x, 1, x, 1);
	        squashPath.splice(0, 6);
	        squashPath.unshift(x, 0, x, 0, x, 1, x + gap / 2, 1, x + gap, 1, x + gap, 0, x + gap, 0, x + gap, -0.6, x + gap + 0.033, 0);

	        for (i = 0; i < squashPath.length; i += 2) {
	          squashPath[i] -= x;
	        }
	      }

	      for (i = 0; i < path.length; i += 2) {
	        path[i] -= x;
	        path[i + 1] = 1 - path[i + 1];
	      }
	    }

	    if (gap) {
	      _normalizeX(squashPath);

	      squashPath[2] = "C" + squashPath[2];
	      createCustomEase(vars.squashID || id + "-squash", "M" + squashPath.join(","));
	    }

	    _normalizeX(path);

	    path[2] = "C" + path[2];
	    return createCustomEase(id, "M" + path.join(","));
	  }
	};

	var CustomBounce = function () {
	  function CustomBounce(id, vars) {
	    this.ease = _create(id, vars);
	  }

	  CustomBounce.create = function create(id, vars) {
	    return _create(id, vars);
	  };

	  CustomBounce.register = function register(core) {
	    gsap = core;

	    _initCore();
	  };

	  return CustomBounce;
	}();
	_getGSAP() && gsap.registerPlugin(CustomBounce);
	CustomBounce.version = "3.15.0";

	exports.CustomBounce = CustomBounce;
	exports.default = CustomBounce;

	Object.defineProperty(exports, '__esModule', { value: true });

})));
