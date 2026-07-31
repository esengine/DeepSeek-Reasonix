/*!
 * PixiPlugin 3.15.0
 * https://gsap.com
 *
 * @license Copyright 2008-2026, GreenSock. All rights reserved.
 * Subject to the terms at https://gsap.com/standard-license
 * @author: Jack Doyle, jack@greensock.com
*/
/* eslint-disable */

let gsap, _splitColor, _coreInitted, _PIXI, PropTween, _getSetter, _isV4, _isV8Plus,
	_windowExists = () => typeof(window) !== "undefined",
	_getGSAP = () => gsap || (_windowExists() && (gsap = window.gsap) && gsap.registerPlugin && gsap),
	_isFunction = value => typeof(value) === "function",
	_warn = message => console.warn(message),
	_idMatrix = [1,0,0,0,0,0,1,0,0,0,0,0,1,0,0,0,0,0,1,0],
	_lumR = 0.212671,
	_lumG = 0.715160,
	_lumB = 0.072169,
	_filterClass = name => _isFunction(_PIXI[name]) ? _PIXI[name] : _PIXI.filters[name], // in PIXI 7.1, filters moved from PIXI.filters to just PIXI
	_applyMatrix = (m, m2) => {
		let temp = [],
			i = 0,
			z = 0,
			y, x;
		for (y = 0; y < 4; y++) {
			for (x = 0; x < 5; x++) {
				z = (x === 4) ? m[i + 4] : 0;
				temp[i + x] = m[i]   * m2[x] + m[i+1] * m2[x + 5] +	m[i+2] * m2[x + 10] + m[i+3] * m2[x + 15] +	z;
			}
			i += 5;
		}
		return temp;
	},
	_setSaturation = (m, n) => {
		let inv = 1 - n,
			r = inv * _lumR,
			g = inv * _lumG,
			b = inv * _lumB;
		return _applyMatrix([r + n, g, b, 0, 0, r, g + n, b, 0, 0, r, g, b + n, 0, 0, 0, 0, 0, 1, 0], m);
	},
	_colorize = (m, color, amount) => {
		let c = _splitColor(color),
			r = c[0] / 255,
			g = c[1] / 255,
			b = c[2] / 255,
			inv = 1 - amount;
		return _applyMatrix([inv + amount * r * _lumR, amount * r * _lumG, amount * r * _lumB, 0, 0, amount * g * _lumR, inv + amount * g * _lumG, amount * g * _lumB, 0, 0, amount * b * _lumR, amount * b * _lumG, inv + amount * b * _lumB, 0, 0, 0, 0, 0, 1, 0], m);
	},
	_setHue = (m, n) => {
		n *= Math.PI / 180;
		let c = Math.cos(n),
			s = Math.sin(n);
		return _applyMatrix([(_lumR + (c * (1 - _lumR))) + (s * (-_lumR)), (_lumG + (c * (-_lumG))) + (s * (-_lumG)), (_lumB + (c * (-_lumB))) + (s * (1 - _lumB)), 0, 0, (_lumR + (c * (-_lumR))) + (s * 0.143), (_lumG + (c * (1 - _lumG))) + (s * 0.14), (_lumB + (c * (-_lumB))) + (s * -0.283), 0, 0, (_lumR + (c * (-_lumR))) + (s * (-(1 - _lumR))), (_lumG + (c * (-_lumG))) + (s * _lumG), (_lumB + (c * (1 - _lumB))) + (s * _lumB), 0, 0, 0, 0, 0, 1, 0, 0, 0, 0, 0, 1], m);
	},
	_setContrast = (m, n) => _applyMatrix([n,0,0,0,0.5 * (1 - n), 0,n,0,0,0.5 * (1 - n), 0,0,n,0,0.5 * (1 - n), 0,0,0,1,0], m),
	_getFilter = (target, type) => {
		let filterClass = _filterClass(type),
			filters = target.filters || [],
			i = filters.length,
			filter;
		filterClass || _warn(type + " not found. PixiPlugin.registerPIXI(PIXI)");
		while (--i > -1) {
			if (filters[i] instanceof filterClass) {
				return filters[i];
			}
		}
		filter = new filterClass();
		if (type === "BlurFilter") {
			if (_isV8Plus) {
				filter.strength = 0;
			} else {
				filter.blur = 0;
			}
		}
		target.filters = [...filters, filter];
		return filter;
	},
	_addColorMatrixFilterCacheTween = (p, plugin, cache, vars) => { //we cache the ColorMatrixFilter components in a _gsColorMatrixFilter object attached to the target object so that it's easy to grab the current value at any time.
		plugin.add(cache, p, cache[p], vars[p]);
		plugin._props.push(p);
	},
	_applyBrightnessToMatrix = (brightness, matrix) => {
		let filterClass = _filterClass("ColorMatrixFilter"),
			temp = new filterClass();
		temp.matrix = matrix;
		temp.brightness(brightness, true);
		return temp.matrix;
	},
	_copy = obj => {
		let copy = {},
			p;
		for (p in obj) {
			copy[p] = obj[p];
		}
		return copy;
	},
	_CMFdefaults = {contrast:1, saturation:1, colorizeAmount:0, colorize:"rgb(255,255,255)", hue:0, brightness:1},
	_parseColorMatrixFilter = (target, v, pg) => {
		let filter = _getFilter(target, "ColorMatrixFilter"),
			cache = target._gsColorMatrixFilter = target._gsColorMatrixFilter || _copy(_CMFdefaults),
			combine = v.combineCMF && !("colorMatrixFilter" in v && !v.colorMatrixFilter),
			i, matrix, startMatrix;
		startMatrix = filter.matrix;
		if (v.resolution) {
			filter.resolution = v.resolution;
		}
		if (v.matrix && v.matrix.length === startMatrix.length) {
			matrix = v.matrix;
			if (cache.contrast !== 1) {
				_addColorMatrixFilterCacheTween("contrast", pg, cache, _CMFdefaults);
			}
			if (cache.hue) {
				_addColorMatrixFilterCacheTween("hue", pg, cache, _CMFdefaults);
			}
			if (cache.brightness !== 1) {
				_addColorMatrixFilterCacheTween("brightness", pg, cache, _CMFdefaults);
			}
			if (cache.colorizeAmount) {
				_addColorMatrixFilterCacheTween("colorize", pg, cache, _CMFdefaults);
				_addColorMatrixFilterCacheTween("colorizeAmount", pg, cache, _CMFdefaults);
			}
			if (cache.saturation !== 1) {
				_addColorMatrixFilterCacheTween("saturation", pg, cache, _CMFdefaults);
			}

		} else {
			matrix = _idMatrix.slice();
			if (v.contrast != null) {
				matrix = _setContrast(matrix, +v.contrast);
				_addColorMatrixFilterCacheTween("contrast", pg, cache, v);
			} else if (cache.contrast !== 1) {
				if (combine) {
					matrix = _setContrast(matrix, cache.contrast);
				} else {
					_addColorMatrixFilterCacheTween("contrast", pg, cache, _CMFdefaults);
				}
			}
			if (v.hue != null) {
				matrix = _setHue(matrix, +v.hue);
				_addColorMatrixFilterCacheTween("hue", pg, cache, v);
			} else if (cache.hue) {
				if (combine) {
					matrix = _setHue(matrix, cache.hue);
				} else {
					_addColorMatrixFilterCacheTween("hue", pg, cache, _CMFdefaults);
				}
			}
			if (v.brightness != null) {
				matrix = _applyBrightnessToMatrix(+v.brightness, matrix);
				_addColorMatrixFilterCacheTween("brightness", pg, cache, v);
			} else if (cache.brightness !== 1) {
				if (combine) {
					matrix = _applyBrightnessToMatrix(cache.brightness, matrix);
				} else {
					_addColorMatrixFilterCacheTween("brightness", pg, cache, _CMFdefaults);
				}
			}
			if (v.colorize != null) {
				v.colorizeAmount = ("colorizeAmount" in v) ? +v.colorizeAmount : 1;
				matrix = _colorize(matrix, v.colorize, v.colorizeAmount);
				_addColorMatrixFilterCacheTween("colorize", pg, cache, v);
				_addColorMatrixFilterCacheTween("colorizeAmount", pg, cache, v);
			} else if (cache.colorizeAmount) {
				if (combine) {
					matrix = _colorize(matrix, cache.colorize, cache.colorizeAmount);
				} else {
					_addColorMatrixFilterCacheTween("colorize", pg, cache, _CMFdefaults);
					_addColorMatrixFilterCacheTween("colorizeAmount", pg, cache, _CMFdefaults);
				}
			}
			if (v.saturation != null) {
				matrix = _setSaturation(matrix, +v.saturation);
				_addColorMatrixFilterCacheTween("saturation", pg, cache, v);
			} else if (cache.saturation !== 1) {
				if (combine) {
					matrix = _setSaturation(matrix, cache.saturation);
				} else {
					_addColorMatrixFilterCacheTween("saturation", pg, cache, _CMFdefaults);
				}
			}
		}
		i = matrix.length;
		while (--i > -1) {
			if (matrix[i] !== startMatrix[i]) {
				pg.add(startMatrix, i, startMatrix[i], matrix[i], "colorMatrixFilter");
			}
		}
		pg._props.push("colorMatrixFilter");
	},
	_renderColor = (ratio, {t, p, color, set}) => {
		set(t, p, color[0] << 16 | color[1] << 8 | color[2]);
	},
	_renderDirtyCache = (ratio, {g}) => {
		if (_isV8Plus) {
			g.fill();
			g.stroke();
		} else if (g) { // in order for PixiJS to actually redraw GraphicsData, we've gotta increment the "dirty" and "clearDirty" values. If we don't do this, the values will be tween properly, but not rendered.
			g.dirty++;
			g.clearDirty++;
		}
	},
	_renderAutoAlpha = (ratio, data) => {
		data.t.visible = !!data.t.alpha;
	},
	_addColorTween = (target, p, value, plugin) => {
		let currentValue = target[p],
			startColor = _splitColor(_isFunction(currentValue) ? target[ ((p.indexOf("set") || !_isFunction(target["get" + p.substr(3)])) ? p : "get" + p.substr(3)) ]() : currentValue),
			endColor = _splitColor(value);
		plugin._pt = new PropTween(plugin._pt, target, p, 0, 0, _renderColor, {t:target, p:p, color:startColor, set:_getSetter(target, p)});
		plugin.add(startColor, 0, startColor[0], endColor[0]);
		plugin.add(startColor, 1, startColor[1], endColor[1]);
		plugin.add(startColor, 2, startColor[2], endColor[2]);
	},

	_colorProps = {tint:1, lineColor:1, fillColor:1, strokeColor:1},
	_xyContexts = "position,scale,skew,pivot,anchor,tilePosition,tileScale".split(","),
	_contexts = {x:"position", y:"position", tileX:"tilePosition", tileY:"tilePosition"},
	_colorMatrixFilterProps = {colorMatrixFilter:1, saturation:1, contrast:1, hue:1, colorize:1, colorizeAmount:1, brightness:1, combineCMF:1},
	_DEG2RAD = Math.PI / 180,
	_isString = value => typeof(value) === "string",
	_degreesToRadians = value => (_isString(value) && value.charAt(1) === "=") ? value.substr(0, 2) + (parseFloat(value.substr(2)) * _DEG2RAD) : value * _DEG2RAD,
	_renderPropWithEnd = (ratio, data) => data.set(data.t, data.p, ratio === 1 ? data.e : (Math.round((data.s + data.c * ratio) * 100000) / 100000), data),
	_addRotationalPropTween = (plugin, target, property, startNum, endValue, radians) => {
		let cap = 360 * (radians ? _DEG2RAD : 1),
			isString = _isString(endValue),
			relative = (isString && endValue.charAt(1) === "=") ? +(endValue.charAt(0) + "1") : 0,
			endNum = parseFloat(relative ? endValue.substr(2) : endValue) * (radians ? _DEG2RAD : 1),
			change = relative ? endNum * relative : endNum - startNum,
			finalValue = startNum + change,
			direction, pt;
		if (isString) {
			direction = endValue.split("_")[1];
			if (direction === "short") {
				change %= cap;
				if (change !== change % (cap / 2)) {
					change += (change < 0) ? cap : -cap;
				}
			}
			if (direction === "cw" && change < 0) {
				change = ((change + cap * 1e10) % cap) - ~~(change / cap) * cap;
			} else if (direction === "ccw" && change > 0) {
				change = ((change - cap * 1e10) % cap) - ~~(change / cap) * cap;
			}
		}
		plugin._pt = pt = new PropTween(plugin._pt, target, property, startNum, change, _renderPropWithEnd);
		pt.e = finalValue;
		return pt;
	},
	_initCore = () => {
		if (!_coreInitted) {
			gsap = _getGSAP();
			_PIXI = _coreInitted = _PIXI || (_windowExists() && window.PIXI);
			let version = (_PIXI && _PIXI.VERSION && parseFloat(_PIXI.VERSION.split(".")[0])) || 0;
			_isV4 = version === 4;
			_isV8Plus = version >= 8;
			_splitColor = color => gsap.utils.splitColor((color + "").substr(0,2) === "0x" ? "#" + color.substr(2) : color); // some colors in PIXI are reported as "0xFF4421" instead of "#FF4421".
		}
	}, i, p;

