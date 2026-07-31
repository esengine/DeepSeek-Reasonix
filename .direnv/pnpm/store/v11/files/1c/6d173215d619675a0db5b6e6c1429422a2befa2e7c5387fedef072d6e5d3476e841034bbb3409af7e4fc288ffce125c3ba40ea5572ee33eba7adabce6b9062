const require_getTag = require("../_internal/getTag.js");
//#region src/compat/predicate/isError.ts
/**
* Checks if `value` is an Error object.
*
* @param value The value to check.
* @returns Returns `true` if `value` is an Error object, `false` otherwise.
*
* @example
* ```typescript
* console.log(isError(new Error())); // true
* console.log(isError('Error')); // false
* console.log(isError({ name: 'Error', message: '' })); // false
* ```
*/
function isError(value) {
	return require_getTag.getTag(value) === "[object Error]";
}
//#endregion
exports.isError = isError;
