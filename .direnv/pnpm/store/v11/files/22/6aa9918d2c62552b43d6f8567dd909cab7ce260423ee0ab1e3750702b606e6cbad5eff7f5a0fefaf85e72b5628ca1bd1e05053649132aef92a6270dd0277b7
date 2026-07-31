const require_uniq = require("../../array/uniq.js");
const require_isArrayLikeObject = require("../predicate/isArrayLikeObject.js");
const require_flatMapDepth = require("./flatMapDepth.js");
//#region src/compat/array/union.ts
/**
* This function takes multiple arrays and returns a new array containing only the unique values
* from all input arrays, preserving the order of their first occurrence.
*
* @template T - The type of elements in the arrays.
* @param arrays - The arrays to inspect.
* @returns Returns the new array of combined unique values.
*
* @example
* // Returns [2, 1]
* union([2], [1, 2]);
*
* @example
* // Returns [2, 1, 3]
* union([2], [1, 2], [2, 3]);
*
* @example
* // Returns [1, 3, 2, [5], [4]] (does not deeply flatten nested arrays)
* union([1, 3, 2], [1, [5]], [2, [4]]);
*
* @example
* // Returns [0, 2, 1] (ignores non-array values like 3 and { '0': 1 })
* union([0], 3, { '0': 1 }, null, [2, 1]);
* @example
* // Returns [0, 'a', 2, 1] (treats array-like object { 0: 'a', length: 1 } as a valid array)
* union([0], { 0: 'a', length: 1 }, [2, 1]);
*/
function union(...arrays) {
	const validArrays = arrays.filter(require_isArrayLikeObject.isArrayLikeObject);
	const flattened = require_flatMapDepth.flatMapDepth(validArrays, (v) => Array.from(v), 1);
	return require_uniq.uniq(flattened);
}
//#endregion
exports.union = union;
