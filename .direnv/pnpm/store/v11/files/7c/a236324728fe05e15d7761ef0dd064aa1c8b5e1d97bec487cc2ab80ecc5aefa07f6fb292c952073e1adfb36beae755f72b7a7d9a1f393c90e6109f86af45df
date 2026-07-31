const require_compact = require("../../array/compact.js");
const require_isArrayLike = require("../predicate/isArrayLike.js");
//#region src/compat/array/compact.ts
/**
* Removes falsey values (false, null, 0, 0n, '', undefined, NaN) from an array.
*
* @template T - The type of elements in the array.
* @param arr - The input array to remove falsey values.
* @returns A new array with all falsey values removed.
*
* @example
* compact([0, 0n, 1, false, 2, '', 3, null, undefined, 4, NaN, 5]);
* Returns: [1, 2, 3, 4, 5]
*/
function compact(arr) {
	if (!require_isArrayLike.isArrayLike(arr)) return [];
	return require_compact.compact(Array.from(arr));
}
//#endregion
exports.compact = compact;
