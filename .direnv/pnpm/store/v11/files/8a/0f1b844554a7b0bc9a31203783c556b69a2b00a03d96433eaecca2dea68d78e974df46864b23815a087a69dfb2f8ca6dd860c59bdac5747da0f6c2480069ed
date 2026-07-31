import { identity } from "../../function/identity.mjs";
//#region src/compat/object/forIn.ts
/**
* Iterates over an object and invokes the `iteratee` function for each property.
*
* Iterates over string keyed properties including inherited properties.
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
* forIn(obj, (value, key) => {
*   console.log(key, value);
* });
* // Output: 'a' 1, 'b' 2
*
* // Early termination
* forIn(obj, (value, key) => {
*   console.log(key, value);
*   return key !== 'a'; // stop after 'a'
* });
* // Output: 'a' 1
*/
function forIn(object, iteratee = identity) {
	if (object == null) return object;
	for (const key in object) if (iteratee(object[key], key, object) === false) break;
	return object;
}
//#endregion
export { forIn };
