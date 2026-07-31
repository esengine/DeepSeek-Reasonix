import { last } from "../../array/last.mjs";
import { uniq } from "../../array/uniq.mjs";
import { uniqBy } from "../../array/uniqBy.mjs";
import { ary } from "../../function/ary.mjs";
import { iteratee } from "../util/iteratee.mjs";
import { isArrayLikeObject } from "../predicate/isArrayLikeObject.mjs";
import { flattenArrayLike } from "../_internal/flattenArrayLike.mjs";
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
	const lastValue = last(values);
	const flattened = flattenArrayLike(values);
	if (isArrayLikeObject(lastValue) || lastValue == null) return uniq(flattened);
	return uniqBy(flattened, ary(iteratee(lastValue), 1));
}
//#endregion
export { unionBy };
