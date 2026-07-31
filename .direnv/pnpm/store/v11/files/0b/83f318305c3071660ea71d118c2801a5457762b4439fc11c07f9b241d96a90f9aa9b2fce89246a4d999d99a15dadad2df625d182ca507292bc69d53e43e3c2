const require_property = require("../object/property.js");
const require_matches = require("../predicate/matches.js");
const require_matchesProperty = require("../predicate/matchesProperty.js");
const require_identity = require("../function/identity.js");
//#region src/compat/array/findIndex.ts
/**
* Finds the index of the first item in an array that has a specific property, where the property name is provided as a PropertyKey.
*
* @template T
* @param arr - The array to search through.
* @param doesMatch - The criteria to match against the items in the array. This can be a function, a partial object, a key-value pair, or a property name.
* @param propertyToCheck - The property name to check for in the items of the array.
* @param [fromIndex=0] - The index to start the search from, defaults to 0.
* @returns The index of the first item that has the specified property, or `-1` if no match is found.
*
* @example
* // Using a property name
* const items = [{ id: 1, name: 'Alice' }, { id: 2, name: 'Bob' }];
* const result = findIndex(items, 'name');
* console.log(result); // 0
*/
function findIndex(arr, doesMatch = require_identity.identity, fromIndex = 0) {
	if (!arr) return -1;
	if (fromIndex < 0) fromIndex = Math.max(arr.length + fromIndex, 0);
	const subArray = Array.from(arr).slice(fromIndex);
	let index = -1;
	switch (typeof doesMatch) {
		case "function":
			index = subArray.findIndex(doesMatch);
			break;
		case "object":
			if (Array.isArray(doesMatch) && doesMatch.length === 2) {
				const key = doesMatch[0];
				const value = doesMatch[1];
				index = subArray.findIndex(require_matchesProperty.matchesProperty(key, value));
			} else index = subArray.findIndex(require_matches.matches(doesMatch));
			break;
		case "number":
		case "symbol":
		case "string": index = subArray.findIndex(require_property.property(doesMatch));
	}
	return index === -1 ? -1 : index + fromIndex;
}
//#endregion
exports.findIndex = findIndex;
