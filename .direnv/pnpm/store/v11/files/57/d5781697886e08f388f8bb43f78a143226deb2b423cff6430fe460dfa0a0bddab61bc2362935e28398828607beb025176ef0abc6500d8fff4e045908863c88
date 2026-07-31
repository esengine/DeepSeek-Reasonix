const require_difference = require("../../array/difference.js");
const require_differenceBy = require("../../array/differenceBy.js");
const require_iteratee = require("../util/iteratee.js");
const require_isArrayLikeObject = require("../predicate/isArrayLikeObject.js");
const require_last = require("./last.js");
const require_flattenArrayLike = require("../_internal/flattenArrayLike.js");
//#region src/compat/array/differenceBy.ts
/**
* Computes the difference between an array and multiple arrays using an iteratee function.
*
* @template T
* @param array - The primary array from which to derive the difference.
* @param values - Multiple arrays containing elements to be excluded from the primary array.
* @returns A new array containing the elements that are present in the primary array but not in the values arrays.
*/
function differenceBy(array, ..._values) {
	if (!require_isArrayLikeObject.isArrayLikeObject(array)) return [];
	const iteratee$1 = require_last.last(_values);
	const values = require_flattenArrayLike.flattenArrayLike(_values);
	if (require_isArrayLikeObject.isArrayLikeObject(iteratee$1)) return require_difference.difference(Array.from(array), values);
	return require_differenceBy.differenceBy(Array.from(array), values, require_iteratee.iteratee(iteratee$1));
}
//#endregion
exports.differenceBy = differenceBy;
