const require_isArrayLike = require("./isArrayLike.js");
const require_isObjectLike = require("./isObjectLike.js");
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
	return require_isObjectLike.isObjectLike(value) && require_isArrayLike.isArrayLike(value);
}
//#endregion
exports.isArrayLikeObject = isArrayLikeObject;
