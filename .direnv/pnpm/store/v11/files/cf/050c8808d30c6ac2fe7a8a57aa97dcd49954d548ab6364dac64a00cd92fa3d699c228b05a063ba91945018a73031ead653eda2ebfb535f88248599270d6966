/*!
 * ScrollToPlugin 3.15.0
 * https://gsap.com
 *
 * @license Copyright 2008-2026, GreenSock. All rights reserved.
 * Subject to the terms at https://gsap.com/standard-license
 * @author: Jack Doyle, jack@greensock.com
*/
/* eslint-disable */

let gsap, _coreInitted, _window, _docEl, _body, _toArray, _config, ScrollTrigger,
	_windowExists = () => typeof(window) !== "undefined",
	_getGSAP = () => gsap || (_windowExists() && (gsap = window.gsap) && gsap.registerPlugin && gsap),
	_isString = value => typeof(value) === "string",
	_isFunction = value => typeof(value) === "function",
	_max = (element, axis) => {
		let dim = (axis === "x") ? "Width" : "Height",
			scroll = "scroll" + dim,
			client = "client" + dim;
		return (element === _window || element === _docEl || element === _body) ? Math.max(_docEl[scroll], _body[scroll]) - (_window["inner" + dim] || _docEl[client] || _body[client]) : element[scroll] - element["offset" + dim];
	},
	_buildGetter = (e, axis) => { //pass in an element and an axis ("x" or "y") and it'll return a getter function for the scroll position of that element (like scrollTop or scrollLeft, although if the element is the window, it'll use the pageXOffset/pageYOffset or the documentElement's scrollTop/scrollLeft or document.body's. Basically this streamlines things and makes a very fast getter across browsers.
		let p = "scroll" + ((axis === "x") ? "Left" : "Top");
		if (e === _window) {
			if (e.pageXOffset != null) {
				p = "page" + axis.toUpperCase() + "Offset";
			} else {
				e = _docEl[p] != null ? _docEl : _body;
			}
		}
		return () => e[p];
	},
	_clean = (value, index, target, targets) => {
		_isFunction(value) && (value = value(index, target, targets));
		if (typeof(value) !== "object") {
			return _isString(value) && value !== "max" && value.charAt(1) !== "=" ? {x: value, y: value} : {y: value}; //if we don't receive an object as the parameter, assume the user intends "y".
		} else if (value.nodeType) {
			return {y: value, x: value};
		} else {
			let result = {}, p;
			for (p in value) {
				result[p] = p !== "onAutoKill" && _isFunction(value[p]) ? value[p](index, target, targets) : value[p];
			}
			return result;
		}
	},
	_getOffset = (element, container) => {
		element = _toArray(element)[0];
		if (!element || !element.getBoundingClientRect) {
			return console.warn("scrollTo target doesn't exist. Using 0") || {x:0, y:0};
		}
		let rect = element.getBoundingClientRect(),
			isRoot = (!container || container === _window || container === _body),
			cRect = isRoot ? {top:_docEl.clientTop - (_window.pageYOffset || _docEl.scrollTop || _body.scrollTop || 0), left:_docEl.clientLeft - (_window.pageXOffset || _docEl.scrollLeft || _body.scrollLeft || 0)} : container.getBoundingClientRect(),
			offsets = {x: rect.left - cRect.left, y: rect.top - cRect.top};
		if (!isRoot && container) { //only add the current scroll position if it's not the window/body.
			offsets.x += _buildGetter(container, "x")();
			offsets.y += _buildGetter(container, "y")();
		}
		return offsets;
	},
	_parseVal = (value, target, axis, currentVal, offset) => !isNaN(value) && typeof(value) !== "object" ? parseFloat(value) - offset : (_isString(value) && value.charAt(1) === "=") ? parseFloat(value.substr(2)) * (value.charAt(0) === "-" ? -1 : 1) + currentVal - offset : (value === "max") ? _max(target, axis) - offset : Math.min(_max(target, axis), _getOffset(value, target)[axis] - offset),
	_initCore = () => {
		gsap = _getGSAP();
		if (_windowExists() && gsap && typeof(document) !== "undefined" && document.body) {
			_window = window;
			_body = document.body;
			_docEl = document.documentElement;
			_toArray = gsap.utils.toArray;
			gsap.config({autoKillThreshold:7});
			_config = gsap.config();
			_coreInitted = 1;
		}
	};


