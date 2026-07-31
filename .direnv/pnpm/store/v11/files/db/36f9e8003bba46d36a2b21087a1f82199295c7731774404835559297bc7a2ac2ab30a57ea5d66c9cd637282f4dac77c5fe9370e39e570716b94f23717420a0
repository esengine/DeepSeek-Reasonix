const require_isNumber = require("./isNumber.js");
//#region src/compat/predicate/isNaN.ts
/**
* Checks if the value is NaN.
*
* @param value - The value to check.
* @returns `true` if the value is NaN, `false` otherwise.
*
* @example
* isNaN(NaN); // true
* isNaN(0); // false
* isNaN('NaN'); // false
* isNaN(undefined); // false
*/
function isNaN(value) {
	return require_isNumber.isNumber(value) && Number.isNaN(Number(value));
}
//#endregion
exports.isNaN = isNaN;
