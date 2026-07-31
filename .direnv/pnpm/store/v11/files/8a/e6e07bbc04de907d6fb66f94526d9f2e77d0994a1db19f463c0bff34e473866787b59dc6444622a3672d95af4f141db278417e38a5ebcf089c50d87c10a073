const require_without = require("../../array/without.js");
const require_isArrayLikeObject = require("../predicate/isArrayLikeObject.js");
//#region src/compat/array/without.ts
/**
* Creates an array that excludes all specified values.
*
* It correctly excludes `NaN`, as it compares values using [SameValueZero](https://tc39.es/ecma262/multipage/abstract-operations.html#sec-samevaluezero).
*
* @template T The type of elements in the array.
* @param array - The array to filter.
* @param values - The values to exclude.
* @returns A new array without the specified values.
*
* @example
* // Removes the specified values from the array
* without([1, 2, 3, 4, 5], 2, 4);
* // Returns: [1, 3, 5]
*
* @example
* // Removes specified string values from the array
* without(['a', 'b', 'c', 'a'], 'a');
* // Returns: ['b', 'c']
*/
function without(array, ...values) {
	if (!require_isArrayLikeObject.isArrayLikeObject(array)) return [];
	return require_without.without(Array.from(array), ...values);
}
//#endregion
exports.without = without;
