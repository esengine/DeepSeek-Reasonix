const require_capitalize = require("./capitalize.js");
const require_words = require("./words.js");
//#region src/string/camelCase.ts
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
* const convertedStr5 = camelCase('Keep unicode 😅') // returns 'keepUnicode😅'
*/
function camelCase(str) {
	const words$1 = require_words.words(str);
	if (words$1.length === 0) return "";
	const [first, ...rest] = words$1;
	return `${first.toLowerCase()}${rest.map((word) => require_capitalize.capitalize(word)).join("")}`;
}
//#endregion
exports.camelCase = camelCase;
