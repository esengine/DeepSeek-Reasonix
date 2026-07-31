import { words } from "./words.mjs";
//#region src/string/lowerCase.ts
/**
* Converts a string to lower case.
*
* Lower case is the naming convention in which each word is written in lowercase and separated by an space ( ) character.
*
* @param str - The string that is to be changed to lower case.
* @returns The converted string to lower case.
*
* @example
* const convertedStr1 = lowerCase('camelCase') // returns 'camel case'
* const convertedStr2 = lowerCase('some whitespace') // returns 'some whitespace'
* const convertedStr3 = lowerCase('hyphen-text') // returns 'hyphen text'
* const convertedStr4 = lowerCase('HTTPRequest') // returns 'http request'
*/
function lowerCase(str) {
	return words(str).map((word) => word.toLowerCase()).join(" ");
}
//#endregion
export { lowerCase };
