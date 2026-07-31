import { difference } from "../../array/difference.mjs";
import { differenceBy as differenceBy$1 } from "../../array/differenceBy.mjs";
import { iteratee } from "../util/iteratee.mjs";
import { isArrayLikeObject } from "../predicate/isArrayLikeObject.mjs";
import { last } from "./last.mjs";
import { flattenArrayLike } from "../_internal/flattenArrayLike.mjs";
//#region src/compat/array/differenceBy.ts
/**
* Computes the difference between an array and multiple arrays using an iteratee function.
*
* @template T
* @param array - The primary array from which to derive the difference.
* @param values - Multiple arrays containing elements to be excluded from the primary array.
* @returns A new array containing the elements that are present in the primary array but not in the values arrays.
*/
function differenceBy(array, ..._values) {
	if (!isArrayLikeObject(array)) return [];
	const iteratee$1 = last(_values);
	const values = flattenArrayLike(_values);
	if (isArrayLikeObject(iteratee$1)) return difference(Array.from(array), values);
	return differenceBy$1(Array.from(array), values, iteratee(iteratee$1));
}
//#endregion
export { differenceBy };
