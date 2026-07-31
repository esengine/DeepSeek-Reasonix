const require_isLength = require("../../predicate/isLength.js");
//#region src/compat/predicate/isArrayLike.ts
/**
* Checks if `value` is array-like.
*
* @param value The value to check.
* @returns Returns `true` if `value` is array-like, else `false`.
*
* @example
* isArrayLike([1, 2, 3]); // true
* isArrayLike('abc'); // true
* isArrayLike({ 0: 'a', length: 1 }); // true
* isArrayLike({}); // false
* isArrayLike(null); // false
* isArrayLike(undefined); // false
*/
function isArrayLike(value) {
	return value != null && typeof value !== "function" && require_isLength.isLength(value.length);
}
//#endregion
exports.isArrayLike = isArrayLike;
