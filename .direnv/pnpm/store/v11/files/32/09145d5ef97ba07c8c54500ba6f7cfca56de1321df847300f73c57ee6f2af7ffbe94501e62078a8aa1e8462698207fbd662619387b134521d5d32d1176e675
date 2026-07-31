const require_isBuffer = require("../../predicate/isBuffer.js");
const require_isArrayLike = require("./isArrayLike.js");
const require_isArguments = require("./isArguments.js");
const require_isPrototype = require("../_internal/isPrototype.js");
const require_isTypedArray = require("./isTypedArray.js");
//#region src/compat/predicate/isEmpty.ts
/**
* Checks if a given value is empty.
*
* - If the given value is a string, checks if it is an empty string.
* - If the given value is an array, `Map`, or `Set`, checks if its size is 0.
* - If the given value is an [array-like object](../predicate/isArrayLike.md), checks if its length is 0.
* - If the given value is an object, checks if it is an empty object with no properties.
* - Primitive values (booleans, numbers, or bigints) are considered empty.
*
* @param [value] - The value to check.
* @returns `true` if the value is empty, `false` otherwise.
*
* @example
* isEmpty(); // true
* isEmpty(null); // true
* isEmpty(""); // true
* isEmpty([]); // true
* isEmpty({}); // true
* isEmpty(new Map()); // true
* isEmpty(new Set()); // true
* isEmpty("hello"); // false
* isEmpty([1, 2, 3]); // false
* isEmpty({ a: 1 }); // false
* isEmpty(new Map([["key", "value"]])); // false
* isEmpty(new Set([1, 2, 3])); // false
*/
function isEmpty(value) {
	if (value == null) return true;
	if (require_isArrayLike.isArrayLike(value)) {
		if (typeof value.splice !== "function" && typeof value !== "string" && !require_isBuffer.isBuffer(value) && !require_isTypedArray.isTypedArray(value) && !require_isArguments.isArguments(value)) return false;
		return value.length === 0;
	}
	if (typeof value === "object" || typeof value === "function") {
		if (value instanceof Map || value instanceof Set) return value.size === 0;
		const keys = Object.keys(value);
		if (require_isPrototype.isPrototype(value)) return keys.filter((x) => x !== "constructor").length === 0;
		return keys.length === 0;
	}
	return true;
}
//#endregion
exports.isEmpty = isEmpty;
