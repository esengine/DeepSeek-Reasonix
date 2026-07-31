import { last } from "../../array/last.mjs";
import { uniq } from "../../array/uniq.mjs";
import { uniqWith } from "../../array/uniqWith.mjs";
import { isArrayLikeObject } from "../predicate/isArrayLikeObject.mjs";
import { flattenArrayLike } from "../_internal/flattenArrayLike.mjs";
//#region src/compat/array/unionWith.ts
/**
* This function takes multiple arrays and returns a new array containing only the unique values
* from all input arrays, preserving the order of their first occurrence.
* A comparator function can be provided for comparison and it output values from the first possible array
*
* @template T - The type of elements in the arrays.
* @param values - The arrays to inspect, or the comparator function.
* @returns Returns the new array of combined unique values.
*
* @example
* const objects = [
*   { x: 1, y: 2 },
*   { x: 2, y: 1 },
* ];
* const others = [
*   { x: 1, y: 1 },
*   { x: 1, y: 2 },
* ];
* // Returns [objects[0], objects[1], others[0]]
* unionWith(objects, others, isEqual);
*
* @example
* const objects = [{ x: 1, y: 1 }];
* const others = [{ x: 1, y: 2 }];
* // Returns [{ x: 1, y: 1 }]
* unionWith(objects, others, (a, b) => a.x === b.x);
*/
function unionWith(...values) {
	const lastValue = last(values);
	const flattened = flattenArrayLike(values);
	if (isArrayLikeObject(lastValue) || lastValue == null) return uniq(flattened);
	return uniqWith(flattened, lastValue);
}
//#endregion
export { unionWith };
