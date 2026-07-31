const require_upperCase = require("../../string/upperCase.js");
const require_deburr = require("./deburr.js");
const require_normalizeForCase = require("../_internal/normalizeForCase.js");
//#region src/compat/string/upperCase.ts
/**
* Converts a string to upper case.
*
* Upper case is the naming convention in which each word is written in uppercase and separated by an space ( ) character.
*
* @param str - The string that is to be changed to upper case.
* @returns The converted string to upper case.
*
* @example
* const convertedStr1 = upperCase('camelCase') // returns 'CAMEL CASE'
* const convertedStr2 = upperCase('some whitespace') // returns 'SOME WHITESPACE'
* const convertedStr3 = upperCase('hyphen-text') // returns 'HYPHEN TEXT'
* const convertedStr4 = upperCase('HTTPRequest') // returns 'HTTP REQUEST'
*/
function upperCase(str) {
	return require_upperCase.upperCase(require_normalizeForCase.normalizeForCase(require_deburr.deburr(str)));
}
//#endregion
exports.upperCase = upperCase;
