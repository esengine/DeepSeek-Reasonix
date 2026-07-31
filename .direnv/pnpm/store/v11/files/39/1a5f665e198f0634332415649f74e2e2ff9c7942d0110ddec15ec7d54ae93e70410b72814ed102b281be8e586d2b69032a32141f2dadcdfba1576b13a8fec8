import { dropRightWhile as dropRightWhile$1 } from "../../array/dropRightWhile.mjs";
import { identity } from "../../function/identity.mjs";
import { toArray } from "../_internal/toArray.mjs";
import { isArrayLike } from "../predicate/isArrayLike.mjs";
import { property } from "../object/property.mjs";
import { matches } from "../predicate/matches.mjs";
import { matchesProperty } from "../predicate/matchesProperty.mjs";
//#region src/compat/array/dropRightWhile.ts
/**
* Removes elements from the end of an array until the predicate returns false.
*
* This function iterates over an array and drops elements from the end until the provided
* predicate function returns false. It then returns a new array with the remaining elements.
*
* @template T - The type of elements in the array.
* @param array - The array from which to drop elements.
* @param predicate - A predicate function that determines
* whether to continue dropping elements. The function is called with each element, index, and array, and dropping
* continues as long as it returns true.
* @returns A new array with the elements remaining after the predicate returns false.
*
* @example
* const array = [3, 2, 1];
* const result = dropRightWhile(array, (item, index, arr) => index >= 1);
* // Returns: [3]
*/
function dropRightWhile(array, predicate = identity) {
	if (!isArrayLike(array)) return [];
	return dropRightWhileImpl(toArray(array), predicate);
}
function dropRightWhileImpl(arr, predicate) {
	switch (typeof predicate) {
		case "function": return dropRightWhile$1(arr, (item, index, arr) => Boolean(predicate(item, index, arr)));
		case "object": if (Array.isArray(predicate) && predicate.length === 2) {
			const key = predicate[0];
			const value = predicate[1];
			return dropRightWhile$1(arr, matchesProperty(key, value));
		} else return dropRightWhile$1(arr, matches(predicate));
		case "symbol":
		case "number":
		case "string": return dropRightWhile$1(arr, property(predicate));
	}
}
//#endregion
export { dropRightWhile };
