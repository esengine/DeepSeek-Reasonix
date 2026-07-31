const require_getSymbols = require("../compat/_internal/getSymbols.js");
const require_getTag = require("../compat/_internal/getTag.js");
const require_tags = require("../compat/_internal/tags.js");
const require_isBuffer = require("./isBuffer.js");
const require_isPlainObject = require("./isPlainObject.js");
const require_isEqualsSameValueZero = require("../_internal/isEqualsSameValueZero.js");
require("../compat/util/eq.js");
//#region src/predicate/isEqualWith.ts
/**
* Compares two values for equality using a custom comparison function.
*
* The custom function allows for fine-tuned control over the comparison process. If it returns a boolean, that result determines the equality. If it returns undefined, the function falls back to the default equality comparison.
*
* This function also uses the custom equality function to compare values inside objects,
* arrays, maps, sets, and other complex structures, ensuring a deep comparison.
*
* This approach provides flexibility in handling complex comparisons while maintaining efficient default behavior for simpler cases.
*
* The custom comparison function can take up to six parameters:
* - `x`: The value from the first object `a`.
* - `y`: The value from the second object `b`.
* - `property`: The property key used to get `x` and `y`.
* - `xParent`: The parent of the first value `x`.
* - `yParent`: The parent of the second value `y`.
* - `stack`: An internal stack (Map) to handle circular references.
*
* @param a - The first value to compare.
* @param b - The second value to compare.
* @param areValuesEqual - A function to customize the comparison.
*   If it returns a boolean, that result will be used. If it returns undefined,
*   the default equality comparison will be used.
* @returns `true` if the values are equal according to the customizer, otherwise `false`.
*
* @example
* const customizer = (a, b) => {
*   if (typeof a === 'string' && typeof b === 'string') {
*     return a.toLowerCase() === b.toLowerCase();
*   }
* };
* isEqualWith('Hello', 'hello', customizer); // true
* isEqualWith({ a: 'Hello' }, { a: 'hello' }, customizer); // true
* isEqualWith([1, 2, 3], [1, 2, 3], customizer); // true
*/
function isEqualWith(a, b, areValuesEqual) {
	return isEqualWithImpl(a, b, void 0, void 0, void 0, void 0, areValuesEqual);
}
function isEqualWithImpl(a, b, property, aParent, bParent, stack, areValuesEqual) {
	const result = areValuesEqual(a, b, property, aParent, bParent, stack);
	if (result !== void 0) return result;
	if (typeof a === typeof b) switch (typeof a) {
		case "bigint":
		case "string":
		case "boolean":
		case "symbol":
		case "undefined": return a === b;
		case "number": return a === b || Object.is(a, b);
		case "function": return a === b;
		case "object": return areObjectsEqual(a, b, stack, areValuesEqual);
	}
	return areObjectsEqual(a, b, stack, areValuesEqual);
}
function areObjectsEqual(a, b, stack, areValuesEqual) {
	if (Object.is(a, b)) return true;
	let aTag = require_getTag.getTag(a);
	let bTag = require_getTag.getTag(b);
	if (aTag === "[object Arguments]") aTag = require_tags.objectTag;
	if (bTag === "[object Arguments]") bTag = require_tags.objectTag;
	if (aTag !== bTag) return false;
	switch (aTag) {
		case require_tags.stringTag: return a.toString() === b.toString();
		case require_tags.numberTag: {
			const x = a.valueOf();
			const y = b.valueOf();
			return require_isEqualsSameValueZero.isEqualsSameValueZero(x, y);
		}
		case require_tags.booleanTag:
		case require_tags.dateTag:
		case require_tags.symbolTag: return Object.is(a.valueOf(), b.valueOf());
		case require_tags.regexpTag: return a.source === b.source && a.flags === b.flags;
		case require_tags.functionTag: return a === b;
	}
	stack = stack ?? /* @__PURE__ */ new Map();
	const aStack = stack.get(a);
	const bStack = stack.get(b);
	if (aStack != null && bStack != null) return aStack === b;
	stack.set(a, b);
	stack.set(b, a);
	try {
		switch (aTag) {
			case require_tags.mapTag:
				if (a.size !== b.size) return false;
				for (const [key, value] of a.entries()) if (!b.has(key) || !isEqualWithImpl(value, b.get(key), key, a, b, stack, areValuesEqual)) return false;
				return true;
			case require_tags.setTag: {
				if (a.size !== b.size) return false;
				const aValues = Array.from(a.values());
				const bValues = Array.from(b.values());
				for (let i = 0; i < aValues.length; i++) {
					const aValue = aValues[i];
					const index = bValues.findIndex((bValue) => {
						return isEqualWithImpl(aValue, bValue, void 0, a, b, stack, areValuesEqual);
					});
					if (index === -1) return false;
					bValues.splice(index, 1);
				}
				return true;
			}
			case require_tags.arrayTag:
			case require_tags.uint8ArrayTag:
			case require_tags.uint8ClampedArrayTag:
			case require_tags.uint16ArrayTag:
			case require_tags.uint32ArrayTag:
			case require_tags.bigUint64ArrayTag:
			case require_tags.int8ArrayTag:
			case require_tags.int16ArrayTag:
			case require_tags.int32ArrayTag:
			case require_tags.bigInt64ArrayTag:
			case require_tags.float32ArrayTag:
			case require_tags.float64ArrayTag:
				if (require_isBuffer.isBuffer(a) !== require_isBuffer.isBuffer(b)) return false;
				if (a.length !== b.length) return false;
				for (let i = 0; i < a.length; i++) if (!isEqualWithImpl(a[i], b[i], i, a, b, stack, areValuesEqual)) return false;
				return true;
			case require_tags.arrayBufferTag:
				if (a.byteLength !== b.byteLength) return false;
				return areObjectsEqual(new Uint8Array(a), new Uint8Array(b), stack, areValuesEqual);
			case require_tags.dataViewTag:
				if (a.byteLength !== b.byteLength || a.byteOffset !== b.byteOffset) return false;
				return areObjectsEqual(new Uint8Array(a), new Uint8Array(b), stack, areValuesEqual);
			case require_tags.errorTag: return a.name === b.name && a.message === b.message;
			case require_tags.objectTag: {
				if (!(areObjectsEqual(a.constructor, b.constructor, stack, areValuesEqual) || require_isPlainObject.isPlainObject(a) && require_isPlainObject.isPlainObject(b))) return false;
				const aKeys = [...Object.keys(a), ...require_getSymbols.getSymbols(a)];
				const bKeys = [...Object.keys(b), ...require_getSymbols.getSymbols(b)];
				if (aKeys.length !== bKeys.length) return false;
				for (let i = 0; i < aKeys.length; i++) {
					const propKey = aKeys[i];
					const aProp = a[propKey];
					if (!Object.hasOwn(b, propKey)) return false;
					const bProp = b[propKey];
					if (!isEqualWithImpl(aProp, bProp, propKey, a, b, stack, areValuesEqual)) return false;
				}
				return true;
			}
			default: return false;
		}
	} finally {
		stack.delete(a);
		stack.delete(b);
	}
}
//#endregion
exports.isEqualWith = isEqualWith;
