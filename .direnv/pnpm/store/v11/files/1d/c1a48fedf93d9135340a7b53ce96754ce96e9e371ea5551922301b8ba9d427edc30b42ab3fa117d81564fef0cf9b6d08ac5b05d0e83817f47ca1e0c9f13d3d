const require_isArray = require("../predicate/isArray.js");
const require_toKey = require("../_internal/toKey.js");
const require_toPath = require("../util/toPath.js");
const require_isIndex = require("../_internal/isIndex.js");
const require_flattenDepth = require("./flattenDepth.js");
const require_isKey = require("../_internal/isKey.js");
const require_at = require("../object/at.js");
const require_unset = require("../object/unset.js");
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
	const indices = require_flattenDepth.flattenDepth(_indices, 1);
	if (!array) return Array(indices.length);
	const result = require_at.at(array, indices);
	const indicesToPull = indices.map((index) => require_isIndex.isIndex(index, array.length) ? Number(index) : index).sort((a, b) => b - a);
	for (const index of new Set(indicesToPull)) {
		if (require_isIndex.isIndex(index, array.length)) {
			Array.prototype.splice.call(array, index, 1);
			continue;
		}
		if (require_isKey.isKey(index, array)) {
			delete array[require_toKey.toKey(index)];
			continue;
		}
		const path = require_isArray.isArray(index) ? index : require_toPath.toPath(index);
		require_unset.unset(array, path);
	}
	return result;
}
//#endregion
exports.pullAt = pullAt;
