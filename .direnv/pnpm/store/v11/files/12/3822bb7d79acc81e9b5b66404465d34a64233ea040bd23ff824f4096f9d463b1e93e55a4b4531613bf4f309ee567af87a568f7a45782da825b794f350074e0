const require_unzip = require("../../array/unzip.js");
const require_isArray = require("../predicate/isArray.js");
const require_isArrayLikeObject = require("../predicate/isArrayLikeObject.js");
//#region src/compat/array/unzip.ts
/**
* Gathers elements in the same position in an internal array
* from a grouped array of elements and returns them as a new array.
*
* @template T - The type of elements in the nested array.
* @param array - The nested array to unzip.
* @returns A new array of unzipped elements.
*
* @example
* const zipped = [['a', true, 1],['b', false, 2]];
* const result = unzip(zipped);
* // result will be [['a', 'b'], [true, false], [1, 2]]
*/
function unzip(array) {
	if (!require_isArrayLikeObject.isArrayLikeObject(array) || !array.length) return [];
	array = require_isArray.isArray(array) ? array : Array.from(array);
	array = array.filter((item) => require_isArrayLikeObject.isArrayLikeObject(item));
	return require_unzip.unzip(array);
}
//#endregion
exports.unzip = unzip;
