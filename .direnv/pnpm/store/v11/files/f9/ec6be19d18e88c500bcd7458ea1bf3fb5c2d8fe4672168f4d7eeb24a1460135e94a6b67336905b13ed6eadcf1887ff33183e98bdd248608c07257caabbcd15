const require_take = require("../../array/take.js");
const require_toArray = require("../_internal/toArray.js");
const require_isArrayLike = require("../predicate/isArrayLike.js");
const require_toInteger = require("../util/toInteger.js");
//#region src/compat/array/take.ts
/**
* Returns a new array containing the first `count` elements from the input array `arr`.
* If `count` is greater than the length of `arr`, the entire array is returned.
*
* @template T - Type of elements in the input array.
*
* @param arr - The array to take elements from.
* @param [count=1] - The number of elements to take.
* @param [guard] - Enables use as an iteratee for methods like `_.map`.
* @returns A new array containing the first `count` elements from `arr`.
*
* @example
* // Returns [1, 2, 3]
* take([1, 2, 3, 4, 5], 3);
*
* @example
* // Returns ['a', 'b']
* take(['a', 'b', 'c'], 2);
*
* @example
* // Returns [1, 2, 3]
* take([1, 2, 3], 5);
*/
function take(arr, count = 1, guard) {
	count = guard || count === void 0 ? 1 : require_toInteger.toInteger(count);
	if (count < 1 || !require_isArrayLike.isArrayLike(arr)) return [];
	return require_take.take(require_toArray.toArray(arr), count);
}
//#endregion
exports.take = take;
