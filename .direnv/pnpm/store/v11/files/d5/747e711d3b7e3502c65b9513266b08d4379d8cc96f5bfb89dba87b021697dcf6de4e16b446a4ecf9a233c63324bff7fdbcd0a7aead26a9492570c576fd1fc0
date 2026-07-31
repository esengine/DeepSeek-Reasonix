const require_capitalize = require("../../string/capitalize.js");
const require_toString = require("../util/toString.js");
//#region src/compat/string/capitalize.ts
/**
* Converts the first character of string to upper case and the remaining to lower case.
*
* @param string - The string to capitalize.
* @returns The capitalized string.
*
* @example
* const convertedStr1 = capitalize('fred') // returns 'Fred'
* const convertedStr2 = capitalize('FRED') // returns 'Fred'
* const convertedStr3 = capitalize('') // returns ''
*/
function capitalize(str) {
	return require_capitalize.capitalize(require_toString.toString(str));
}
//#endregion
exports.capitalize = capitalize;
