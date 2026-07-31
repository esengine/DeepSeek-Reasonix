import { isEqualsSameValueZero } from "../../_internal/isEqualsSameValueZero.mjs";
import "../util/eq.mjs";
import copyArray from "../_internal/copyArray.mjs";
//#region src/compat/array/pullAllWith.ts
/**
* Removes and returns elements from an array using a provided comparison function to determine which elements to remove.
*
* @template T
* @param array - The array to modify.
* @param values - The values to remove from the array.
* @param comparator - The function to compare elements of `array` with elements of `values`. Should return `true` if the two elements are considered equal.
* @returns The array with specified values removed.
*
* @example
* import pullAllWith from './pullAllWith';
* import isEqual from '../predicate';
*
* const array = [{ x: 1, y: 2 }, { x: 3, y: 4 }, { x: 5, y: 6 }];
* const valuesToRemove = [{ x: 3, y: 4 }];
*
* const result = pullAllWith(array, valuesToRemove, isEqual);
*
* console.log(result); // [{ x: 1, y: 2 }, { x: 5, y: 6 }]
* console.log(array);  // [{ x: 1, y: 2 }, { x: 5, y: 6 }]
*/
function pullAllWith(array, values, comparator) {
	if (array?.length == null || values?.length == null) return array;
	if (array === values) values = copyArray(values);
	let resultLength = 0;
	if (comparator == null) comparator = (a, b) => isEqualsSameValueZero(a, b);
	const valuesArray = Array.isArray(values) ? values : Array.from(values);
	const hasUndefined = valuesArray.includes(void 0);
	for (let i = 0; i < array.length; i++) {
		if (i in array) {
			if (!valuesArray.some((value) => comparator(array[i], value))) array[resultLength++] = array[i];
			continue;
		}
		if (!hasUndefined) delete array[resultLength++];
	}
	array.length = resultLength;
	return array;
}
//#endregion
export { pullAllWith };
