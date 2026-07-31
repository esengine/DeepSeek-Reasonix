const require_noop = require("../function/noop.js");
const require_isEqualWith = require("./isEqualWith.js");
//#region src/predicate/isEqual.ts
/**
* Checks if two values are equal, including support for `Date`, `RegExp`, and deep object comparison.
*
* @param a - The first value to compare.
* @param b - The second value to compare.
* @returns `true` if the values are equal, otherwise `false`.
*
* @example
* isEqual(1, 1); // true
* isEqual({ a: 1 }, { a: 1 }); // true
* isEqual(/abc/g, /abc/g); // true
* isEqual(new Date('2020-01-01'), new Date('2020-01-01')); // true
* isEqual([1, 2, 3], [1, 2, 3]); // true
*/
function isEqual(a, b) {
	return require_isEqualWith.isEqualWith(a, b, require_noop.noop);
}
//#endregion
exports.isEqual = isEqual;