export const ScrollToPlugin = {
	version: "3.15.0",
	name: "scrollTo",
	rawVars: 1,
	register(core) {
		gsap = core;
		_initCore();
	},
	init(target, value, tween, index, targets) {
		_coreInitted || _initCore();
		let data = this,
			snapType = gsap.getProperty(target, "scrollSnapType");
		data.isWin = (target === _window);
		data.target = target;
		data.tween = tween;
		value = _clean(value, index, target, targets);
		data.vars = value;
		data.autoKill = !!("autoKill" in value ? value : _config).autoKill;
		data.getX = _buildGetter(target, "x");
		data.getY = _buildGetter(target, "y");
		data.x = data.xPrev = data.getX();
		data.y = data.yPrev = data.getY();
		ScrollTrigger || (ScrollTrigger = gsap.core.globals().ScrollTrigger);
		gsap.getProperty(target, "scrollBehavior") === "smooth" && gsap.set(target, {scrollBehavior: "auto"});
		if (snapType && snapType !== "none") { // disable scroll snapping to avoid strange behavior
			data.snap = 1;
			data.snapInline = target.style.scrollSnapType;
			target.style.scrollSnapType = "none";
		}
		if (value.x != null) {
			data.add(data, "x", data.x, _parseVal(value.x, target, "x", data.x, value.offsetX || 0), index, targets);
			data._props.push("scrollTo_x");
		} else {
			data.skipX = 1;
		}
		if (value.y != null) {
			data.add(data, "y", data.y, _parseVal(value.y, target, "y", data.y, value.offsetY || 0), index, targets);
			data._props.push("scrollTo_y");
		} else {
			data.skipY = 1;
		}
	},
	render(ratio, data) {
		let pt = data._pt,
			{ target, tween, autoKill, xPrev, yPrev, isWin, snap, snapInline } = data,
			x, y, yDif, xDif, threshold;
		while (pt) {
			pt.r(ratio, pt.d);
			pt = pt._next;
		}
		x = (isWin || !data.skipX) ? data.getX() : xPrev;
		y = (isWin || !data.skipY) ? data.getY() : yPrev;
		yDif = y - yPrev;
		xDif = x - xPrev;
		threshold = _config.autoKillThreshold;
		if (data.x < 0) { //can't scroll to a position less than 0! Might happen if someone uses a Back.easeOut or Elastic.easeOut when scrolling back to the top of the page (for example)
			data.x = 0;
		}
		if (data.y < 0) {
			data.y = 0;
		}
		if (autoKill) {
			//note: iOS has a bug that throws off the scroll by several pixels, so we need to check if it's within 7 pixels of the previous one that we set instead of just looking for an exact match.
			if (!data.skipX && (xDif > threshold || xDif < -threshold) && x < _max(target, "x")) {
				data.skipX = 1; //if the user scrolls separately, we should stop tweening!
			}
			if (!data.skipY && (yDif > threshold || yDif < -threshold) && y < _max(target, "y")) {
				data.skipY = 1; //if the user scrolls separately, we should stop tweening!
			}
			if (data.skipX && data.skipY) {
				tween.kill();
				data.vars.onAutoKill && data.vars.onAutoKill.apply(tween, data.vars.onAutoKillParams || []);
			}
		}
		if (isWin) {
			_window.scrollTo((!data.skipX) ? data.x : x, (!data.skipY) ? data.y : y);
		} else {
			data.skipY || (target.scrollTop = data.y);
			data.skipX || (target.scrollLeft = data.x);
		}
		if (snap && (ratio === 1 || ratio === 0)) {
			y = target.scrollTop;
			x = target.scrollLeft;
			snapInline ? (target.style.scrollSnapType = snapInline) : target.style.removeProperty("scroll-snap-type");
			target.scrollTop = y + 1; // bug in Safari causes the element to totally reset its scroll position when scroll-snap-type changes, so we need to set it to a slightly different value and then back again to work around this bug.
			target.scrollLeft = x + 1;
			target.scrollTop = y;
			target.scrollLeft = x;
		}
		data.xPrev = data.x;
		data.yPrev = data.y;
		ScrollTrigger && ScrollTrigger.update();
	},
	kill(property) {
		let both = (property === "scrollTo"),
			i = this._props.indexOf(property);
		if (both || property === "scrollTo_x") {
			this.skipX = 1;
		}
		if (both || property === "scrollTo_y") {
			this.skipY = 1;
		}
		i > -1 && this._props.splice(i, 1);
		return !this._props.length;
	}
};

ScrollToPlugin.max = _max;
ScrollToPlugin.getOffset = _getOffset;
ScrollToPlugin.buildGetter = _buildGetter;
ScrollToPlugin.config = vars => {
	_config || _initCore() || (_config = gsap.config()); // in case the window hasn't been defined yet.
	for (let p in vars) {
		_config[p] = vars[p];
	}
}

_getGSAP() && gsap.registerPlugin(ScrollToPlugin);

export { ScrollToPlugin as default };