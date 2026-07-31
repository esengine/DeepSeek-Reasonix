import { isArrayLike } from "../predicate/isArrayLike.mjs";
//#region src/compat/array/join.ts
/**
* Joins elements of an array into a string.
*
* @param array - The array to join.
* @param [separator=','] - The separator used to join the elements, default is common separator `,`.
* @returns Returns a string containing all elements of the array joined by the specified separator.
*
* @example
* const arr = ["a", "b", "c"];
* const result = join(arr, "~");
* console.log(result); // Output: "a~b~c"
*/
function join(array, separator) {
	if (!isArrayLike(array)) return "";
	return Array.from(array).join(separator);
}
//#endregion
export { join };
