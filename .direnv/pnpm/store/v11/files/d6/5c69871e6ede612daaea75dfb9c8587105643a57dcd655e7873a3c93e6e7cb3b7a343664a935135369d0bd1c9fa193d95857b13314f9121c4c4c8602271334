const require_isArrayLike = require("../predicate/isArrayLike.js");
//#region src/compat/object/fromPairs.ts
/**
* Converts an array of key-value pairs into an object.
*
* @template T - The type of the keys in the resulting object. It must extend `PropertyKey`.
* @template U - The type of the values in the resulting object.
*
* @param pairs - An array of key-value pairs where each key is a `PropertyKey` and each value is of type `U`.
* @returns An object where the keys are of type `T` and the values are of type `U`.
*
* @example
* const pairs = [['a', 1], ['b', 2]];
* const result = fromPairs(pairs);
* // result will be: { a: 1, b: 2 }
*/
function fromPairs(pairs) {
	if (!require_isArrayLike.isArrayLike(pairs)) return {};
	const result = {};
	for (let i = 0; i < pairs.length; i++) {
		const [key, value] = pairs[i];
		result[key] = value;
	}
	return result;
}
//#endregion
exports.fromPairs = fromPairs;
