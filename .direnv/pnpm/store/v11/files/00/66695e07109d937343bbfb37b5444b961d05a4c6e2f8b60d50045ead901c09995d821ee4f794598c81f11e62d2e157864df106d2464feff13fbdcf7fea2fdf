const require_intersectionBy = require("../../array/intersectionBy.js");
const require_last = require("../../array/last.js");
const require_uniq = require("../../array/uniq.js");
const require_identity = require("../../function/identity.js");
const require_property = require("../object/property.js");
const require_isArrayLikeObject = require("../predicate/isArrayLikeObject.js");
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
	if (!require_isArrayLikeObject.isArrayLikeObject(array)) return [];
	const lastValue = require_last.last(values);
	if (lastValue === void 0) return Array.from(array);
	let result = require_uniq.uniq(Array.from(array));
	const count = require_isArrayLikeObject.isArrayLikeObject(lastValue) ? values.length : values.length - 1;
	for (let i = 0; i < count; ++i) {
		const value = values[i];
		if (!require_isArrayLikeObject.isArrayLikeObject(value)) return [];
		if (require_isArrayLikeObject.isArrayLikeObject(lastValue)) result = require_intersectionBy.intersectionBy(result, Array.from(value), require_identity.identity);
		else if (typeof lastValue === "function") result = require_intersectionBy.intersectionBy(result, Array.from(value), (value) => lastValue(value));
		else if (typeof lastValue === "string") result = require_intersectionBy.intersectionBy(result, Array.from(value), require_property.property(lastValue));
	}
	return result;
}
//#endregion
exports.intersectionBy = intersectionBy;
