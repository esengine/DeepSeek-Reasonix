const require_isNil = require("../../predicate/isNil.js");
const require_flattenDepth = require("./flattenDepth.js");
const require_map = require("./map.js");
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
	if (require_isNil.isNil(collection)) return [];
	const mapped = require_isNil.isNil(iteratee) ? require_map.map(collection) : require_map.map(collection, iteratee);
	return require_flattenDepth.flattenDepth(mapped, 1);
}
//#endregion
exports.flatMap = flatMap;
