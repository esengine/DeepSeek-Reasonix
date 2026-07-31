import { CacheItem, NullObject, createCacheKey, getCache, setCache } from "./cache.js";
import { isString, isStringOrNumber } from "./common.js";
import { ANGLE, LENGTH, NUM, SYN_FN_CALC, SYN_FN_MATH_START, SYN_FN_VAR, SYN_FN_VAR_START } from "./constant.js";
import { resolveLengthInPixels, roundToPrecision } from "./util.js";
import { resolveVar } from "./css-var.js";
import { calc } from "@csstools/css-calc";
import { TokenType, tokenize } from "@csstools/css-tokenizer";
//#region src/js/css-calc.ts
/**
* css-calc
*/
var { CloseParen: PAREN_CLOSE, Comment: COMMENT, Dimension: DIM, EOF, Function: FUNC, OpenParen: PAREN_OPEN, Whitespace: W_SPACE } = TokenType;
var NAMESPACE = "css-calc";
var TRIA = 3;
var HEX = 16;
var MAX_PCT = 100;
var REG_FN_CALC = new RegExp(SYN_FN_CALC);
var REG_FN_CALC_NUM = new RegExp(`^calc\\((${NUM})\\)$`);
var REG_FN_MATH_START = new RegExp(SYN_FN_MATH_START);
var REG_FN_VAR = new RegExp(SYN_FN_VAR);
var REG_FN_VAR_START = new RegExp(SYN_FN_VAR_START);
var REG_OPERATOR = /\s[*+/-]\s/;
var REG_PAREN_OPEN = /\($/;
var REG_TYPE_DIM = new RegExp(`^(${NUM})(${ANGLE}|${LENGTH})$`);
var REG_TYPE_DIM_PCT = new RegExp(`^(${NUM})(${ANGLE}|${LENGTH}|%)$`);
var REG_TYPE_PCT = new RegExp(`^(${NUM})%$`);
/**
* Calclator
*/
var Calculator = class {
	#hasNum;
	#numSum;
	#numMul;
	#hasPct;
	#pctSum;
	#pctMul;
	#hasDim;
	#dimSum;
	#dimSub;
	#dimMul;
	#dimDiv;
	#hasEtc;
	#etcSum;
	#etcSub;
	#etcMul;
	#etcDiv;
	#calcOpts;
	/**
	* constructor
	*/
	constructor() {
		this.#hasNum = false;
		this.#numSum = [];
		this.#numMul = [];
		this.#hasPct = false;
		this.#pctSum = [];
		this.#pctMul = [];
		this.#hasDim = false;
		this.#dimSum = [];
		this.#dimSub = [];
		this.#dimMul = [];
		this.#dimDiv = [];
		this.#hasEtc = false;
		this.#etcSum = [];
		this.#etcSub = [];
		this.#etcMul = [];
		this.#etcDiv = [];
		this.#calcOpts = { toCanonicalUnits: true };
	}
	get hasNum() {
		return this.#hasNum;
	}
	set hasNum(value) {
		this.#hasNum = !!value;
	}
	get numSum() {
		return this.#numSum;
	}
	get numMul() {
		return this.#numMul;
	}
	get hasPct() {
		return this.#hasPct;
	}
	set hasPct(value) {
		this.#hasPct = !!value;
	}
	get pctSum() {
		return this.#pctSum;
	}
	get pctMul() {
		return this.#pctMul;
	}
	get hasDim() {
		return this.#hasDim;
	}
	set hasDim(value) {
		this.#hasDim = !!value;
	}
	get dimSum() {
		return this.#dimSum;
	}
	get dimSub() {
		return this.#dimSub;
	}
	get dimMul() {
		return this.#dimMul;
	}
	get dimDiv() {
		return this.#dimDiv;
	}
	get hasEtc() {
		return this.#hasEtc;
	}
	set hasEtc(value) {
		this.#hasEtc = !!value;
	}
	get etcSum() {
		return this.#etcSum;
	}
	get etcSub() {
		return this.#etcSub;
	}
	get etcMul() {
		return this.#etcMul;
	}
	get etcDiv() {
		return this.#etcDiv;
	}
	/**
	* clear values
	* @returns void
	*/
	clear() {
		this.#hasNum = false;
		this.#numSum.length = 0;
		this.#numMul.length = 0;
		this.#hasPct = false;
		this.#pctSum.length = 0;
		this.#pctMul.length = 0;
		this.#hasDim = false;
		this.#dimSum.length = 0;
		this.#dimSub.length = 0;
		this.#dimMul.length = 0;
		this.#dimDiv.length = 0;
		this.#hasEtc = false;
		this.#etcSum.length = 0;
		this.#etcSub.length = 0;
		this.#etcMul.length = 0;
		this.#etcDiv.length = 0;
	}
	/**
	* sort values
	* @param values - values
	* @returns sorted values
	*/
	sort(values = []) {
		const arr = [...values];
		if (arr.length > 1) arr.sort((a, b) => {
			let res;
			if (REG_TYPE_DIM_PCT.test(a) && REG_TYPE_DIM_PCT.test(b)) {
				const [, valA, unitA] = a.match(REG_TYPE_DIM_PCT);
				const [, valB, unitB] = b.match(REG_TYPE_DIM_PCT);
				if (unitA === unitB) if (Number(valA) === Number(valB)) res = 0;
				else if (Number(valA) > Number(valB)) res = 1;
				else res = -1;
				else if (unitA > unitB) res = 1;
				else res = -1;
			} else if (a === b) res = 0;
			else if (a > b) res = 1;
			else res = -1;
			return res;
		});
		return arr;
	}
	/**
	* multiply values
	* @returns resolved value
	*/
	multiply() {
		const value = [];
		let num;
		if (this.#hasNum) {
			num = 1;
			for (const i of this.#numMul) {
				num *= i;
				if (num === 0 || !Number.isFinite(num) || Number.isNaN(num)) break;
			}
			if (!this.#hasPct && !this.#hasDim && !this.hasEtc) {
				if (Number.isFinite(num)) num = roundToPrecision(num, HEX);
				value.push(num);
			}
		}
		if (this.#hasPct) {
			if (typeof num !== "number") num = 1;
			for (const i of this.#pctMul) {
				num *= i;
				if (num === 0 || !Number.isFinite(num) || Number.isNaN(num)) break;
			}
			if (Number.isFinite(num)) num = `${roundToPrecision(num, HEX)}%`;
			if (!this.#hasDim && !this.hasEtc) value.push(num);
		}
		if (this.#hasDim) {
			let dim = "";
			let mul = "";
			let div = "";
			if (this.#dimMul.length) if (this.#dimMul.length === 1) [mul] = this.#dimMul;
			else mul = `${this.sort(this.#dimMul).join(" * ")}`;
			if (this.#dimDiv.length) if (this.#dimDiv.length === 1) [div] = this.#dimDiv;
			else div = `${this.sort(this.#dimDiv).join(" * ")}`;
			if (Number.isFinite(num)) {
				if (mul) if (div) if (div.includes("*")) dim = calc(`calc(${num} * ${mul} / (${div}))`, this.#calcOpts);
				else dim = calc(`calc(${num} * ${mul} / ${div})`, this.#calcOpts);
				else dim = calc(`calc(${num} * ${mul})`, this.#calcOpts);
				else if (div.includes("*")) dim = calc(`calc(${num} / (${div}))`, this.#calcOpts);
				else dim = calc(`calc(${num} / ${div})`, this.#calcOpts);
				value.push(dim.replace(/^calc/, ""));
			} else {
				if (!value.length && num !== void 0) value.push(num);
				if (mul) {
					if (div) if (div.includes("*")) dim = calc(`calc(${mul} / (${div}))`, this.#calcOpts);
					else dim = calc(`calc(${mul} / ${div})`, this.#calcOpts);
					else dim = calc(`calc(${mul})`, this.#calcOpts);
					if (value.length) value.push("*", dim.replace(/^calc/, ""));
					else value.push(dim.replace(/^calc/, ""));
				} else {
					dim = calc(`calc(${div})`, this.#calcOpts);
					if (value.length) value.push("/", dim.replace(/^calc/, ""));
					else value.push("1", "/", dim.replace(/^calc/, ""));
				}
			}
		}
		if (this.#hasEtc) {
			if (this.#etcMul.length) {
				if (!value.length && num !== void 0) value.push(num);
				const mul = this.sort(this.#etcMul).join(" * ");
				if (value.length) value.push(`* ${mul}`);
				else value.push(`${mul}`);
			}
			if (this.#etcDiv.length) {
				const div = this.sort(this.#etcDiv).join(" * ");
				if (div.includes("*")) if (value.length) value.push(`/ (${div})`);
				else value.push(`1 / (${div})`);
				else if (value.length) value.push(`/ ${div}`);
				else value.push(`1 / ${div}`);
			}
		}
		if (value.length) return value.join(" ");
		return "";
	}
	/**
	* sum values
	* @returns resolved value
	*/
	sum() {
		const value = [];
		if (this.#hasNum) {
			let num = 0;
			for (const i of this.#numSum) {
				num += i;
				if (!Number.isFinite(num) || Number.isNaN(num)) break;
			}
			value.push(num);
		}
		if (this.#hasPct) {
			let num = 0;
			for (const i of this.#pctSum) {
				num += i;
				if (!Number.isFinite(num)) break;
			}
			if (Number.isFinite(num)) num = `${num}%`;
			if (value.length) value.push(`+ ${num}`);
			else value.push(num);
		}
		if (this.#hasDim) {
			let dim, sum, sub;
			if (this.#dimSum.length) sum = this.sort(this.#dimSum).join(" + ");
			if (this.#dimSub.length) sub = this.sort(this.#dimSub).join(" + ");
			if (sum) if (sub) if (sub.includes("-")) dim = calc(`calc(${sum} - (${sub}))`, this.#calcOpts);
			else dim = calc(`calc(${sum} - ${sub})`, this.#calcOpts);
			else dim = calc(`calc(${sum})`, this.#calcOpts);
			else dim = calc(`calc(-1 * (${sub}))`, this.#calcOpts);
			if (value.length) value.push("+", dim.replace(/^calc/, ""));
			else value.push(dim.replace(/^calc/, ""));
		}
		if (this.#hasEtc) {
			if (this.#etcSum.length) {
				const sum = this.sort(this.#etcSum).map((item) => {
					let res;
					if (REG_OPERATOR.test(item) && !item.startsWith("(") && !item.endsWith(")")) res = `(${item})`;
					else res = item;
					return res;
				}).join(" + ");
				if (value.length) if (this.#etcSum.length > 1) value.push(`+ (${sum})`);
				else value.push(`+ ${sum}`);
				else value.push(`${sum}`);
			}
			if (this.#etcSub.length) {
				const sub = this.sort(this.#etcSub).map((item) => {
					let res;
					if (REG_OPERATOR.test(item) && !item.startsWith("(") && !item.endsWith(")")) res = `(${item})`;
					else res = item;
					return res;
				}).join(" + ");
				if (value.length) if (this.#etcSub.length > 1) value.push(`- (${sub})`);
				else value.push(`- ${sub}`);
				else if (this.#etcSub.length > 1) value.push(`-1 * (${sub})`);
				else value.push(`-1 * ${sub}`);
			}
		}
		if (value.length) return value.join(" ");
		return "";
	}
};
/**
* sort calc values
* @param values - values to sort
* @param [finalize] - finalize values
* @returns sorted values
*/
var sortCalcValues = (values = [], finalize = false) => {
	if (values.length < TRIA) throw new Error(`Unexpected array length ${values.length}.`);
	const start = values.shift();
	if (!isString(start) || !start.endsWith("(")) throw new Error(`Unexpected token ${start}.`);
	const end = values.pop();
	if (end !== ")") throw new Error(`Unexpected token ${end}.`);
	if (values.length === 1) {
		const [value] = values;
		if (!isStringOrNumber(value)) throw new Error(`Unexpected token ${value}.`);
		return `${start}${value}${end}`;
	}
	const sortedValues = [];
	const cal = new Calculator();
	let operator = "";
	const l = values.length;
	let hasAddSub = false;
	for (let i = 0; i < l; i++) {
		const value = values[i];
		if (!isStringOrNumber(value)) throw new Error(`Unexpected token ${value}.`);
		if (value === "*" || value === "/") operator = value;
		else if (value === "+" || value === "-") {
			const sortedValue = cal.multiply();
			if (sortedValue) sortedValues.push(sortedValue, value);
			hasAddSub = true;
			cal.clear();
			operator = "";
		} else {
			const numValue = Number(value);
			const strValue = `${value}`;
			switch (operator) {
				case "/":
					if (Number.isFinite(numValue)) {
						cal.hasNum = true;
						cal.numMul.push(1 / numValue);
					} else if (REG_TYPE_PCT.test(strValue)) {
						const [, val] = strValue.match(REG_TYPE_PCT);
						cal.hasPct = true;
						cal.pctMul.push(MAX_PCT * MAX_PCT / Number(val));
					} else if (REG_TYPE_DIM.test(strValue)) {
						cal.hasDim = true;
						cal.dimDiv.push(strValue);
					} else {
						cal.hasEtc = true;
						cal.etcDiv.push(strValue);
					}
					break;
				default: if (Number.isFinite(numValue)) {
					cal.hasNum = true;
					cal.numMul.push(numValue);
				} else if (REG_TYPE_PCT.test(strValue)) {
					const [, val] = strValue.match(REG_TYPE_PCT);
					cal.hasPct = true;
					cal.pctMul.push(Number(val));
				} else if (REG_TYPE_DIM.test(strValue)) {
					cal.hasDim = true;
					cal.dimMul.push(strValue);
				} else {
					cal.hasEtc = true;
					cal.etcMul.push(strValue);
				}
			}
		}
		if (i === l - 1) {
			const sortedValue = cal.multiply();
			if (sortedValue) sortedValues.push(sortedValue);
			cal.clear();
			operator = "";
		}
	}
	let resolvedValue = "";
	if (finalize && hasAddSub) {
		const finalizedValues = [];
		cal.clear();
		operator = "";
		const l = sortedValues.length;
		for (let i = 0; i < l; i++) {
			const value = sortedValues[i];
			if (isStringOrNumber(value)) if (value === "+" || value === "-") operator = value;
			else {
				const numValue = Number(value);
				const strValue = `${value}`;
				switch (operator) {
					case "-":
						if (Number.isFinite(numValue)) {
							cal.hasNum = true;
							cal.numSum.push(-1 * numValue);
						} else if (REG_TYPE_PCT.test(strValue)) {
							const [, val] = strValue.match(REG_TYPE_PCT);
							cal.hasPct = true;
							cal.pctSum.push(-1 * Number(val));
						} else if (REG_TYPE_DIM.test(strValue)) {
							cal.hasDim = true;
							cal.dimSub.push(strValue);
						} else {
							cal.hasEtc = true;
							cal.etcSub.push(strValue);
						}
						break;
					default: if (Number.isFinite(numValue)) {
						cal.hasNum = true;
						cal.numSum.push(numValue);
					} else if (REG_TYPE_PCT.test(strValue)) {
						const [, val] = strValue.match(REG_TYPE_PCT);
						cal.hasPct = true;
						cal.pctSum.push(Number(val));
					} else if (REG_TYPE_DIM.test(strValue)) {
						cal.hasDim = true;
						cal.dimSum.push(strValue);
					} else {
						cal.hasEtc = true;
						cal.etcSum.push(strValue);
					}
				}
			}
			if (i === l - 1) {
				const sortedValue = cal.sum();
				if (sortedValue) finalizedValues.push(sortedValue);
				cal.clear();
				operator = "";
			}
		}
		resolvedValue = finalizedValues.join(" ").replace(/\+\s-/g, "- ");
	} else resolvedValue = sortedValues.join(" ").replace(/\+\s-/g, "- ");
	if (resolvedValue.startsWith("(") && resolvedValue.endsWith(")") && resolvedValue.lastIndexOf("(") === 0 && resolvedValue.indexOf(")") === resolvedValue.length - 1) resolvedValue = resolvedValue.substring(1, resolvedValue.length - 1);
	return `${start}${resolvedValue}${end}`;
};
/**
* resolve AST node
* @param node - AST node
* @param isRoot - is root node
* @returns resolved value
*/
var resolveNode = (node, isRoot) => {
	const flatItems = [];
	for (const item of node) if (Array.isArray(item)) flatItems.push(resolveNode(item, false));
	else flatItems.push(item);
	if (isRoot) {
		if (flatItems.length >= TRIA) return sortCalcValues(flatItems, true);
		const joined = flatItems.join("");
		return joined.startsWith("calc(") ? joined : `calc(${joined})`;
	}
	if (flatItems.length >= TRIA) {
		let serialized = sortCalcValues(flatItems, false);
		if (REG_FN_VAR_START.test(serialized)) serialized = calc(serialized, { toCanonicalUnits: true });
		return serialized;
	}
	return flatItems.join("");
};
/**
* serialize calc
* @param value - CSS value
* @param [opt] - options
* @returns serialized value
*/
var serializeCalc = (value, opt = {}) => {
	const { format = "" } = opt;
	if (isString(value)) {
		if (!REG_FN_VAR_START.test(value) || format !== "specifiedValue") return value;
		value = value.toLowerCase().trim();
	} else throw new TypeError(`${value} is not a string.`);
	const cacheKey = createCacheKey({
		namespace: NAMESPACE,
		name: "serializeCalc",
		value
	}, opt);
	const cachedResult = getCache(cacheKey);
	if (cachedResult instanceof CacheItem) return cachedResult.item;
	const items = tokenize({ css: value }).map((token) => {
		const [type, val] = token;
		let res = "";
		if (type !== W_SPACE && type !== COMMENT) res = val;
		return res;
	}).filter((v) => v);
	const stack = [[]];
	for (const item of items) if (REG_PAREN_OPEN.test(item)) {
		const newNode = [item];
		const parent = stack[stack.length - 1];
		if (parent) parent.push(newNode);
		stack.push(newNode);
	} else if (item === ")") if (stack.length > 1) {
		const currentLevel = stack.pop();
		if (currentLevel) currentLevel.push(item);
	} else {
		const root = stack[0];
		if (root) root.push(item);
	}
	else {
		const parent = stack[stack.length - 1];
		if (parent) parent.push(item);
	}
	let serializedCalc = "";
	const rootItems = stack[0];
	if (rootItems) if (rootItems.length === 1 && Array.isArray(rootItems[0])) serializedCalc = resolveNode(rootItems[0], true);
	else {
		const flatItems = [];
		for (const item of rootItems) if (Array.isArray(item)) flatItems.push(resolveNode(item, false));
		else flatItems.push(item);
		if (flatItems.length >= TRIA) serializedCalc = sortCalcValues(flatItems, true);
		else {
			const firstItem = flatItems[0] || "";
			serializedCalc = isString(firstItem) && firstItem.startsWith("calc(") ? firstItem : `calc(${firstItem})`;
		}
	}
	setCache(cacheKey, serializedCalc);
	return serializedCalc;
};
/**
* resolve dimension
* @param token - CSS token
* @param [opt] - options
* @returns resolved value
*/
var resolveDimension = (token, opt = {}) => {
	if (!Array.isArray(token)) throw new TypeError(`${token} is not an array.`);
	const [, , , , detail = {}] = token;
	const { unit, value } = detail;
	if (unit === "px") return `${value}${unit}`;
	const pixelValue = resolveLengthInPixels(Number(value), unit, opt);
	if (Number.isFinite(pixelValue)) return `${roundToPrecision(pixelValue, HEX)}px`;
	return new NullObject();
};
/**
* parse tokens
* @param tokens - CSS tokens
* @param [opt] - options
* @returns parsed tokens
*/
var parseTokens = (tokens, opt = {}) => {
	if (!Array.isArray(tokens)) throw new TypeError(`${tokens} is not an array.`);
	const { format = "" } = opt;
	const mathFunc = /* @__PURE__ */ new Set();
	let nest = 0;
	const res = [];
	for (const token of tokens) {
		if (!Array.isArray(token)) throw new TypeError(`${token} is not an array.`);
		const [type = "", value = ""] = token;
		switch (type) {
			case DIM:
				if (format === "specifiedValue" && !mathFunc.has(nest)) res.push(value);
				else {
					const resolvedValue = resolveDimension(token, opt);
					if (isString(resolvedValue)) res.push(resolvedValue);
					else res.push(value);
				}
				break;
			case FUNC:
			case PAREN_OPEN:
				res.push(value);
				nest++;
				if (REG_FN_MATH_START.test(value)) mathFunc.add(nest);
				break;
			case PAREN_CLOSE:
				if (res.length) if (res[res.length - 1] === " ") res.splice(-1, 1, value);
				else res.push(value);
				else res.push(value);
				if (mathFunc.has(nest)) mathFunc.delete(nest);
				nest--;
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
};
/**
* CSS calc()
* @param value - CSS value including calc()
* @param [opt] - options
* @returns resolved value
*/
var cssCalc = (value, opt = {}) => {
	const { format = "" } = opt;
	if (isString(value)) {
		if (REG_FN_VAR.test(value)) if (format === "specifiedValue") return value;
		else {
			const resolvedValue = resolveVar(value, opt);
			if (isString(resolvedValue)) return resolvedValue;
			else return "";
		}
		else if (!REG_FN_CALC.test(value)) return value;
		value = value.toLowerCase().trim();
	} else throw new TypeError(`${value} is not a string.`);
	const cacheKey = createCacheKey({
		namespace: NAMESPACE,
		name: "cssCalc",
		value
	}, opt);
	const cachedResult = getCache(cacheKey);
	if (cachedResult instanceof CacheItem) return cachedResult.item;
	let resolvedValue = calc(parseTokens(tokenize({ css: value }), opt).join(""), { toCanonicalUnits: true });
	if (REG_FN_VAR_START.test(value)) {
		if (REG_TYPE_DIM_PCT.test(resolvedValue)) {
			const [, val, unit] = resolvedValue.match(REG_TYPE_DIM_PCT);
			resolvedValue = `${roundToPrecision(Number(val), HEX)}${unit}`;
		}
		if (resolvedValue && !REG_FN_VAR_START.test(resolvedValue) && format === "specifiedValue") resolvedValue = `calc(${resolvedValue})`;
	}
	if (format === "specifiedValue") {
		if (/\s[-+*/]\s/.test(resolvedValue) && !resolvedValue.includes("NaN")) resolvedValue = serializeCalc(resolvedValue, opt);
		else if (REG_FN_CALC_NUM.test(resolvedValue)) {
			const [, val] = resolvedValue.match(REG_FN_CALC_NUM);
			resolvedValue = `calc(${roundToPrecision(Number(val), HEX)})`;
		}
	}
	setCache(cacheKey, resolvedValue);
	return resolvedValue;
};
//#endregion
export { cssCalc, resolveDimension, serializeCalc };

//# sourceMappingURL=css-calc.js.map