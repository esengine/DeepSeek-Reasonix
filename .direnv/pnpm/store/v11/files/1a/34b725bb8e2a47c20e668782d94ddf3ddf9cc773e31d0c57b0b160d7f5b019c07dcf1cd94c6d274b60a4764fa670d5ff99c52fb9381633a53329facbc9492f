import { CacheItem, NullObject, createCacheKey, getCache, setCache } from "./cache.js";
import { isString, isStringOrNumber } from "./common.js";
import { CS_LAB, CS_LCH, FN_REL, FN_REL_CAPT, FN_VAR, NONE, SYN_COLOR_TYPE, SYN_FN_MATH_START, SYN_FN_VAR, SYN_MIX, VAL_SPEC } from "./constant.js";
import { NAMED_COLORS, convertColorToRgb } from "./color.js";
import { resolveColor } from "./resolve.js";
import { roundToPrecision, splitValue } from "./util.js";
import { resolveDimension, serializeCalc } from "./css-calc.js";
import { TokenType, tokenize } from "@csstools/css-tokenizer";
import { SyntaxFlag, color } from "@csstools/css-color-parser";
import { parseComponentValue } from "@csstools/css-parser-algorithms";
//#region src/js/relative-color.ts
/**
* relative-color
*/
var { CloseParen: PAREN_CLOSE, Comment: COMMENT, Delim: DELIM, Dimension: DIM, EOF, Function: FUNC, Ident: IDENT, Number: NUM, OpenParen: PAREN_OPEN, Percentage: PCT, Whitespace: W_SPACE } = TokenType;
var { HasNoneKeywords: KEY_NONE } = SyntaxFlag;
var NAMESPACE = "relative-color";
var OCT = 8;
var DEC = 10;
var HEX = 16;
var MAX_PCT = 100;
var MAX_RGB = 255;
var COLOR_CHANNELS = new Map([
	["color", [
		"r",
		"g",
		"b",
		"alpha"
	]],
	["hsl", [
		"h",
		"s",
		"l",
		"alpha"
	]],
	["hsla", [
		"h",
		"s",
		"l",
		"alpha"
	]],
	["hwb", [
		"h",
		"w",
		"b",
		"alpha"
	]],
	["lab", [
		"l",
		"a",
		"b",
		"alpha"
	]],
	["lch", [
		"l",
		"c",
		"h",
		"alpha"
	]],
	["oklab", [
		"l",
		"a",
		"b",
		"alpha"
	]],
	["oklch", [
		"l",
		"c",
		"h",
		"alpha"
	]],
	["rgb", [
		"r",
		"g",
		"b",
		"alpha"
	]],
	["rgba", [
		"r",
		"g",
		"b",
		"alpha"
	]]
]);
var REG_COLOR_CAPT = new RegExp(`^${FN_REL}(${SYN_COLOR_TYPE}|${SYN_MIX})\\s+`);
var REG_CS_HSL = /(?:hsla?|hwb)$/;
var REG_CS_CIE = new RegExp(`^(?:${CS_LAB}|${CS_LCH})$`);
var REG_FN_CALC_SUM = /^(?:abs|sig?n|cos|tan)\(/;
var REG_FN_MATH_START = new RegExp(SYN_FN_MATH_START);
var REG_FN_REL = new RegExp(FN_REL);
var REG_FN_REL_CAPT = new RegExp(`^${FN_REL_CAPT}`);
var REG_FN_REL_START = new RegExp(`^${FN_REL}`);
var REG_FN_VAR = new RegExp(SYN_FN_VAR);
/**
* resolve relative color channels
* @param value
*   - CSS color value
*   - system colors are not supported
* @param [opt] - options
* @param [opt.currentColor]
*   - color to use for `currentcolor` keyword
*   - if omitted, it will be treated as a missing color
*     i.e. `rgb(none none none / none)`
* @param [opt.customProperty]
*   - custom properties
*   - pair of `--` prefixed property name and value,
*     e.g. `customProperty: { '--some-color': '#0000ff' }`
*   - and/or `callback` function to get the value of the custom property,
*     e.g. `customProperty: { callback: someDeclaration.getPropertyValue }`
* @param [opt.dimension]
*   - dimension, convert relative length to pixels
*   - pair of unit and it's value as a number in pixels,
*     e.g. `dimension: { em: 12, rem: 16, vw: 10.26 }`
*   - and/or `callback` function to get the value as a number in pixels,
*     e.g. `dimension: { callback: convertUnitToPixel }`
* @param [opt.format]
*   - output format, one of below
*   - `computedValue` (default), [computed value][139] of the color
*   - `specifiedValue`, [specified value][140] of the color
*   - `hex`, hex color notation, i.e. `rrggbb`
*   - `hexAlpha`, hex color notation with alpha channel, i.e. `#rrggbbaa`
* @returns
*   - one of rgba?(), #rrggbb(aa)?, color-name, '(empty-string)',
*     color(color-space r g b / alpha), color(color-space x y z / alpha),
*     lab(l a b / alpha), lch(l c h / alpha), oklab(l a b / alpha),
*     oklch(l c h / alpha), null
*   - in `computedValue`, values are numbers, however `rgb()` values are
*     integers
*   - in `specifiedValue`, returns `empty string` for unknown and/or invalid
*     color
*   - in `hex`, returns `null` for `transparent`, and also returns `null` if
*     any of `r`, `g`, `b`, `alpha` is not a number
*   - in `hexAlpha`, returns `#00000000` for `transparent`,
*     however returns `null` if any of `r`, `g`, `b`, `alpha` is not a number
*/
function resolveColorChannels(tokens, opt = {}) {
	if (!Array.isArray(tokens)) throw new TypeError(`${tokens} is not an array.`);
	const { colorSpace = "", format = "" } = opt;
	const colorChannel = COLOR_CHANNELS.get(colorSpace);
	if (!colorChannel) return new NullObject();
	const mathFunc = /* @__PURE__ */ new Set();
	const channels = [
		[],
		[],
		[],
		[]
	];
	let i = 0;
	let nest = 0;
	let func = "";
	let precededPct = false;
	for (const token of tokens) {
		if (!Array.isArray(token)) throw new TypeError(`${token} is not an array.`);
		const [type, value, , , detail] = token;
		const channel = channels[i];
		if (Array.isArray(channel)) switch (type) {
			case DELIM:
				if (func) {
					if ((value === "+" || value === "-") && precededPct && !REG_FN_CALC_SUM.test(func)) return new NullObject();
					precededPct = false;
					channel.push(value);
				}
				break;
			case DIM: {
				if (!func || !REG_FN_CALC_SUM.test(func)) return new NullObject();
				const resolvedValue = resolveDimension(token, opt);
				if (isString(resolvedValue)) channel.push(resolvedValue);
				else channel.push(value);
				break;
			}
			case FUNC:
				channel.push(value);
				func = value;
				nest++;
				if (REG_FN_MATH_START.test(value)) mathFunc.add(nest);
				break;
			case IDENT:
				if (!colorChannel.includes(value)) return new NullObject();
				channel.push(value);
				if (!func) i++;
				break;
			case NUM:
				channel.push(Number(detail?.value));
				if (!func) i++;
				break;
			case PAREN_OPEN:
				channel.push(value);
				nest++;
				break;
			case PAREN_CLOSE:
				if (func) {
					if (channel[channel.length - 1] === " ") channel[channel.length - 1] = value;
					else channel.push(value);
					if (mathFunc.has(nest)) mathFunc.delete(nest);
					nest--;
					if (nest === 0) {
						func = "";
						i++;
					}
				}
				break;
			case PCT:
				if (!func) return new NullObject();
				else if (!REG_FN_CALC_SUM.test(func)) {
					let lastValue;
					for (let j = channel.length - 1; j >= 0; j--) if (channel[j] !== " ") {
						lastValue = channel[j];
						break;
					}
					if (lastValue === "+" || lastValue === "-") return new NullObject();
					else if (lastValue === "*" || lastValue === "/") precededPct = false;
					else precededPct = true;
				}
				channel.push(Number(detail?.value) / MAX_PCT);
				break;
			case W_SPACE:
				if (channel.length && func) {
					const lastValue = channel[channel.length - 1];
					if (typeof lastValue === "number") channel.push(value);
					else if (isString(lastValue) && !lastValue.endsWith("(") && lastValue !== " ") channel.push(value);
				}
				break;
			default: if (type !== COMMENT && type !== EOF && func) channel.push(value);
		}
	}
	const channelValues = [];
	for (const channel of channels) if (channel.length === 1) {
		const [resolvedValue] = channel;
		if (isStringOrNumber(resolvedValue)) channelValues.push(resolvedValue);
	} else if (channel.length) {
		const resolvedValue = serializeCalc(channel.join(""), { format });
		channelValues.push(resolvedValue);
	}
	return channelValues;
}
/**
* extract origin color
* @param value - CSS color value
* @param [opt] - options
* @returns origin color value
*/
function extractOriginColor(value, opt = {}) {
	const { colorScheme = "normal", currentColor = "", format = "" } = opt;
	if (isString(value)) {
		value = value.toLowerCase().trim();
		if (!value) return new NullObject();
		if (!REG_FN_REL_START.test(value)) return value;
	} else return new NullObject();
	const cacheKey = createCacheKey({
		namespace: NAMESPACE,
		name: "extractOriginColor",
		value
	}, opt);
	const cachedResult = getCache(cacheKey);
	if (cachedResult instanceof CacheItem) {
		if (cachedResult.isNull) return cachedResult;
		return cachedResult.item;
	}
	if (/currentcolor/.test(value)) if (currentColor) value = value.replace(/currentcolor/g, currentColor);
	else {
		setCache(cacheKey, null);
		return new NullObject();
	}
	let colorSpace = "";
	if (REG_FN_REL_CAPT.test(value)) [, colorSpace] = value.match(REG_FN_REL_CAPT);
	opt.colorSpace = colorSpace;
	if (value.includes("light-dark(")) {
		const [, originColor = ""] = splitValue(value.replace(new RegExp(`^${colorSpace}\\(`), "").replace(/\)$/, ""));
		const specifiedOriginColor = resolveColor(originColor, {
			colorScheme,
			format: VAL_SPEC
		});
		if (specifiedOriginColor === "") {
			setCache(cacheKey, null);
			return new NullObject();
		}
		if (format === "specifiedValue") value = value.replace(originColor, specifiedOriginColor);
		else {
			const resolvedOriginColor = resolveColor(specifiedOriginColor, opt);
			if (isString(resolvedOriginColor)) value = value.replace(originColor, resolvedOriginColor);
		}
	}
	if (REG_COLOR_CAPT.test(value)) {
		const [, originColor] = value.match(REG_COLOR_CAPT);
		const [, restValue] = value.split(originColor);
		if (/^[a-z]+$/.test(originColor)) {
			if (!/^transparent$/.test(originColor) && !Object.hasOwn(NAMED_COLORS, originColor)) {
				setCache(cacheKey, null);
				return new NullObject();
			}
		} else if (format === "specifiedValue") {
			const resolvedOriginColor = resolveColor(originColor, opt);
			if (isString(resolvedOriginColor)) value = value.replace(originColor, resolvedOriginColor);
		}
		if (format === "specifiedValue") {
			const channelValues = resolveColorChannels(tokenize({ css: restValue }), opt);
			if (channelValues instanceof NullObject) {
				setCache(cacheKey, null);
				return channelValues;
			}
			const [v1, v2, v3, v4] = channelValues;
			let channelValue = "";
			if (isStringOrNumber(v4)) channelValue = ` ${v1} ${v2} ${v3} / ${v4})`;
			else channelValue = ` ${channelValues.join(" ")})`;
			if (restValue !== channelValue) value = value.replace(restValue, channelValue);
		}
	} else {
		const [, restValue] = value.split(REG_FN_REL_START);
		const tokens = tokenize({ css: restValue });
		const originColor = [];
		let nest = 0;
		let tokenIndex = 0;
		for (const [type, tokenValue] of tokens) {
			tokenIndex++;
			switch (type) {
				case FUNC:
				case PAREN_OPEN:
					originColor.push(tokenValue);
					nest++;
					break;
				case PAREN_CLOSE: {
					const lastValue = originColor[originColor.length - 1];
					if (lastValue === " ") originColor[originColor.length - 1] = tokenValue;
					else if (isString(lastValue)) originColor.push(tokenValue);
					nest--;
					break;
				}
				case W_SPACE: {
					const lastValue = originColor[originColor.length - 1];
					if (isString(lastValue) && !lastValue.endsWith("(") && lastValue !== " ") originColor.push(tokenValue);
					break;
				}
				default: if (type !== COMMENT && type !== EOF) originColor.push(tokenValue);
			}
			if (nest === 0) break;
		}
		const resolvedOriginColor = resolveRelativeColor(originColor.join("").trim(), opt);
		if (resolvedOriginColor instanceof NullObject) {
			setCache(cacheKey, null);
			return resolvedOriginColor;
		}
		const channelValues = resolveColorChannels(tokens.slice(tokenIndex), opt);
		if (channelValues instanceof NullObject) {
			setCache(cacheKey, null);
			return channelValues;
		}
		const [v1, v2, v3, v4] = channelValues;
		let channelValue = "";
		if (isStringOrNumber(v4)) channelValue = ` ${v1} ${v2} ${v3} / ${v4})`;
		else channelValue = ` ${channelValues.join(" ")})`;
		value = value.replace(restValue, `${resolvedOriginColor}${channelValue}`);
	}
	setCache(cacheKey, value);
	return value;
}
/**
* resolve relative color
* @param value - CSS relative color value
* @param [opt] - options
* @returns resolved value
*/
function resolveRelativeColor(value, opt = {}) {
	const { format = "" } = opt;
	if (isString(value)) {
		if (REG_FN_VAR.test(value)) {
			if (format !== "specifiedValue") throw new SyntaxError(`Unexpected token ${FN_VAR} found.`);
			return value;
		} else if (!REG_FN_REL.test(value)) return value;
		value = value.toLowerCase().trim();
	} else throw new TypeError(`${value} is not a string.`);
	const cacheKey = createCacheKey({
		namespace: NAMESPACE,
		name: "resolveRelativeColor",
		value
	}, opt);
	const cachedResult = getCache(cacheKey);
	if (cachedResult instanceof CacheItem) {
		if (cachedResult.isNull) return cachedResult;
		return cachedResult.item;
	}
	const originColor = extractOriginColor(value, opt);
	if (originColor instanceof NullObject) {
		setCache(cacheKey, null);
		return originColor;
	}
	value = originColor;
	if (format === "specifiedValue") {
		if (value.startsWith("rgba(")) value = value.replace("rgba(", "rgb(");
		else if (value.startsWith("hsla(")) value = value.replace("hsla(", "hsl(");
		return value;
	}
	const parsedComponents = color(parseComponentValue(tokenize({ css: value })));
	if (!parsedComponents) {
		setCache(cacheKey, null);
		return new NullObject();
	}
	const { alpha: alphaComponent, channels: channelsComponent, colorNotation, syntaxFlags } = parsedComponents;
	let alpha;
	if (Number.isNaN(Number(alphaComponent))) if (syntaxFlags instanceof Set && syntaxFlags.has(KEY_NONE)) alpha = NONE;
	else alpha = 0;
	else alpha = roundToPrecision(Number(alphaComponent), OCT);
	let v1;
	let v2;
	let v3;
	[v1, v2, v3] = channelsComponent;
	let resolvedValue;
	if (REG_CS_CIE.test(colorNotation)) {
		const hasNone = syntaxFlags instanceof Set && syntaxFlags.has(KEY_NONE);
		if (Number.isNaN(v1)) if (hasNone) v1 = NONE;
		else v1 = 0;
		else v1 = roundToPrecision(v1, HEX);
		if (Number.isNaN(v2)) if (hasNone) v2 = NONE;
		else v2 = 0;
		else v2 = roundToPrecision(v2, HEX);
		if (Number.isNaN(v3)) if (hasNone) v3 = NONE;
		else v3 = 0;
		else v3 = roundToPrecision(v3, HEX);
		if (alpha === 1) resolvedValue = `${colorNotation}(${v1} ${v2} ${v3})`;
		else resolvedValue = `${colorNotation}(${v1} ${v2} ${v3} / ${alpha})`;
	} else if (REG_CS_HSL.test(colorNotation)) {
		if (Number.isNaN(v1)) v1 = 0;
		if (Number.isNaN(v2)) v2 = 0;
		if (Number.isNaN(v3)) v3 = 0;
		let [r, g, b] = convertColorToRgb(`${colorNotation}(${v1} ${v2} ${v3} / ${alpha})`);
		r = roundToPrecision(r / MAX_RGB, DEC);
		g = roundToPrecision(g / MAX_RGB, DEC);
		b = roundToPrecision(b / MAX_RGB, DEC);
		if (alpha === 1) resolvedValue = `color(srgb ${r} ${g} ${b})`;
		else resolvedValue = `color(srgb ${r} ${g} ${b} / ${alpha})`;
	} else {
		const cs = colorNotation === "rgb" ? "srgb" : colorNotation;
		const hasNone = syntaxFlags instanceof Set && syntaxFlags.has(KEY_NONE);
		if (Number.isNaN(v1)) if (hasNone) v1 = NONE;
		else v1 = 0;
		else v1 = roundToPrecision(v1, DEC);
		if (Number.isNaN(v2)) if (hasNone) v2 = NONE;
		else v2 = 0;
		else v2 = roundToPrecision(v2, DEC);
		if (Number.isNaN(v3)) if (hasNone) v3 = NONE;
		else v3 = 0;
		else v3 = roundToPrecision(v3, DEC);
		if (alpha === 1) resolvedValue = `color(${cs} ${v1} ${v2} ${v3})`;
		else resolvedValue = `color(${cs} ${v1} ${v2} ${v3} / ${alpha})`;
	}
	setCache(cacheKey, resolvedValue);
	return resolvedValue;
}
//#endregion
export { resolveRelativeColor };

//# sourceMappingURL=relative-color.js.map