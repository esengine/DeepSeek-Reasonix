const require_dropRightWhile = require("../../array/dropRightWhile.js");
const require_identity = require("../../function/identity.js");
const require_toArray = require("../_internal/toArray.js");
const require_isArrayLike = require("../predicate/isArrayLike.js");
const require_property = require("../object/property.js");
const require_matches = require("../predicate/matches.js");
const require_matchesProperty = require("../predicate/matchesProperty.js");
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
function dropRightWhile(array, predicate = require_identity.identity) {
	if (!require_isArrayLike.isArrayLike(array)) return [];
	return dropRightWhileImpl(require_toArray.toArray(array), predicate);
}
function dropRightWhileImpl(arr, predicate) {
	switch (typeof predicate) {
		case "function": return require_dropRightWhile.dropRightWhile(arr, (item, index, arr) => Boolean(predicate(item, index, arr)));
		case "object": if (Array.isArray(predicate) && predicate.length === 2) {
			const key = predicate[0];
			const value = predicate[1];
			return require_dropRightWhile.dropRightWhile(arr, require_matchesProperty.matchesProperty(key, value));
		} else return require_dropRightWhile.dropRightWhile(arr, require_matches.matches(predicate));
		case "symbol":
		case "number":
		case "string": return require_dropRightWhile.dropRightWhile(arr, require_property.property(predicate));
	}
}
//#endregion
exports.dropRightWhile = dropRightWhile;
