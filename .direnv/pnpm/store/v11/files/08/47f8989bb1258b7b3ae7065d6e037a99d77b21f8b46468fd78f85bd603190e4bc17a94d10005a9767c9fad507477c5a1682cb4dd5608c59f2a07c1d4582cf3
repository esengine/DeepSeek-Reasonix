import { identity } from "../../function/identity.mjs";
import { range } from "../../math/range.mjs";
import { isArrayLike } from "../predicate/isArrayLike.mjs";
//#region src/compat/array/forEachRight.ts
/**
* Iterates over elements of 'array' from right to left and invokes 'callback' for each element.
*
* @template T - The type of object.
* @param object - The object to iterate over.
* @param [callback] - The function invoked for each property.
* The callback function receives three arguments:
*  - 'value': The current property being processed in the object.
*  - 'key': The key of the current property being processed in the object.
*  - 'object': The object 'forEachRight' was called upon.
* @returns Returns the original object.
*
* @example
* forEachRight({'a': 1, 'b': 2 }, (value, key, object) => console.log(value, key));
* // Output:
* // 2 'b'
* // 1 'a'
*/
function forEachRight(collection, callback = identity) {
	if (!collection) return collection;
	const keys = isArrayLike(collection) ? range(0, collection.length) : Object.keys(collection);
	for (let i = keys.length - 1; i >= 0; i--) {
		const key = keys[i];
		const value = collection[key];
		if (callback(value, key, collection) === false) break;
	}
	return collection;
}
//#endregion
export { forEachRight };
