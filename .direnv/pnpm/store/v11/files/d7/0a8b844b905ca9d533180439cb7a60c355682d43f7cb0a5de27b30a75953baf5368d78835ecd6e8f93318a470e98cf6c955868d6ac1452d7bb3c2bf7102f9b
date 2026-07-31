(function (global, factory) {
	typeof exports === 'object' && typeof module !== 'undefined' ? factory(exports) :
	typeof define === 'function' && define.amd ? define(['exports'], factory) :
	(global = global || self, factory(global.window = global.window || {}));
}(this, (function (exports) { 'use strict';

	/*!
	 * CSSRulePlugin 3.15.0
	 * https://gsap.com
	 *
	 * @license Copyright 2008-2026, GreenSock. All rights reserved.
	 * Subject to the terms at https://gsap.com/standard-license
	 * @author: Jack Doyle, jack@greensock.com
	*/
	var gsap,
	    _coreInitted,
	    _doc,
	    CSSPlugin,
	    _windowExists = function _windowExists() {
	  return typeof window !== "undefined";
	},
	    _getGSAP = function _getGSAP() {
	  return gsap || _windowExists() && (gsap = window.gsap) && gsap.registerPlugin && gsap;
	},
	    _checkRegister = function _checkRegister() {
	  if (!_coreInitted) {
	    _initCore();

	    if (!CSSPlugin) {
	      console.warn("Please gsap.registerPlugin(CSSPlugin, CSSRulePlugin)");
	    }
	  }

	  return _coreInitted;
	},
	    _initCore = function _initCore(core) {
	  gsap = core || _getGSAP();

	  if (_windowExists()) {
	    _doc = document;
	  }

	  if (gsap) {
	    CSSPlugin = gsap.plugins.css;

	    if (CSSPlugin) {
	      _coreInitted = 1;
	    }
	  }
	};

	var CSSRulePlugin = {
	  version: "3.15.0",
	  name: "cssRule",
	  init: function init(target, value, tween, index, targets) {
	    if (!_checkRegister() || typeof target.cssText === "undefined") {
	      return false;
	    }

	    var div = target._gsProxy = target._gsProxy || _doc.createElement("div");

	    this.ss = target;
	    this.style = div.style;
	    div.style.cssText = target.cssText;
	    CSSPlugin.prototype.init.call(this, div, value, tween, index, targets);
	  },
	  render: function render(ratio, data) {
	    var pt = data._pt,
	        style = data.style,
	        ss = data.ss,
	        i;

	    while (pt) {
	      pt.r(ratio, pt.d);
	      pt = pt._next;
	    }

	    i = style.length;

	    while (--i > -1) {
	      ss[style[i]] = style[style[i]];
	    }
	  },
	  getRule: function getRule(selector) {
	    _checkRegister();

	    var ruleProp = _doc.all ? "rules" : "cssRules",
	        styleSheets = _doc.styleSheets,
	        i = styleSheets.length,
	        pseudo = selector.charAt(0) === ":",
	        j,
	        curSS,
	        cs,
	        a;
	    selector = (pseudo ? "" : ",") + selector.split("::").join(":").toLowerCase() + ",";

	    if (pseudo) {
	      a = [];
	    }

	    while (i--) {
	      try {
	        curSS = styleSheets[i][ruleProp];

	        if (!curSS) {
	          continue;
	        }

	        j = curSS.length;
	      } catch (e) {
	        console.warn(e);
	        continue;
	      }

	      while (--j > -1) {
	        cs = curSS[j];

	        if (cs.selectorText && ("," + cs.selectorText.split("::").join(":").toLowerCase() + ",").indexOf(selector) !== -1) {
	          if (pseudo) {
	            a.push(cs.style);
	          } else {
	            return cs.style;
	          }
	        }
	      }
	    }

	    return a;
	  },
	  register: _initCore
	};
	_getGSAP() && gsap.registerPlugin(CSSRulePlugin);

	exports.CSSRulePlugin = CSSRulePlugin;
	exports.default = CSSRulePlugin;

	Object.defineProperty(exports, '__esModule', { value: true });

})));
