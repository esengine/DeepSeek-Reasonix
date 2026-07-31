import { isArrayLike } from "../predicate/isArrayLike.mjs";
import { isMap } from "../predicate/isMap.mjs";
//#region src/compat/util/toArray.ts
/**
* Converts a value to an array.
*
* @param value - The value to convert.
* @returns Returns the converted array.
*
* @example
* toArray({ 'a': 1, 'b': 2 }) // => returns [1,2]
* toArray('abc') // => returns ['a', 'b', 'c']
* toArray(1) // => returns []
* toArray(null) // => returns []
*/
function toArray(value) {
	if (value == null) return [];
	if (isArrayLike(value) || isMap(value)) return Array.from(value);
	if (typeof value === "object") return Object.values(value);
	return [];
}
//#endregion
export { toArray };
