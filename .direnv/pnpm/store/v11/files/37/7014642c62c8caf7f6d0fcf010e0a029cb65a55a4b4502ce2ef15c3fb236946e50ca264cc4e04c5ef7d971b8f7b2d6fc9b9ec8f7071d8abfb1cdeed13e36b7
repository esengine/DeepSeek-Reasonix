const require_shuffle = require("../../array/shuffle.js");
const require_isArray = require("../predicate/isArray.js");
const require_isArrayLike = require("../predicate/isArrayLike.js");
const require_isObjectLike = require("../predicate/isObjectLike.js");
const require_values = require("../object/values.js");
const require_isNil = require("../predicate/isNil.js");
//#region src/compat/array/shuffle.ts
/**
* Randomizes the order of elements in an `collection` using the Fisher-Yates algorithm.
*
* This function takes an `collection` and returns a new `collection` with its elements shuffled in a random order.
*
* @template T - The type of elements in the `collection`.
* @param collection - The `collection` to shuffle.
* @returns A new `collection` with its elements shuffled in random order.
*/
function shuffle(collection) {
	if (require_isNil.isNil(collection)) return [];
	if (require_isArray.isArray(collection)) return require_shuffle.shuffle(collection);
	if (require_isArrayLike.isArrayLike(collection)) return require_shuffle.shuffle(Array.from(collection));
	if (require_isObjectLike.isObjectLike(collection)) return require_shuffle.shuffle(require_values.values(collection));
	return [];
}
//#endregion
exports.shuffle = shuffle;
