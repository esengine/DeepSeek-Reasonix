import { isNil } from "../../predicate/isNil.mjs";
import { flattenDepth } from "./flattenDepth.mjs";
import { map } from "./map.mjs";
//#region src/compat/array/flatMap.ts
/**
* Creates a flattened array of values by running each element in collection through iteratee and flattening the mapped results.
*
* @template R
* @param collection - The collection to iterate over.
* @param [iteratee] - The function invoked per iteration.
* @returns Returns the new flattened array.
*
* @example
* flatMap([1, 2], n => [n, n * 2]);
* // => [1, 2, 2, 4]
*/
function flatMap(collection, iteratee) {
	if (isNil(collection)) return [];
	return flattenDepth(isNil(iteratee) ? map(collection) : map(collection, iteratee), 1);
}
//#endregion
export { flatMap };
