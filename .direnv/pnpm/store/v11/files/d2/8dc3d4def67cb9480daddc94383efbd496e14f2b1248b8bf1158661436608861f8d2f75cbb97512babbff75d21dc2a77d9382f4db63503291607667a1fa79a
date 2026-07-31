const require_windowed = require("../../array/windowed.js");
const require_isArrayLikeObject = require("../predicate/isArrayLikeObject.js");
const require_last = require("./last.js");
const require_differenceWith = require("./differenceWith.js");
const require_intersectionWith = require("./intersectionWith.js");
const require_unionWith = require("./unionWith.js");
//#region src/compat/array/xorWith.ts
/**
* Creates an array of unique values that is the symmetric difference of the given arrays using a custom comparator function.
*
* The symmetric difference is the set of elements which are in either of the arrays,
* but not in their intersection, determined by the comparator function.
*
* @template T - Type of elements in the input arrays.
*
* @param values - The arrays to inspect, or the comparator function.
* @returns An array containing the elements that are present in exactly one of the arrays
*  as determined by the comparator.
*
* @example
* // Custom comparator function for objects with an 'id' property
* const idComparator = (a, b) => a.id === b.id;
* xorWith([{ id: 1 }, { id: 2 }], [{ id: 2 }, { id: 3 }], idComparator);
* // Returns [{ id: 1 }, { id: 3 }]
*/
function xorWith(...values) {
	const lastValue = require_last.last(values);
	let comparator = (a, b) => a === b;
	if (typeof lastValue === "function") {
		comparator = lastValue;
		values = values.slice(0, -1);
	}
	const arrays = values.filter(require_isArrayLikeObject.isArrayLikeObject);
	const union = require_unionWith.unionWith(...arrays, comparator);
	const intersections = require_windowed.windowed(arrays, 2).map(([arr1, arr2]) => require_intersectionWith.intersectionWith(arr1, arr2, comparator));
	return require_differenceWith.differenceWith(union, require_unionWith.unionWith(...intersections, comparator), comparator);
}
//#endregion
exports.xorWith = xorWith;
