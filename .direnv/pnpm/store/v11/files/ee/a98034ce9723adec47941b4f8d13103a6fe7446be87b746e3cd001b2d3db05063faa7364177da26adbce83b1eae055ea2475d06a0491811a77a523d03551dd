const require_words = require("./words.js");
//#region src/string/constantCase.ts
/**
* Converts a string to constant case.
*
* Constant case is a naming convention where each word is written in uppercase letters and separated by an underscore (`_`). For example, `CONSTANT_CASE`.
*
* @param str - The string that is to be changed to constant case.
* @returns The converted string to constant case.
*
* @example
* const convertedStr1 = constantCase('camelCase') // returns 'CAMEL_CASE'
* const convertedStr2 = constantCase('some whitespace') // returns 'SOME_WHITESPACE'
* const convertedStr3 = constantCase('hyphen-text') // returns 'HYPHEN_TEXT'
* const convertedStr4 = constantCase('HTTPRequest') // returns 'HTTP_REQUEST'
*/
function constantCase(str) {
	return require_words.words(str).map((word) => word.toUpperCase()).join("_");
}
//#endregion
exports.constantCase = constantCase;
