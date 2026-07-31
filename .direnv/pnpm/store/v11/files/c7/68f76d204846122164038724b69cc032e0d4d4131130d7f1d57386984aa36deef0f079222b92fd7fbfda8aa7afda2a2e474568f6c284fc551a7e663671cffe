const require_dropWhile = require("../../array/dropWhile.js");
const require_identity = require("../../function/identity.js");
const require_toArray = require("../_internal/toArray.js");
const require_isArrayLike = require("../predicate/isArrayLike.js");
const require_property = require("../object/property.js");
const require_matches = require("../predicate/matches.js");
const require_matchesProperty = require("../predicate/matchesProperty.js");
//#region src/compat/array/dropWhile.ts
/**
* Creates a slice of array excluding elements dropped from the beginning.
* Elements are dropped until predicate returns falsey.
* The predicate is invoked with three arguments: (value, index, array).
*
* @template T - The type of elements in the array
* @param array - The array to query
* @param [predicate=identity] - The function invoked per iteration
* @returns Returns the slice of array
*
* @example
* dropWhile([1, 2, 3], n => n < 3)
* // => [3]
*
* dropWhile([{ a: 1, b: 2 }, { a: 1, b: 3 }], { a: 1 })
* // => []
*
* dropWhile([{ a: 1, b: 2 }, { a: 1, b: 3 }], ['a', 1])
* // => []
*
* dropWhile([{ a: 1, b: 2 }, { a: 1, b: 3 }], 'a')
* // => []
*/
function dropWhile(array, predicate = require_identity.identity) {
	if (!require_isArrayLike.isArrayLike(array)) return [];
	return dropWhileImpl(require_toArray.toArray(array), predicate);
}
function dropWhileImpl(arr, predicate) {
	switch (typeof predicate) {
		case "function": return require_dropWhile.dropWhile(arr, (item, index, arr) => Boolean(predicate(item, index, arr)));
		case "object": if (Array.isArray(predicate) && predicate.length === 2) {
			const key = predicate[0];
			const value = predicate[1];
			return require_dropWhile.dropWhile(arr, require_matchesProperty.matchesProperty(key, value));
		} else return require_dropWhile.dropWhile(arr, require_matches.matches(predicate));
		case "number":
		case "symbol":
		case "string": return require_dropWhile.dropWhile(arr, require_property.property(predicate));
	}
}
//#endregion
exports.dropWhile = dropWhile;
