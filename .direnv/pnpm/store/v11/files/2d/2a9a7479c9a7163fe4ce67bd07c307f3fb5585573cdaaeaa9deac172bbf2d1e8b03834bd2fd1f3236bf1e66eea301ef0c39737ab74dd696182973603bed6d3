const require_identity = require("../../function/identity.js");
//#region src/compat/object/forInRight.ts
/**
* Iterates over an object in reverse order and invokes the `iteratee` function for each property.
*
* Iterates over string keyed properties including inherited properties in reverse order.
*
* The iteration is terminated early if the `iteratee` function returns `false`.
*
* @template T - The type of the object
* @param object - The object to iterate over
* @param iteratee - The function invoked per iteration
* @returns Returns the object
*
* @example
* // Iterate over all properties including inherited ones
* const obj = { a: 1, b: 2 };
* forInRight(obj, (value, key) => {
*   console.log(key, value);
* });
* // Output: 'b' 2, 'a' 1
*
* // Early termination
* forInRight(obj, (value, key) => {
*   console.log(key, value);
*   return key !== 'a'; // stop after 'a'
* });
* // Output: 'b' 2
*/
function forInRight(object, iteratee = require_identity.identity) {
	if (object == null) return object;
	const keys = [];
	for (const key in object) keys.push(key);
	for (let i = keys.length - 1; i >= 0; i--) {
		const key = keys[i];
		if (iteratee(object[key], key, object) === false) break;
	}
	return object;
}
//#endregion
exports.forInRight = forInRight;
