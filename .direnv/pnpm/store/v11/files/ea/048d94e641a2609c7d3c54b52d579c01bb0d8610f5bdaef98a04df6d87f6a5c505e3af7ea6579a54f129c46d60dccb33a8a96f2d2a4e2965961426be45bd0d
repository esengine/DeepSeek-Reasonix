const require_windowed = require("../../array/windowed.js");
const require_identity = require("../../function/identity.js");
const require_iteratee = require("../util/iteratee.js");
const require_isArrayLikeObject = require("../predicate/isArrayLikeObject.js");
const require_last = require("./last.js");
const require_differenceBy = require("./differenceBy.js");
const require_intersectionBy = require("./intersectionBy.js");
const require_unionBy = require("./unionBy.js");
//#region src/compat/array/xorBy.ts
/**
* Computes the symmetric difference between two arrays using a custom mapping function.
* The symmetric difference is the set of elements which are in either of the arrays,
* but not in their intersection, determined by the result of the mapping function.
*
* @template T - Type of elements in the input arrays.
* @template U - Type of the values returned by the mapping function.
*
* @param values - The arrays to inspect, or the function to map array elements to comparison values.
* @returns An array containing the elements that are present in either `arr1` or `arr2` but not in both, based on the values returned by the mapping function.
*
* @example
* // Custom mapping function for objects with an 'id' property
* const idMapper = obj => obj.id;
* xorBy([{ id: 1 }, { id: 2 }], [{ id: 2 }, { id: 3 }], idMapper);
* // Returns [{ id: 1 }, { id: 3 }]
*/
function xorBy(...values) {
	const lastValue = require_last.last(values);
	let mapper = require_identity.identity;
	if (!require_isArrayLikeObject.isArrayLikeObject(lastValue) && lastValue != null) {
		mapper = require_iteratee.iteratee(lastValue);
		values = values.slice(0, -1);
	}
	const arrays = values.filter(require_isArrayLikeObject.isArrayLikeObject);
	const union = require_unionBy.unionBy(...arrays, mapper);
	const intersections = require_windowed.windowed(arrays, 2).map(([arr1, arr2]) => require_intersectionBy.intersectionBy(arr1, arr2, mapper));
	return require_differenceBy.differenceBy(union, require_unionBy.unionBy(...intersections, mapper), mapper);
}
//#endregion
exports.xorBy = xorBy;
