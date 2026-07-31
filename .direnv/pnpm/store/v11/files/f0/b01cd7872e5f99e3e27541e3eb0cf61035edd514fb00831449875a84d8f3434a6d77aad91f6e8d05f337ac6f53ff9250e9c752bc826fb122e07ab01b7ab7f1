import { identity } from "../../function/identity.mjs";
import { toArray } from "../_internal/toArray.mjs";
import { property } from "../object/property.mjs";
import { matches } from "../predicate/matches.mjs";
import { matchesProperty } from "../predicate/matchesProperty.mjs";
//#region src/compat/array/findLastIndex.ts
/**
* Finds the index of the last element in the array that satisfies the predicate.
*
* @template T
* @param arr - The array to search through.
* @param doesMatch - The predicate function, partial object, property-value pair, or property name.
* @param [fromIndex=arr.length - 1] - The index to start the search from, defaults to the last index of the array.
* @returns The index of the last matching element, or -1 if not found.
*
* @example
* const items = [{ id: 1, name: 'Alice' }, { id: 2, name: 'Bob' }];
* findLastIndex(items, 'name');
* // => 1
*/
function findLastIndex(arr, doesMatch = identity, fromIndex = arr ? arr.length - 1 : 0) {
	if (!arr) return -1;
	if (fromIndex < 0) fromIndex = Math.max(arr.length + fromIndex, 0);
	else fromIndex = Math.min(fromIndex, arr.length - 1);
	const subArray = toArray(arr).slice(0, fromIndex + 1);
	switch (typeof doesMatch) {
		case "function": return subArray.findLastIndex(doesMatch);
		case "object": if (Array.isArray(doesMatch) && doesMatch.length === 2) {
			const key = doesMatch[0];
			const value = doesMatch[1];
			return subArray.findLastIndex(matchesProperty(key, value));
		} else return subArray.findLastIndex(matches(doesMatch));
		case "number":
		case "symbol":
		case "string": return subArray.findLastIndex(property(doesMatch));
	}
}
//#endregion
export { findLastIndex };
