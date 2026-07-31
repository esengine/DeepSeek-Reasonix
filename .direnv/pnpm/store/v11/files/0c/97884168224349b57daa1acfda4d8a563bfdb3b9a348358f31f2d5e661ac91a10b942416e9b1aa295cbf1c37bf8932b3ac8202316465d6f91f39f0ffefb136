/*!
 * CSSRulePlugin 3.15.0
 * https://gsap.com
 *
 * @license Copyright 2008-2026, GreenSock. All rights reserved.
 * Subject to the terms at https://gsap.com/standard-license
 * @author: Jack Doyle, jack@greensock.com
*/
/* eslint-disable */

let gsap, _coreInitted, _win, _doc, CSSPlugin,
	_windowExists = () => typeof(window) !== "undefined",
	_getGSAP = () => gsap || (_windowExists() && (gsap = window.gsap) && gsap.registerPlugin && gsap),
	_checkRegister = () => {
		if (!_coreInitted) {
			_initCore();
			if (!CSSPlugin) {
				console.warn("Please gsap.registerPlugin(CSSPlugin, CSSRulePlugin)");
			}
		}
		return _coreInitted;
	},
	_initCore = core => {
		gsap = core || _getGSAP();
		if (_windowExists()) {
			_win = window;
			_doc = document;
		}
		if (gsap) {
			CSSPlugin = gsap.plugins.css;
			if (CSSPlugin) {
				_coreInitted = 1;
			}
		}
	};


export const CSSRulePlugin = {
	version: "3.15.0",
	name: "cssRule",
	init(target, value, tween, index, targets) {
		if (!_checkRegister() || typeof(target.cssText) === "undefined") {
			return false;
		}
		let div = target._gsProxy = target._gsProxy || _doc.createElement("div");
		this.ss = target;
		this.style = div.style;
		div.style.cssText = target.cssText;
		CSSPlugin.prototype.init.call(this, div, value, tween, index, targets); //we just offload all the work to the regular CSSPlugin and then copy the cssText back over to the rule in the render() method. This allows us to have all of the updates to CSSPlugin automatically flow through to CSSRulePlugin instead of having to maintain both
	},
	render(ratio, data) {
		let pt = data._pt,
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
	getRule(selector) {
		_checkRegister();
		let ruleProp = _doc.all ? "rules" : "cssRules",
			styleSheets = _doc.styleSheets,
			i = styleSheets.length,
			pseudo = (selector.charAt(0) === ":"),
			j, curSS, cs, a;
		selector = (pseudo ? "" : ",") + selector.split("::").join(":").toLowerCase() + ","; //note: old versions of IE report tag name selectors as upper case, so we just change everything to lowercase.
		if (pseudo) {
			a = [];
		}
		while (i--) {
			//Firefox may throw insecure operation errors when css is loaded from other domains, so try/catch.
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
				if (cs.selectorText && ("," + cs.selectorText.split("::").join(":").toLowerCase() + ",").indexOf(selector) !== -1) { //note: IE adds an extra ":" to pseudo selectors, so .myClass:after becomes .myClass::after, so we need to strip the extra one out.
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

export { CSSRulePlugin as default };