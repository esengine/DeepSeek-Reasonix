const require_isArrayBuffer = require("../../predicate/isArrayBuffer.js");
//#region src/compat/predicate/isArrayBuffer.ts
/**
* Checks if a given value is `ArrayBuffer`.
*
* This function can also serve as a type predicate in TypeScript, narrowing the type of the argument to `ArrayBuffer`.
*
* @param value The value to check if it is a `ArrayBuffer`.
* @returns Returns `true` if `value` is a `ArrayBuffer`, else `false`.
*
* @example
* const value1 = new ArrayBuffer();
* const value2 = new Array();
* const value3 = new Map();
*
* console.log(isArrayBuffer(value1)); // true
* console.log(isArrayBuffer(value2)); // false
* console.log(isArrayBuffer(value3)); // false
*/
function isArrayBuffer(value) {
	return require_isArrayBuffer.isArrayBuffer(value);
}
//#endregion
exports.isArrayBuffer = isArrayBuffer;
