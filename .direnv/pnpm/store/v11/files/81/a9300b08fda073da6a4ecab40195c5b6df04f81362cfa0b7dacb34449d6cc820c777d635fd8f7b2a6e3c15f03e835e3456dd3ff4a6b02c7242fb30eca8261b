const require_dropRight = require("../../array/dropRight.js");
const require_toArray = require("../_internal/toArray.js");
const require_isArrayLike = require("../predicate/isArrayLike.js");
const require_toInteger = require("../util/toInteger.js");
//#region src/compat/array/dropRight.ts
/**
* Removes a specified number of elements from the end of an array and returns the rest.
*
* This function takes an array and a number, and returns a new array with the specified number
* of elements removed from the end.
*
* @template T - The type of elements in the array.
* @param collection - The array from which to drop elements.
* @param itemsCount - The number of elements to drop from the end of the array.
* @param [guard] - Enables use as an iteratee for methods like `_.map`.
* @returns A new array with the specified number of elements removed from the end.
*
* @example
* const array = [1, 2, 3, 4, 5];
* const result = dropRight(array, 2);
* // result will be [1, 2, 3] since the last two elements are dropped.
*/
function dropRight(collection, itemsCount = 1, guard) {
	if (!require_isArrayLike.isArrayLike(collection)) return [];
	itemsCount = guard ? 1 : require_toInteger.toInteger(itemsCount);
	return require_dropRight.dropRight(require_toArray.toArray(collection), itemsCount);
}
//#endregion
exports.dropRight = dropRight;
