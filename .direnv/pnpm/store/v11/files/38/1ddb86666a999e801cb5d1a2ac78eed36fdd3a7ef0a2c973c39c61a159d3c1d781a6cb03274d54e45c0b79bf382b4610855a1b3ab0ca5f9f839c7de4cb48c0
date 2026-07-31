import { isArray } from "../predicate/isArray.mjs";
import { toKey } from "../_internal/toKey.mjs";
import { toPath } from "../util/toPath.mjs";
import { isIndex } from "../_internal/isIndex.mjs";
import { flattenDepth } from "./flattenDepth.mjs";
import { isKey } from "../_internal/isKey.mjs";
import { at } from "../object/at.mjs";
import { unset } from "../object/unset.mjs";
//#region src/compat/array/pullAt.ts
/**
* Removes elements from an array at specified indices and returns the removed elements.
*
* @template T
* @param array - The array from which elements will be removed.
* @param _indices - An array of indices specifying the positions of elements to remove.
* @returns An array containing the elements that were removed from the original array.
*
* @example
* const numbers = [10, 20, 30, 40, 50];
* const removed = pullAt(numbers, [1, 3, 4]);
* console.log(removed); // [20, 40, 50]
* console.log(numbers); // [10, 30]
*/
function pullAt(array, ..._indices) {
	const indices = flattenDepth(_indices, 1);
	if (!array) return Array(indices.length);
	const result = at(array, indices);
	const indicesToPull = indices.map((index) => isIndex(index, array.length) ? Number(index) : index).sort((a, b) => b - a);
	for (const index of new Set(indicesToPull)) {
		if (isIndex(index, array.length)) {
			Array.prototype.splice.call(array, index, 1);
			continue;
		}
		if (isKey(index, array)) {
			delete array[toKey(index)];
			continue;
		}
		unset(array, isArray(index) ? index : toPath(index));
	}
	return result;
}
//#endregion
export { pullAt };
