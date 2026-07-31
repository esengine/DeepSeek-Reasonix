import { dropRight as dropRight$1 } from "../../array/dropRight.mjs";
import { toArray } from "../_internal/toArray.mjs";
import { isArrayLike } from "../predicate/isArrayLike.mjs";
import { toInteger } from "../util/toInteger.mjs";
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
	if (!isArrayLike(collection)) return [];
	itemsCount = guard ? 1 : toInteger(itemsCount);
	return dropRight$1(toArray(collection), itemsCount);
}
//#endregion
export { dropRight };
