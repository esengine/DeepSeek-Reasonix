const require_snakeCase = require("../../string/snakeCase.js");
const require_deburr = require("./deburr.js");
const require_normalizeForCase = require("../_internal/normalizeForCase.js");
//#region src/compat/string/snakeCase.ts
/**
* Converts a string to snake case.
*
* Snake case is the naming convention in which each word is written in lowercase and separated by an underscore (_) character.
*
* @param str - The string that is to be changed to snake case.
* @returns The converted string to snake case.
*
* @example
* const convertedStr1 = snakeCase('camelCase') // returns 'camel_case'
* const convertedStr2 = snakeCase('some whitespace') // returns 'some_whitespace'
* const convertedStr3 = snakeCase('hyphen-text') // returns 'hyphen_text'
* const convertedStr4 = snakeCase('HTTPRequest') // returns 'http_request'
*/
function snakeCase(str) {
	return require_snakeCase.snakeCase(require_normalizeForCase.normalizeForCase(require_deburr.deburr(str)));
}
//#endregion
exports.snakeCase = snakeCase;
