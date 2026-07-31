const require_isPrimitive = require("../predicate/isPrimitive.js");
const require_isTypedArray = require("../predicate/isTypedArray.js");
const require_getSymbols = require("../compat/_internal/getSymbols.js");
const require_getTag = require("../compat/_internal/getTag.js");
const require_tags = require("../compat/_internal/tags.js");
const require_isBuffer = require("../predicate/isBuffer.js");
//#region src/object/cloneDeepWith.ts
/**
* Deeply clones the given object.
*
* You can customize the deep cloning process using the `cloneValue` function.
* The function takes the current value `value`, the property name `key`, and the entire object `obj` as arguments.
* If the function returns a value, that value is used;
* if it returns `undefined`, the default cloning method is used.
*
* @template T - The type of the object.
* @param obj - The object to clone.
* @param [cloneValue] - A function to customize the cloning process.
* @returns A deep clone of the given object.
*
* @example
* // Clone a primitive value
* const num = 29;
* const clonedNum = cloneDeepWith(num);
* console.log(clonedNum); // 29
* console.log(clonedNum === num); // true
*
* @example
* // Clone an object with a customizer
* const obj = { a: 1, b: 2 };
* const clonedObj = cloneDeepWith(obj, (value) => {
*   if (typeof value === 'number') {
*     return value * 2; // Double the number
*   }
* });
* console.log(clonedObj); // { a: 2, b: 4 }
* console.log(clonedObj === obj); // false
*
* @example
* // Clone an array with a customizer
* const arr = [1, 2, 3];
* const clonedArr = cloneDeepWith(arr, (value) => {
*   return value + 1; // Increment each value
* });
* console.log(clonedArr); // [2, 3, 4]
* console.log(clonedArr === arr); // false
*/
function cloneDeepWith(obj, cloneValue) {
	return cloneDeepWithImpl(obj, void 0, obj, /* @__PURE__ */ new Map(), cloneValue);
}
function cloneDeepWithImpl(valueToClone, keyToClone, objectToClone, stack = /* @__PURE__ */ new Map(), cloneValue = void 0) {
	const cloned = cloneValue?.(valueToClone, keyToClone, objectToClone, stack);
	if (cloned !== void 0) return cloned;
	if (require_isPrimitive.isPrimitive(valueToClone)) return valueToClone;
	if (stack.has(valueToClone)) return stack.get(valueToClone);
	if (Array.isArray(valueToClone)) {
		const result = new Array(valueToClone.length);
		stack.set(valueToClone, result);
		for (let i = 0; i < valueToClone.length; i++) result[i] = cloneDeepWithImpl(valueToClone[i], i, objectToClone, stack, cloneValue);
		if (Object.hasOwn(valueToClone, "index")) result.index = valueToClone.index;
		if (Object.hasOwn(valueToClone, "input")) result.input = valueToClone.input;
		return result;
	}
	if (valueToClone instanceof Date) return new Date(valueToClone.getTime());
	if (valueToClone instanceof RegExp) {
		const result = new RegExp(valueToClone.source, valueToClone.flags);
		result.lastIndex = valueToClone.lastIndex;
		return result;
	}
	if (valueToClone instanceof Map) {
		const result = /* @__PURE__ */ new Map();
		stack.set(valueToClone, result);
		for (const [key, value] of valueToClone) result.set(key, cloneDeepWithImpl(value, key, objectToClone, stack, cloneValue));
		return result;
	}
	if (valueToClone instanceof Set) {
		const result = /* @__PURE__ */ new Set();
		stack.set(valueToClone, result);
		for (const value of valueToClone) result.add(cloneDeepWithImpl(value, void 0, objectToClone, stack, cloneValue));
		return result;
	}
	if (require_isBuffer.isBuffer(valueToClone)) return valueToClone.subarray();
	if (require_isTypedArray.isTypedArray(valueToClone)) {
		const result = new (Object.getPrototypeOf(valueToClone)).constructor(valueToClone.length);
		stack.set(valueToClone, result);
		for (let i = 0; i < valueToClone.length; i++) result[i] = cloneDeepWithImpl(valueToClone[i], i, objectToClone, stack, cloneValue);
		return result;
	}
	if (valueToClone instanceof ArrayBuffer || typeof SharedArrayBuffer !== "undefined" && valueToClone instanceof SharedArrayBuffer) return valueToClone.slice(0);
	if (valueToClone instanceof DataView) {
		const result = new DataView(valueToClone.buffer.slice(0), valueToClone.byteOffset, valueToClone.byteLength);
		stack.set(valueToClone, result);
		copyProperties(result, valueToClone, objectToClone, stack, cloneValue);
		return result;
	}
	if (typeof File !== "undefined" && valueToClone instanceof File) {
		const result = new File([valueToClone], valueToClone.name, { type: valueToClone.type });
		stack.set(valueToClone, result);
		copyProperties(result, valueToClone, objectToClone, stack, cloneValue);
		return result;
	}
	if (typeof Blob !== "undefined" && valueToClone instanceof Blob) {
		const result = new Blob([valueToClone], { type: valueToClone.type });
		stack.set(valueToClone, result);
		copyProperties(result, valueToClone, objectToClone, stack, cloneValue);
		return result;
	}
	if (valueToClone instanceof Error) {
		const result = structuredClone(valueToClone);
		stack.set(valueToClone, result);
		result.message = valueToClone.message;
		result.name = valueToClone.name;
		result.stack = valueToClone.stack;
		result.cause = valueToClone.cause;
		result.constructor = valueToClone.constructor;
		copyProperties(result, valueToClone, objectToClone, stack, cloneValue);
		return result;
	}
	if (valueToClone instanceof Boolean) {
		const result = new Boolean(valueToClone.valueOf());
		stack.set(valueToClone, result);
		copyProperties(result, valueToClone, objectToClone, stack, cloneValue);
		return result;
	}
	if (valueToClone instanceof Number) {
		const result = new Number(valueToClone.valueOf());
		stack.set(valueToClone, result);
		copyProperties(result, valueToClone, objectToClone, stack, cloneValue);
		return result;
	}
	if (valueToClone instanceof String) {
		const result = new String(valueToClone.valueOf());
		stack.set(valueToClone, result);
		copyProperties(result, valueToClone, objectToClone, stack, cloneValue);
		return result;
	}
	if (typeof valueToClone === "object" && isCloneableObject(valueToClone)) {
		const result = Object.create(Object.getPrototypeOf(valueToClone));
		stack.set(valueToClone, result);
		copyProperties(result, valueToClone, objectToClone, stack, cloneValue);
		return result;
	}
	return valueToClone;
}
function copyProperties(target, source, objectToClone = target, stack, cloneValue) {
	const keys = [...Object.keys(source), ...require_getSymbols.getSymbols(source)];
	for (let i = 0; i < keys.length; i++) {
		const key = keys[i];
		const descriptor = Object.getOwnPropertyDescriptor(target, key);
		if (descriptor == null || descriptor.writable) target[key] = cloneDeepWithImpl(source[key], key, objectToClone, stack, cloneValue);
	}
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
//#endregion
exports.cloneDeepWith = cloneDeepWith;
exports.cloneDeepWithImpl = cloneDeepWithImpl;
exports.copyProperties = copyProperties;
