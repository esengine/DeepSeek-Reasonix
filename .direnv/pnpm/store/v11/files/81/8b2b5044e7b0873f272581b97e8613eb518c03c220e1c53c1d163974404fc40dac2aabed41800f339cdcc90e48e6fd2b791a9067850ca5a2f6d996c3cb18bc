import { flatten } from "./flatten.mjs";
//#region src/array/flattenDeep.ts
/**
* Flattens all depths of a nested array.
*
* @template T - The type of elements within the array.
* @param arr - The array to flatten.
* @returns A new array that has been flattened.
*
* @example
* const arr = flattenDeep([1, [2, [3]], [4, [5, 6]]]);
* // Returns: [1, 2, 3, 4, 5, 6]
*/
function flattenDeep(arr) {
	return flatten(arr, Infinity);
}
//#endregion
export { flattenDeep };
