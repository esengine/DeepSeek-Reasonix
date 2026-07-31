import { CacheItem, NullObject, createCacheKey, getCache, setCache } from "./cache.js";
import { isString } from "./common.js";
import { SYN_FN_CALC, SYN_FN_REL, SYN_FN_VAR, VAL_COMP } from "./constant.js";
import { convertColorToHsl, convertColorToHwb, convertColorToLab, convertColorToLch, convertColorToOklab, convertColorToOklch, convertColorToRgb, numberToHexString, parseColorFunc, parseColorValue } from "./color.js";
import { resolveRelativeColor } from "./relative-color.js";
import { resolveColor } from "./resolve.js";
import { resolveVar } from "./css-var.js";
import { cssCalc } from "./css-calc.js";
//#region src/js/convert.ts
/**
* convert
*/
var NAMESPACE = "convert";
var REG_FN_CALC = new RegExp(SYN_FN_CALC);
var REG_FN_REL = new RegExp(SYN_FN_REL);
var REG_FN_VAR = new RegExp(SYN_FN_VAR);
/**
* pre process
* @param value - CSS color value
* @param [opt] - options
* @returns value
*/
var preProcess = (value, opt = {}) => {
	if (!isString(value)) return new NullObject();
	value = value.trim();
	if (!value) return new NullObject();
	const cacheKey = createCacheKey({
		namespace: NAMESPACE,
		name: "preProcess",
		value
	}, opt);
	const cachedResult = getCache(cacheKey);
	if (cachedResult instanceof CacheItem) {
		if (cachedResult.isNull) return cachedResult;
		return cachedResult.item;
	}
	let res = value;
	if (REG_FN_VAR.test(value)) {
		const resolved = resolveVar(value, opt);
		if (isString(resolved)) res = resolved;
		else {
			setCache(cacheKey, null);
			return new NullObject();
		}
	}
	if (isString(res)) {
		if (REG_FN_REL.test(res)) {
			const resolved = resolveRelativeColor(res, opt);
			if (isString(resolved)) res = resolved;
			else {
				setCache(cacheKey, null);
				return new NullObject();
			}
		} else if (REG_FN_CALC.test(res)) res = cssCalc(res, opt);
	}
	if (isString(res)) {
		if (res.startsWith("color-mix")) res = resolveColor(res, {
			...opt,
			format: VAL_COMP,
			nullable: true
		});
	}
	setCache(cacheKey, res);
	return res;
};
/**
* converter factory to reduce boilerplate
* @param name - function name for cache
* @param format - color format
* @param convertFn - conversion function
* @returns color converter function
*/
var createColorConverter = (name, format, convertFn) => {
	const colorConverterFn = (value, opt = {}) => {
		if (!isString(value)) throw new TypeError(`${value} is not a string.`);
		const resolved = preProcess(value, opt);
		if (resolved instanceof NullObject) return [
			0,
			0,
			0,
			0
		];
		const val = resolved.toLowerCase();
		const cacheKey = createCacheKey({
			namespace: NAMESPACE,
			name,
			value: val
		}, opt);
		const cached = getCache(cacheKey);
		if (cached instanceof CacheItem) return cached.item;
		const result = convertFn(val, {
			...opt,
			format
		});
		setCache(cacheKey, result);
		return result;
	};
	return colorConverterFn;
};
/**
* convert number to hex string
* @param value - numeric value
* @returns hex string: 00..ff
*/
var numberToHex = (value) => numberToHexString(value);
/**
* convert color to hex
* @param value - CSS color value
* @param [opt] - options
* @param [opt.alpha] - enable alpha channel
* @returns #rrggbb | #rrggbbaa | null
*/
var colorToHex = (value, opt = {}) => {
	if (!isString(value)) throw new TypeError(`${value} is not a string.`);
	const resolved = preProcess(value, opt);
	if (resolved instanceof NullObject) return null;
	const val = resolved.toLowerCase();
	const cacheKey = createCacheKey({
		namespace: NAMESPACE,
		name: "colorToHex",
		value: val
	}, opt);
	const cached = getCache(cacheKey);
	if (cached instanceof CacheItem) {
		if (cached.isNull) return null;
		return cached.item;
	}
	const hex = resolveColor(val, {
		...opt,
		nullable: true,
		format: opt.alpha ? "hexAlpha" : "hex"
	});
	if (isString(hex)) {
		setCache(cacheKey, hex);
		return hex;
	}
	setCache(cacheKey, null);
	return null;
};
/**
* convert color to hsl
* @param value - CSS color value
* @param [opt] - options
* @returns ColorChannels - [h, s, l, alpha]
*/
var colorToHsl = createColorConverter("colorToHsl", "hsl", convertColorToHsl);
/**
* convert color to hwb
* @param value - CSS color value
* @param [opt] - options
* @returns ColorChannels - [h, w, b, alpha]
*/
var colorToHwb = createColorConverter("colorToHwb", "hwb", convertColorToHwb);
/**
* convert color to lab
* @param value - CSS color value
* @param [opt] - options
* @returns ColorChannels - [l, a, b, alpha]
*/
var colorToLab = createColorConverter("colorToLab", "lab", convertColorToLab);
/**
* convert color to lch
* @param value - CSS color value
* @param [opt] - options
* @returns ColorChannels - [l, c, h, alpha]
*/
var colorToLch = createColorConverter("colorToLch", "lch", convertColorToLch);
/**
* convert color to oklab
* @param value - CSS color value
* @param [opt] - options
* @returns ColorChannels - [l, a, b, alpha]
*/
var colorToOklab = createColorConverter("colorToOklab", "oklab", convertColorToOklab);
/**
* convert color to oklch
* @param value - CSS color value
* @param [opt] - options
* @returns ColorChannels - [l, c, h, alpha]
*/
var colorToOklch = createColorConverter("colorToOklch", "oklch", convertColorToOklch);
/**
* convert color to rgb
* @param value - CSS color value
* @param [opt] - options
* @returns ColorChannels - [r, g, b, alpha]
*/
var colorToRgb = createColorConverter("colorToRgb", "rgb", convertColorToRgb);
/**
* convert color to xyz
* @param value - CSS color value
* @param [opt] - options
* @returns ColorChannels - [x, y, z, alpha]
*/
var colorToXyz = (value, opt = {}) => {
	if (!isString(value)) throw new TypeError(`${value} is not a string.`);
	const resolved = preProcess(value, opt);
	if (resolved instanceof NullObject) return [
		0,
		0,
		0,
		0
	];
	const val = resolved.toLowerCase();
	const cacheKey = createCacheKey({
		namespace: NAMESPACE,
		name: "colorToXyz",
		value: val
	}, opt);
	const cached = getCache(cacheKey);
	if (cached instanceof CacheItem) return cached.item;
	let parsed;
	if (val.startsWith("color(")) parsed = parseColorFunc(val, opt);
	else parsed = parseColorValue(val, opt);
	const [, ...xyz] = parsed;
	setCache(cacheKey, xyz);
	return xyz;
};
/**
* convert color to xyz-d50
* @param value - CSS color value
* @param [opt] - options
* @returns ColorChannels - [x, y, z, alpha]
*/
var colorToXyzD50 = (value, opt = {}) => {
	opt.d50 = true;
	return colorToXyz(value, opt);
};
var convert = {
	colorToHex,
	colorToHsl,
	colorToHwb,
	colorToLab,
	colorToLch,
	colorToOklab,
	colorToOklch,
	colorToRgb,
	colorToXyz,
	colorToXyzD50,
	numberToHex
};
//#endregion
export { convert };

//# sourceMappingURL=convert.js.map