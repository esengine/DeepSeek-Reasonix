const require_isPrimitive = require("../../predicate/isPrimitive.js");
const require_getTag = require("../_internal/getTag.js");
const require_tags = require("../_internal/tags.js");
const require_isArray = require("../predicate/isArray.js");
const require_isTypedArray = require("../predicate/isTypedArray.js");
//#region src/compat/object/clone.ts
/**
* Creates a shallow clone of the given object.
*
* @template T - The type of the object.
* @param obj - The object to clone.
* @returns A shallow clone of the given object.
*
* @example
* // Clone a primitive object
* const num = 29;
* const clonedNum = clone(num);
* console.log(clonedNum); // 29
* console.log(clonedNum === num); // true
*
* @example
* // Clone an array
* const arr = [1, 2, 3];
* const clonedArr = clone(arr);
* console.log(clonedArr); // [1, 2, 3]
* console.log(clonedArr === arr); // false
*
* @example
* // Clone an object
* const obj = { a: 1, b: 'es-toolkit', c: [1, 2, 3] };
* const clonedObj = clone(obj);
* console.log(clonedObj); // { a: 1, b: 'es-toolkit', c: [1, 2, 3] }
* console.log(clonedObj === obj); // false
*/
function clone(obj) {
	if (require_isPrimitive.isPrimitive(obj)) return obj;
	const tag = require_getTag.getTag(obj);
	if (!isCloneableObject(obj)) return {};
	if (require_isArray.isArray(obj)) {
		const result = Array.from(obj);
		if (obj.length > 0 && typeof obj[0] === "string" && Object.hasOwn(obj, "index")) {
			result.index = obj.index;
			result.input = obj.input;
		}
		return result;
	}
	if (require_isTypedArray.isTypedArray(obj)) {
		const typedArray = obj;
		const Ctor = typedArray.constructor;
		return new Ctor(typedArray.buffer, typedArray.byteOffset, typedArray.length);
	}
	if (tag === "[object ArrayBuffer]") return new ArrayBuffer(obj.byteLength);
	if (tag === "[object DataView]") {
		const dataView = obj;
		const buffer = dataView.buffer;
		const byteOffset = dataView.byteOffset;
		const byteLength = dataView.byteLength;
		const clonedBuffer = new ArrayBuffer(byteLength);
		const srcView = new Uint8Array(buffer, byteOffset, byteLength);
		new Uint8Array(clonedBuffer).set(srcView);
		return new DataView(clonedBuffer);
	}
	if (tag === "[object Boolean]" || tag === "[object Number]" || tag === "[object String]") {
		const Ctor = obj.constructor;
		const clone = new Ctor(obj.valueOf());
		if (tag === "[object String]") cloneStringObjectProperties(clone, obj);
		else copyOwnProperties(clone, obj);
		return clone;
	}
	if (tag === "[object Date]") return new Date(Number(obj));
	if (tag === "[object RegExp]") {
		const regExp = obj;
		const clone = new RegExp(regExp.source, regExp.flags);
		clone.lastIndex = regExp.lastIndex;
		return clone;
	}
	if (tag === "[object Symbol]") return Object(Symbol.prototype.valueOf.call(obj));
	if (tag === "[object Map]") {
		const map = obj;
		const result = /* @__PURE__ */ new Map();
		map.forEach((obj, key) => {
			result.set(key, obj);
		});
		return result;
	}
	if (tag === "[object Set]") {
		const set = obj;
		const result = /* @__PURE__ */ new Set();
		set.forEach((obj) => {
			result.add(obj);
		});
		return result;
	}
	if (tag === "[object Arguments]") {
		const args = obj;
		const result = {};
		copyOwnProperties(result, args);
		result.length = args.length;
		result[Symbol.iterator] = args[Symbol.iterator];
		return result;
	}
	const result = {};
	copyPrototype(result, obj);
	copyOwnProperties(result, obj);
	copySymbolProperties(result, obj);
	return result;
}
function isCloneableObject(object) {
	switch (require_getTag.getTag(object)) {
		case require_tags.argumentsTag:
		case require_tags.arrayTag:
		case require_tags.arrayBufferTag:
		case require_tags.dataViewTag:
		case require_tags.booleanTag:
		case require_tags.dateTag:
		case require_tags.float32ArrayTag:
		case require_tags.float64ArrayTag:
		case require_tags.int8ArrayTag:
		case require_tags.int16ArrayTag:
		case require_tags.int32ArrayTag:
		case require_tags.mapTag:
		case require_tags.numberTag:
		case require_tags.objectTag:
		case require_tags.regexpTag:
		case require_tags.setTag:
		case require_tags.stringTag:
		case require_tags.symbolTag:
		case require_tags.uint8ArrayTag:
		case require_tags.uint8ClampedArrayTag:
		case require_tags.uint16ArrayTag:
		case require_tags.uint32ArrayTag: return true;
		default: return false;
	}
}
function copyOwnProperties(target, source) {
	for (const key in source) if (Object.hasOwn(source, key)) target[key] = source[key];
}
function copySymbolProperties(target, source) {
	const symbols = Object.getOwnPropertySymbols(source);
	for (let i = 0; i < symbols.length; i++) {
		const symbol = symbols[i];
		if (Object.prototype.propertyIsEnumerable.call(source, symbol)) target[symbol] = source[symbol];
	}
}
function cloneStringObjectProperties(target, source) {
	const stringLength = source.valueOf().length;
	for (const key in source) if (Object.hasOwn(source, key) && (Number.isNaN(Number(key)) || Number(key) >= stringLength)) target[key] = source[key];
}
function copyPrototype(target, source) {
	const proto = Object.getPrototypeOf(source);
	if (proto !== null) {
		if (typeof source.constructor === "function") Object.setPrototypeOf(target, proto);
	}
}
//#endregion
exports.clone = clone;
