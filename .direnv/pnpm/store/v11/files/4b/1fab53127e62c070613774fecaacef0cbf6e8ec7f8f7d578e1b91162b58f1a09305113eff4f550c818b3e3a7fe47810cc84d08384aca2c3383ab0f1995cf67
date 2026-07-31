import { windowed } from "../../array/windowed.mjs";
import { identity } from "../../function/identity.mjs";
import { iteratee } from "../util/iteratee.mjs";
import { isArrayLikeObject } from "../predicate/isArrayLikeObject.mjs";
import { last } from "./last.mjs";
import { differenceBy } from "./differenceBy.mjs";
import { intersectionBy } from "./intersectionBy.mjs";
import { unionBy } from "./unionBy.mjs";
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
	const lastValue = last(values);
	let mapper = identity;
	if (!isArrayLikeObject(lastValue) && lastValue != null) {
		mapper = iteratee(lastValue);
		values = values.slice(0, -1);
	}
	const arrays = values.filter(isArrayLikeObject);
	return differenceBy(unionBy(...arrays, mapper), unionBy(...windowed(arrays, 2).map(([arr1, arr2]) => intersectionBy(arr1, arr2, mapper)), mapper), mapper);
}
//#endregion
export { xorBy };
