const require_difference = require("../../array/difference.js");
const require_differenceWith = require("../../array/differenceWith.js");
const require_isArrayLikeObject = require("../predicate/isArrayLikeObject.js");
const require_last = require("./last.js");
const require_flattenArrayLike = require("../_internal/flattenArrayLike.js");
//#region src/compat/array/differenceWith.ts
/**
* Computes the difference between the primary array and one or more arrays using an optional comparator function.
*
* @template T
* @param array - The primary array to compare elements against.
* @param values - One or more arrays to compare with the primary array, and an optional comparator function to determine if two elements are considered equal.
* @returns A new array containing the elements from the primary array that do not match any elements in the provided arrays or those compared using the comparator function.
*
* @example
* // Example with a comparator function
* const array = [{ id: 1 }, { id: 2 }, { id: 3 }];
* const values1 = [{ id: 2 }];
* const values2 = [{ id: 3 }];
* const comparator = (a, b) => a.id === b.id;
*
* const result = differenceWith(array, values1, values2, comparator);
* // result will be [{ id: 1 }]
*
* @example
* // Example without a comparator function
* const array = [1, 2, 3];
* const values1 = [2];
* const values2 = [3];
*
* const result = differenceWith(array, values1, values2);
* // result will be [1]
*/
function differenceWith(array, ...values) {
	if (!require_isArrayLikeObject.isArrayLikeObject(array)) return [];
	const comparator = require_last.last(values);
	const flattenedValues = require_flattenArrayLike.flattenArrayLike(values);
	if (typeof comparator === "function") return require_differenceWith.differenceWith(Array.from(array), flattenedValues, comparator);
	return require_difference.difference(Array.from(array), flattenedValues);
}
//#endregion
exports.differenceWith = differenceWith;
