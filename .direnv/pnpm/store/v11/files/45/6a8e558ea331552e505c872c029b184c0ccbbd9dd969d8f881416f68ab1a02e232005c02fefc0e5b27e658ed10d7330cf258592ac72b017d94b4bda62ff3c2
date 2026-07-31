import { CacheItem, NullObject, createCacheKey, getCache, setCache } from "./cache.js";
import { isString } from "./common.js";
import { SYN_FN_CALC, SYN_FN_VAR } from "./constant.js";
import { isColor } from "./util.js";
import { cssCalc } from "./css-calc.js";
import { TokenType, tokenize } from "@csstools/css-tokenizer";
//#region src/js/css-var.ts
/**
* css-var
*/
var { CloseParen: PAREN_CLOSE, Comment: COMMENT, EOF, Ident: IDENT, Whitespace: W_SPACE } = TokenType;
var NAMESPACE = "css-var";
var REG_FN_CALC = new RegExp(SYN_FN_CALC);
var REG_FN_VAR = new RegExp(SYN_FN_VAR);
var REG_CSS_WIDE_KEYWORD = /^(?:inherit|initial|revert(?:-layer)?|unset)$/;
/**
* resolve custom property
* @param tokens - CSS tokens
* @param [opt] - options
* @returns result - [tokens, resolvedValue]
*/
function resolveCustomProperty(tokens, opt = {}) {
	if (!Array.isArray(tokens)) throw new TypeError(`${tokens} is not an array.`);
	const { customProperty = {} } = opt;
	const items = [];
	while (tokens.length) {
		const token = tokens.shift();
		if (!token) break;
		if (!Array.isArray(token)) throw new TypeError(`${token} is not an array.`);
		const [type, value] = token;
		if (type === PAREN_CLOSE) break;
		if (value === "var(") {
			const [, item] = resolveCustomProperty(tokens, opt);
			if (item) items.push(item);
		} else if (type === IDENT) {
			if (value.startsWith("--")) {
				let item;
				if (Object.hasOwn(customProperty, value)) item = customProperty[value];
				else if (typeof customProperty.callback === "function") item = customProperty.callback(value);
				if (item) items.push(item);
			} else if (value) items.push(value);
		}
	}
	let resolveAsColor = false;
	if (items.length > 1) resolveAsColor = isColor(items[items.length - 1]);
	let resolvedValue = "";
	for (let item of items) {
		item = item.trim();
		if (REG_FN_VAR.test(item)) {
			const resolvedItem = resolveVar(item, opt);
			if (isString(resolvedItem)) {
				if (!resolveAsColor || isColor(resolvedItem)) resolvedValue = resolvedItem;
			}
		} else if (REG_FN_CALC.test(item)) {
			item = cssCalc(item, opt);
			if (!resolveAsColor || isColor(item)) resolvedValue = item;
		} else if (item && !REG_CSS_WIDE_KEYWORD.test(item)) {
			if (!resolveAsColor || isColor(item)) resolvedValue = item;
		}
		if (resolvedValue) break;
	}
	return [tokens, resolvedValue];
}
/**
* parse tokens
* @param tokens - CSS tokens
* @param [opt] - options
* @returns parsed tokens
*/
function parseTokens(tokens, opt = {}) {
	const res = [];
	while (tokens.length) {
		const token = tokens.shift();
		if (!token) break;
		const [type = "", value = ""] = token;
		if (value === "var(") {
			const [, resolvedValue] = resolveCustomProperty(tokens, opt);
			if (!resolvedValue) return new NullObject();
			res.push(resolvedValue);
		} else switch (type) {
			case PAREN_CLOSE:
				if (res.length) if (res[res.length - 1] === " ") res[res.length - 1] = value;
				else res.push(value);
				else res.push(value);
				break;
			case W_SPACE:
				if (res.length) {
					const lastValue = res[res.length - 1];
					if (isString(lastValue) && !lastValue.endsWith("(") && lastValue !== " ") res.push(value);
				}
				break;
			default: if (type !== COMMENT && type !== EOF) res.push(value);
		}
	}
	return res;
}
/**
* resolve CSS var()
* @param value - CSS value including var()
* @param [opt] - options
* @returns resolved value
*/
function resolveVar(value, opt = {}) {
	const { format = "" } = opt;
	if (isString(value)) {
		if (!REG_FN_VAR.test(value) || format === "specifiedValue") return value;
		value = value.trim();
	} else throw new TypeError(`${value} is not a string.`);
	const cacheKey = createCacheKey({
		namespace: NAMESPACE,
		name: "resolveVar",
		value
	}, opt);
	const cachedResult = getCache(cacheKey);
	if (cachedResult instanceof CacheItem) {
		if (cachedResult.isNull) return cachedResult;
		return cachedResult.item;
	}
	const values = parseTokens(tokenize({ css: value }), opt);
	if (Array.isArray(values)) {
		let color = values.join("");
		if (REG_FN_CALC.test(color)) color = cssCalc(color, opt);
		setCache(cacheKey, color);
		return color;
	} else {
		setCache(cacheKey, null);
		return new NullObject();
	}
}
/**
* CSS var()
* @param value - CSS value including var()
* @param [opt] - options
* @returns resolved value
*/
var cssVar = (value, opt = {}) => {
	const resolvedValue = resolveVar(value, opt);
	if (isString(resolvedValue)) return resolvedValue;
	return "";
};
//#endregion
export { cssVar, resolveVar };

//# sourceMappingURL=css-var.js.map