//context setup...
for (i = 0; i < _xyContexts.length; i++) {
	p = _xyContexts[i];
	_contexts[p + "X"] = p;
	_contexts[p + "Y"] = p;
}


export const PixiPlugin = {
	version: "3.15.0",
	name: "pixi",
	register(core, Plugin, propTween) {
		gsap = core;
		PropTween = propTween;
		_getSetter = Plugin.getSetter;
		_initCore();
	},
	headless: true, // doesn't need window
	registerPIXI(pixi) {
		_PIXI = pixi;
	},
	init(target, values, tween, index, targets) {
		_PIXI || _initCore();
		if (!_PIXI) {
			_warn("PIXI was not found. PixiPlugin.registerPIXI(PIXI);");
			return false;
		}
		let context, axis, value, colorMatrix, filter, p, padding, i, data, subProp;
		for (p in values) {
			context = _contexts[p];
			value = values[p];
			if (context) {
				axis = ~p.charAt(p.length-1).toLowerCase().indexOf("x") ? "x" : "y";
				this.add(target[context], axis, target[context][axis], (context === "skew") ? _degreesToRadians(value) : value, 0, 0, 0, 0, 0, 1);
			} else if (p === "scale" || p === "anchor" || p === "pivot" || p === "tileScale") {
				this.add(target[p], "x", target[p].x, value);
				this.add(target[p], "y", target[p].y, value);
			} else if (p === "rotation" || p === "angle") { //PIXI expects rotation in radians, but as a convenience we let folks define it in degrees and we do the conversion.
				_addRotationalPropTween(this, target, p, target[p], value, p === "rotation");
			} else if (_colorMatrixFilterProps[p]) {
				if (!colorMatrix) {
					_parseColorMatrixFilter(target, values.colorMatrixFilter || values, this);
					colorMatrix = true;
				}
			} else if (p === "blur" || p === "blurX" || p === "blurY" || p === "blurPadding") {
				filter = _getFilter(target, "BlurFilter");
				this.add(filter, p, filter[p], value);
				if (values.blurPadding !== 0) {
					padding = values.blurPadding || Math.max(filter[p], value) * 2;
					i = target.filters.length;
					while (--i > -1) {
						target.filters[i].padding = Math.max(target.filters[i].padding, padding); //if we don't expand the padding on all the filters, it can look clipped.
					}
				}
			} else if (_colorProps[p]) {
				if ((p === "lineColor" || p === "fillColor" || p === "strokeColor") && target instanceof _PIXI.Graphics) {
					data = "fillStyle" in target ? [target] : (target.geometry || target).graphicsData; //"geometry" was introduced in PIXI version 5
					subProp = p.substr(0, p.length - 5);
					_isV8Plus && subProp === "line" && (subProp = "stroke"); // in v8, lineColor became strokeColor.
					this._pt = new PropTween(this._pt, target, p, 0, 0, _renderDirtyCache, {g: target.geometry || target});
					i = data.length;
					while (--i > -1) {
						_addColorTween(_isV4 ? data[i] : data[i][subProp + "Style"], _isV4 ? p : "color", value, this);
					}
				} else {
					_addColorTween(target, p, value, this);
				}
			} else if (p === "autoAlpha") {
				this._pt = new PropTween(this._pt, target, "visible", 0, 0, _renderAutoAlpha);
				this.add(target, "alpha", target.alpha, value);
				this._props.push("alpha", "visible");
			} else if (p !== "resolution") {
				this.add(target, p, "get", value);
			}
			this._props.push(p);
		}
	}
};

_getGSAP() && gsap.registerPlugin(PixiPlugin);

export { PixiPlugin as default };