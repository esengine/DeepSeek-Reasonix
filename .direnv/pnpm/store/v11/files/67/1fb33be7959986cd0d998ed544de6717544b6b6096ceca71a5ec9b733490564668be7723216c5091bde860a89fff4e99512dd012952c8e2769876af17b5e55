import { flatMapDepth } from "./flatMapDepth.mjs";
//#region src/compat/array/flatMapDeep.ts
/**
* Creates a flattened array of values by running each element through iteratee and recursively flattening the mapped results.
*
* @template T, R
* @param collection - The array or object to iterate over.
* @param [iteratee] - The function that produces the new array elements.
* @returns A new array that has been deeply flattened.
*
* @example
* flatMapDeep([1, 2, 3], n => [[n, n]]);
* // => [1, 1, 2, 2, 3, 3]
*/
function flatMapDeep(collection, iteratee) {
	return flatMapDepth(collection, iteratee, Infinity);
}
//#endregion
export { flatMapDeep };
