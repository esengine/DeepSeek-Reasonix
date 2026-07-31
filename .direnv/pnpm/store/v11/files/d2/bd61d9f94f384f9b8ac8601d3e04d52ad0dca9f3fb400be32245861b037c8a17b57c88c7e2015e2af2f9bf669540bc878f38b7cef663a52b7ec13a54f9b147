const require_identity = require("../../function/identity.js");
const require_isArrayLike = require("../predicate/isArrayLike.js");
const require_iteratee = require("../util/iteratee.js");
const require_isObjectLike = require("../predicate/isObjectLike.js");
const require_reduce = require("./reduce.js");
//#region src/compat/array/keyBy.ts
/**
* Maps each element of an array or an object based on a provided key-generating function.
*
* This function takes an array or object and a function that generates a key from each element or value. It returns
* an object where the keys are the generated keys and the values are the corresponding elements or values.
* If there are multiple elements or values generating the same key, the last one among them is used
* as the value.
*
* @param collection - The collection to iterate over.
* @param [iteratee] - The iteratee to transform keys.
*   - If a function is provided, it's invoked for each element in the collection.
*   - If a property name (string) is provided, that property of each element is used as the key.
*   - If a property-value pair (array) is provided, elements with matching property values are used.
*   - If a partial object is provided, elements with matching properties are used.
*   - If omitted, the identity function is used.
* @returns Returns the composed aggregate object.
*
* @example
* // Using an array of objects
* keyBy([{ id: 'a' }, { id: 'b' }], 'id');
* // => { a: { id: 'a' }, b: { id: 'b' } }
*
* @example
* // Using a function iteratee
* keyBy(['a', 'b', 'c'], val => val.toUpperCase());
* // => { A: 'a', B: 'b', C: 'c' }
*/
function keyBy(collection, iteratee$1) {
	if (!require_isArrayLike.isArrayLike(collection) && !require_isObjectLike.isObjectLike(collection)) return {};
	const keyFn = require_iteratee.iteratee(iteratee$1 ?? require_identity.identity);
	return require_reduce.reduce(collection, (result, value) => {
		const key = keyFn(value);
		result[key] = value;
		return result;
	}, {});
}
//#endregion
exports.keyBy = keyBy;
