const require_capitalize = require("./capitalize.js");
const require_words = require("./words.js");
//#region src/string/pascalCase.ts
/**
* Converts a string to Pascal case.
*
* Pascal case is the naming convention in which each word is capitalized and concatenated without any separator characters.
*
* @param str - The string that is to be changed to pascal case.
* @returns The converted string to Pascal case.
*
* @example
* const convertedStr1 = pascalCase('pascalCase') // returns 'PascalCase'
* const convertedStr2 = pascalCase('some whitespace') // returns 'SomeWhitespace'
* const convertedStr3 = pascalCase('hyphen-text') // returns 'HyphenText'
* const convertedStr4 = pascalCase('HTTPRequest') // returns 'HttpRequest'
*/
function pascalCase(str) {
	return require_words.words(str).map((word) => require_capitalize.capitalize(word)).join("");
}
//#endregion
exports.pascalCase = pascalCase;
