import { fill as fill$1 } from "../../array/fill.mjs";
import { isArrayLike } from "../predicate/isArrayLike.mjs";
import { isString } from "../predicate/isString.mjs";
//#region src/compat/array/fill.ts
/**
* Fills elements of an array with a specified value from the start position up to, but not including, the end position.
*
* This function mutates the original array and replaces its elements with the provided value, starting from the specified
* start index up to the end index (non-inclusive). If the start or end indices are not provided, it defaults to filling the
* entire array.
*
* @template T, U
* @param array - The array to fill.
* @param value - The value to fill the array with.
* @param [start=0] - The start position. Defaults to 0.
* @param [end=arr.length] - The end position. Defaults to the array's length.
* @returns The array with the filled values.
*
* @example
* fill([1, 2, 3], 'a');
* // => ['a', 'a', 'a']
*
* fill(Array(3), 2);
* // => [2, 2, 2]
*
* fill([4, 6, 8, 10], '*', 1, 3);
* // => [4, '*', '*', 10]
*
* fill([1, 2, 3], '*', -2, -1);
* // => [1, '*', 3]
*/
function fill(array, value, start = 0, end = array ? array.length : 0) {
	if (!isArrayLike(array)) return [];
	if (isString(array)) return array;
	start = Math.floor(start);
	end = Math.floor(end);
	if (!start) start = 0;
	if (!end) end = 0;
	return fill$1(array, value, start, end);
}
//#endregion
export { fill };
