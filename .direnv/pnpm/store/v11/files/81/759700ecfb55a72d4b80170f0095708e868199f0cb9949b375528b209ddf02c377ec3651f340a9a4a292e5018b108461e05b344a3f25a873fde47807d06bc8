const require_identity = require("../../function/identity.js");
const require_toArray = require("../_internal/toArray.js");
const require_property = require("../object/property.js");
const require_matches = require("../predicate/matches.js");
const require_matchesProperty = require("../predicate/matchesProperty.js");
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
function findLastIndex(arr, doesMatch = require_identity.identity, fromIndex = arr ? arr.length - 1 : 0) {
	if (!arr) return -1;
	if (fromIndex < 0) fromIndex = Math.max(arr.length + fromIndex, 0);
	else fromIndex = Math.min(fromIndex, arr.length - 1);
	const subArray = require_toArray.toArray(arr).slice(0, fromIndex + 1);
	switch (typeof doesMatch) {
		case "function": return subArray.findLastIndex(doesMatch);
		case "object": if (Array.isArray(doesMatch) && doesMatch.length === 2) {
			const key = doesMatch[0];
			const value = doesMatch[1];
			return subArray.findLastIndex(require_matchesProperty.matchesProperty(key, value));
		} else return subArray.findLastIndex(require_matches.matches(doesMatch));
		case "number":
		case "symbol":
		case "string": return subArray.findLastIndex(require_property.property(doesMatch));
	}
}
//#endregion
exports.findLastIndex = findLastIndex;
