import { isArrayLike } from "./isArrayLike.mjs";
import { isObjectLike } from "./isObjectLike.mjs";
//#region src/compat/predicate/isArrayLikeObject.ts
/**
* Checks if the given value is a non-primitive, array-like object.
*
* @param value The value to check.
* @returns `true` if the value is a non-primitive, array-like object, `false` otherwise.
*
* @example
* isArrayLikeObject([1, 2, 3]); // true
* isArrayLikeObject({ 0: 'a', length: 1 }); // true
* isArrayLikeObject('abc'); // false
* isArrayLikeObject(()=>{}); // false
*/
function isArrayLikeObject(value) {
	return isObjectLike(value) && isArrayLike(value);
}
//#endregion
export { isArrayLikeObject };
