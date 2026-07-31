import { flatten } from "../../array/flatten.mjs";
//#region src/compat/array/concat.ts
/**
* Concatenates multiple arrays and values into a single array.
*
* @template T The type of elements in the array.
* @param values - The values and/or arrays to concatenate.
* @returns A new array containing all the input values.
*
* @example
* // Concatenate individual values
* concat(1, 2, 3);
* // returns [1, 2, 3]
*
* @example
* // Concatenate arrays of values
* concat([1, 2], [3, 4]);
* // returns [1, 2, 3, 4]
*
* @example
* // Concatenate a mix of individual values and arrays
* concat(1, [2, 3], 4);
* // returns [1, 2, 3, 4]
*
* @example
* // Concatenate nested arrays
* concat([1, [2, 3]], 4);
* // returns [1, [2, 3], 4]
*/
function concat(...values) {
	return flatten(values);
}
//#endregion
export { concat };
