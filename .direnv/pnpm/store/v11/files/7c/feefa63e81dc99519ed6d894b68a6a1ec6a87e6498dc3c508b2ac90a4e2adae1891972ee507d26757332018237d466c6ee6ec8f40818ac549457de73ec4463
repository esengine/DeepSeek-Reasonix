import { intersectionBy as intersectionBy$1 } from "../../array/intersectionBy.mjs";
import { last } from "../../array/last.mjs";
import { uniq } from "../../array/uniq.mjs";
import { identity } from "../../function/identity.mjs";
import { property } from "../object/property.mjs";
import { isArrayLikeObject } from "../predicate/isArrayLikeObject.mjs";
//#region src/compat/array/intersectionBy.ts
/**
* Returns the intersection of multiple arrays after applying the iteratee function to their elements.
*
* This function takes multiple arrays and an optional iteratee function (or property key)
* to compare the elements after transforming them. It returns a new array containing the elements from
* the first array that are present in all subsequent arrays after applying the iteratee to each element.
* If no iteratee is provided, the identity function is used.
*
* If the first array is `null` or `undefined`, an empty array is returned.
*
* @template T
* @param array - The first array to compare.
* @param values - The arrays to compare, or the iteratee function.
* @returns A new array containing the elements from the first array that are present
*  in all subsequent arrays after applying the iteratee.
*
* @example
* const array1 = [{ x: 1 }, { x: 2 }, { x: 3 }];
* const array2 = [{ x: 2 }, { x: 3 }];
* const result = intersectionBy(array1, array2, 'x');
* // result will be [{ x: 2 }, { x: 3 }] since these elements have the same `x` property.
*
* @example
* const array1 = [1.1, 2.2, 3.3];
* const array2 = [2.3, 3.3];
* const result = intersectionBy(array1, array2, Math.floor);
* // result will be [2.3, 3.3] since it shares the same integer part when `Math.floor` is applied.
*/
function intersectionBy(array, ...values) {
	if (!isArrayLikeObject(array)) return [];
	const lastValue = last(values);
	if (lastValue === void 0) return Array.from(array);
	let result = uniq(Array.from(array));
	const count = isArrayLikeObject(lastValue) ? values.length : values.length - 1;
	for (let i = 0; i < count; ++i) {
		const value = values[i];
		if (!isArrayLikeObject(value)) return [];
		if (isArrayLikeObject(lastValue)) result = intersectionBy$1(result, Array.from(value), identity);
		else if (typeof lastValue === "function") result = intersectionBy$1(result, Array.from(value), (value) => lastValue(value));
		else if (typeof lastValue === "string") result = intersectionBy$1(result, Array.from(value), property(lastValue));
	}
	return result;
}
//#endregion
export { intersectionBy };
