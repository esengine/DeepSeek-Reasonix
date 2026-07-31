import { isPrimitive } from "../../predicate/isPrimitive.mjs";
import { getTag } from "../_internal/getTag.mjs";
import { argumentsTag, arrayBufferTag, arrayTag, booleanTag, dataViewTag, dateTag, float32ArrayTag, float64ArrayTag, int16ArrayTag, int32ArrayTag, int8ArrayTag, mapTag, numberTag, objectTag, regexpTag, setTag, stringTag, symbolTag, uint16ArrayTag, uint32ArrayTag, uint8ArrayTag, uint8ClampedArrayTag } from "../_internal/tags.mjs";
import { isArray } from "../predicate/isArray.mjs";
import { isTypedArray } from "../predicate/isTypedArray.mjs";
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
	if (isPrimitive(obj)) return obj;
	const tag = getTag(obj);
	if (!isCloneableObject(obj)) return {};
	if (isArray(obj)) {
		const result = Array.from(obj);
		if (obj.length > 0 && typeof obj[0] === "string" && Object.hasOwn(obj, "index")) {
			result.index = obj.index;
			result.input = obj.input;
		}
		return result;
	}
	if (isTypedArray(obj)) {
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
	switch (getTag(object)) {
		case argumentsTag:
		case arrayTag:
		case arrayBufferTag:
		case dataViewTag:
		case booleanTag:
		case dateTag:
		case float32ArrayTag:
		case float64ArrayTag:
		case int8ArrayTag:
		case int16ArrayTag:
		case int32ArrayTag:
		case mapTag:
		case numberTag:
		case objectTag:
		case regexpTag:
		case setTag:
		case stringTag:
		case symbolTag:
		case uint8ArrayTag:
		case uint8ClampedArrayTag:
		case uint16ArrayTag:
		case uint32ArrayTag: return true;
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
export { clone };
