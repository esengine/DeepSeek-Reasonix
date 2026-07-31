const require_last = require("../../array/last.js");
const require_uniq = require("../../array/uniq.js");
const require_uniqBy = require("../../array/uniqBy.js");
const require_ary = require("../../function/ary.js");
const require_iteratee = require("../util/iteratee.js");
const require_isArrayLikeObject = require("../predicate/isArrayLikeObject.js");
const require_flattenArrayLike = require("../_internal/flattenArrayLike.js");
//#region src/compat/array/unionBy.ts
/**
* This function takes multiple arrays and returns a new array containing only the unique values
* from all input arrays, preserving the order of their first occurrence.
* An iteratee function can be provided for comparison and it output values from the first possible array
*
* @template T - The type of elements in the arrays.
* @param values - The arrays to inspect, or the iteratee function.
* @returns Returns the new array of combined unique values.
*
* @example
* // Returns [2.1, 1.2]
* unionBy([2.1], [1.2, 2.3], Math.floor);
*
* @example
* // Returns [{ x: 1 }, { x: 2 }]
* unionBy([{ x: 1 }], [{ x: 2 }, { x: 1 }], 'x');
*
* @example
* // Returns [{ x: 1, y: 1 }]
* unionBy([{ x: 1, y: 1 }], [{ x: 1, y: 2 }], 'x');
*/
function unionBy(...values) {
	const lastValue = require_last.last(values);
	const flattened = require_flattenArrayLike.flattenArrayLike(values);
	if (require_isArrayLikeObject.isArrayLikeObject(lastValue) || lastValue == null) return require_uniq.uniq(flattened);
	return require_uniqBy.uniqBy(flattened, require_ary.ary(require_iteratee.iteratee(lastValue), 1));
}
//#endregion
exports.unionBy = unionBy;
