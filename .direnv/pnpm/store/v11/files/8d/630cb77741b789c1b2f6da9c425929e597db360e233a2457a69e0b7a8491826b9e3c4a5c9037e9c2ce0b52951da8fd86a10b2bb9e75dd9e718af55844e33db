const require_identity = require("../../function/identity.js");
const require_range = require("../../math/range.js");
const require_isArrayLike = require("../predicate/isArrayLike.js");
//#region src/compat/array/forEach.ts
/**
* Iterates over each element of the object invoking the provided callback function for each property.
*
* @template T - The type of object.
* @param object - The object to iterate over.
* @param [callback] - The function invoked for each property.
* The callback function receives three arguments:
*  - 'value': The current property being processed in the object.
*  - 'key': The key of the current property being processed in the object.
*  - 'object': The object 'forEach' was called upon.
* @returns Returns the original object.
*
* @example
* forEach({'a': 1, 'b': 2 }, (value, key, object) => console.log(value, key));
* // Output:
* // 1 'a'
* // 2 'b'
*/
function forEach(collection, callback = require_identity.identity) {
	if (!collection) return collection;
	const keys = require_isArrayLike.isArrayLike(collection) || Array.isArray(collection) ? require_range.range(0, collection.length) : Object.keys(collection);
	for (let i = 0; i < keys.length; i++) {
		const key = keys[i];
		const value = collection[key];
		if (callback(value, key, collection) === false) break;
	}
	return collection;
}
//#endregion
exports.forEach = forEach;
