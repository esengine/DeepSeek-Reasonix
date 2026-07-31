import { CacheItem, createCacheKey, getCache, setCache } from "./cache.js";
import { isString } from "./common.js";
import { SYN_COLOR_TYPE, SYN_MIX, VAL_SPEC } from "./constant.js";
import { NAMED_COLORS } from "./color.js";
import { resolveColor } from "./resolve.js";
import { TokenType, tokenize } from "@csstools/css-tokenizer";
//#region src/js/util.ts
/**
* util
*/
var { CloseParen: PAREN_CLOSE, Comma: COMMA, Comment: COMMENT, Delim: DELIM, EOF, Function: FUNC, OpenParen: PAREN_OPEN, Whitespace: W_SPACE } = TokenType;
var NAMESPACE = "util";
var DEC = 10;
var HEX = 16;
var DEG = 360;
var DEG_HALF = 180;
var REG_COLOR = new RegExp(`^(?:${SYN_COLOR_TYPE})$`);
var REG_DIMENSION = /^([+-]?(?:\d+(?:\.\d+)?|\.\d+)(?:e[+-]?\d+)?)([a-z]*)$/i;
var REG_FN_COLOR = /^(?:(?:ok)?l(?:ab|ch)|color(?:-mix)?|hsla?|hwb|rgba?|var)\(/;
var REG_MIX = new RegExp(SYN_MIX);
var REG_DASHED_IDENT = /--[\w-]+/g;
var REG_COMMA = /^,$/;
var REG_SLASH = /^\/$/;
var REG_WHITESPACE = /^\s+$/;
/**
* split value
* NOTE: comments are stripped, it can be preserved if, in the options param,
* `delimiter` is either ',' or '/' and with `preserveComment` set to `true`
* @param value - CSS value
* @param [opt] - options
* @returns array of values
*/
var splitValue = (value, opt = {}) => {
	if (!isString(value)) throw new TypeError(`${value} is not a string.`);
	const strValue = value.trim();
	const { delimiter = " ", preserveComment = false } = opt;
	const cacheKey = createCacheKey({
		namespace: NAMESPACE,
		name: "splitValue",
		value: strValue
	}, {
		delimiter,
		preserveComment
	});
	const cachedResult = getCache(cacheKey);
	if (cachedResult instanceof CacheItem) return cachedResult.item;
	let regDelimiter;
	switch (delimiter) {
		case ",":
			regDelimiter = REG_COMMA;
			break;
		case "/":
			regDelimiter = REG_SLASH;
			break;
		default: regDelimiter = REG_WHITESPACE;
	}
	const tokens = tokenize({ css: strValue });
	let nest = 0;
	let currentStr = "";
	const res = [];
	for (const [type, val] of tokens) switch (type) {
		case COMMA:
		case DELIM:
			if (nest === 0 && regDelimiter.test(val)) {
				res.push(currentStr.trim());
				currentStr = "";
			} else currentStr += val;
			break;
		case COMMENT:
			if (preserveComment && (delimiter === "," || delimiter === "/")) currentStr += val;
			break;
		case FUNC:
		case PAREN_OPEN:
			currentStr += val;
			nest++;
			break;
		case PAREN_CLOSE:
			currentStr += val;
			nest--;
			break;
		case W_SPACE:
			if (regDelimiter.test(val)) if (nest === 0) {
				if (currentStr) {
					res.push(currentStr.trim());
					currentStr = "";
				}
			} else currentStr += " ";
			else if (!currentStr.endsWith(" ")) currentStr += " ";
			break;
		default: if (type === EOF) {
			res.push(currentStr.trim());
			currentStr = "";
		} else currentStr += val;
	}
	setCache(cacheKey, res);
	return res;
};
/**
* extract dashed-ident tokens
* @param value - CSS value
* @returns array of dashed-ident tokens
*/
var extractDashedIdent = (value) => {
	if (!isString(value)) throw new TypeError(`${value} is not a string.`);
	const strValue = value.trim();
	const cacheKey = createCacheKey({
		namespace: NAMESPACE,
		name: "extractDashedIdent",
		value: strValue
	});
	const cachedResult = getCache(cacheKey);
	if (cachedResult instanceof CacheItem) return cachedResult.item;
	const matches = strValue.match(REG_DASHED_IDENT);
	const res = matches ? [...new Set(matches)] : [];
	setCache(cacheKey, res);
	return res;
};
/**
* is color
* @param value - CSS value
* @param [opt] - options
* @returns result
*/
var isColor = (value, opt = {}) => {
	if (!isString(value)) return false;
	const str = value.toLowerCase().trim();
	if (!str) return false;
	if (/^[a-z]+$/.test(str)) return str === "currentcolor" || str === "transparent" || Object.hasOwn(NAMED_COLORS, str);
	if (REG_COLOR.test(str) || REG_MIX.test(str)) return true;
	if (REG_FN_COLOR.test(str)) {
		const colorOpt = {
			...opt,
			nullable: true
		};
		if (!colorOpt.format) colorOpt.format = VAL_SPEC;
		return !!resolveColor(str, colorOpt);
	}
	return false;
};
/**
* round to specified precision
* @param value - numeric value
* @param bit - minimum bits
* @returns rounded value
*/
var roundToPrecision = (value, bit = 0) => {
	if (!Number.isFinite(value)) throw new TypeError(`${value} is not a finite number.`);
	if (!Number.isFinite(bit)) throw new TypeError(`${bit} is not a finite number.`);
	if (bit < 0 || bit > HEX) throw new RangeError(`${bit} is not between 0 and ${HEX}.`);
	if (bit === 0) return Math.round(value);
	const precision = bit === HEX ? 6 : bit < DEC ? 4 : 5;
	return parseFloat(value.toPrecision(precision));
};
/**
* interpolate hue
* @param hueA - hue value
* @param hueB - hue value
* @param arc - shorter | longer | increasing | decreasing
* @returns result - [hueA, hueB]
*/
var interpolateHue = (hueA, hueB, arc = "shorter") => {
	if (!Number.isFinite(hueA)) throw new TypeError(`${hueA} is not a finite number.`);
	if (!Number.isFinite(hueB)) throw new TypeError(`${hueB} is not a finite number.`);
	let a = hueA;
	let b = hueB;
	switch (arc) {
		case "decreasing":
			if (b > a) a += DEG;
			break;
		case "increasing":
			if (b < a) b += DEG;
			break;
		case "longer":
			if (b > a && b < a + DEG_HALF) a += DEG;
			else if (b > a - DEG_HALF && b <= a) b += DEG;
			break;
		default: if (b > a + DEG_HALF) a += DEG;
		else if (b < a - DEG_HALF) b += DEG;
	}
	return [a, b];
};
var absoluteFontSize = new Map([
	["xx-small", 9 / 16],
	["x-small", 5 / 8],
	["small", 13 / 16],
	["medium", 1],
	["large", 9 / 8],
	["x-large", 3 / 2],
	["xx-large", 2],
	["xxx-large", 3]
]);
var relativeFontSize = new Map([["smaller", 1 / 1.2], ["larger", 1.2]]);
var absoluteLength = new Map([
	["cm", 96 / 2.54],
	["mm", 96 / 25.4],
	["q", 96 / 101.6],
	["in", 96],
	["pc", 16],
	["pt", 96 / 72],
	["px", 1]
]);
var relativeLength = new Map([
	["rcap", 1],
	["rch", .5],
	["rem", 1],
	["rex", .5],
	["ric", 1],
	["rlh", 1.2]
]);
/**
* resolve length in pixels
* @param value - value
* @param unit - unit
* @param [opt] - options
* @returns pixelated value
*/
var resolveLengthInPixels = (value, unit, opt = {}) => {
	const { dimension = {} } = opt;
	const { callback, em, rem, vh, vw } = dimension;
	if (isString(value)) {
		const str = value.toLowerCase().trim();
		const ratio = absoluteFontSize.get(str);
		if (ratio !== void 0) return ratio * rem;
		const relRatio = relativeFontSize.get(str);
		if (relRatio !== void 0) return relRatio * em;
		return NaN;
	}
	if (Number.isFinite(value) && unit) {
		const u = unit.toLowerCase();
		if (Object.hasOwn(dimension, u)) return value * Number(dimension[u]);
		if (typeof callback === "function") return value * (callback(u) ?? NaN);
		const absRatio = absoluteLength.get(u);
		if (absRatio !== void 0) return value * absRatio;
		const relRatio = relativeLength.get(u);
		if (relRatio !== void 0) return value * relRatio * rem;
		const rUnitRatio = relativeLength.get(`r${u}`);
		if (rUnitRatio !== void 0) return value * rUnitRatio * em;
		switch (u) {
			case "vb":
			case "vi": return value * vw;
			case "vmax": return value * Math.max(vh, vw);
			case "vmin": return value * Math.min(vh, vw);
			default:
		}
	}
	return NaN;
};
/**
* is absolute size or length
* @param value - value
* @param unit - unit
* @returns result
*/
var isAbsoluteSizeOrLength = (value, unit) => {
	if (isString(value)) return absoluteFontSize.has(value.toLowerCase().trim());
	if (isString(unit)) return absoluteLength.has(unit.toLowerCase().trim());
	return value === 0;
};
/**
* is absolute font size
* @param css - css
* @returns result
*/
var isAbsoluteFontSize = (css) => {
	if (!isString(css)) return false;
	const str = css.trim();
	if (isAbsoluteSizeOrLength(str, void 0)) return true;
	const match = str.match(REG_DIMENSION);
	return match ? isAbsoluteSizeOrLength(Number(match[1]), match[2] || void 0) : false;
};
//#endregion
export { extractDashedIdent, interpolateHue, isAbsoluteFontSize, isAbsoluteSizeOrLength, isColor, resolveLengthInPixels, roundToPrecision, splitValue };

//# sourceMappingURL=util.js.map