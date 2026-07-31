const require_camelCase = require("../../string/camelCase.js");
const require_deburr = require("./deburr.js");
const require_normalizeForCase = require("../_internal/normalizeForCase.js");
//#region src/compat/string/camelCase.ts
/**
* Converts a string to camel case.
*
* Camel case is the naming convention in which the first word is written in lowercase and
* each subsequent word begins with a capital letter, concatenated without any separator characters.
*
* @param str - The string that is to be changed to camel case.
* @returns The converted string to camel case.
*
* @example
* const convertedStr1 = camelCase('camelCase') // returns 'camelCase'
* const convertedStr2 = camelCase('some whitespace') // returns 'someWhitespace'
* const convertedStr3 = camelCase('hyphen-text') // returns 'hyphenText'
* const convertedStr4 = camelCase('HTTPRequest') // returns 'httpRequest'
*/
function camelCase(str) {
	return require_camelCase.camelCase(require_normalizeForCase.normalizeForCase(require_deburr.deburr(str)));
}
//#endregion
exports.camelCase = camelCase;
