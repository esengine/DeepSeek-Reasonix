import { toString } from "../util/toString.mjs";
//#region src/compat/string/padEnd.ts
/**
* Pads the end of a string with a given character until it reaches the specified length.
*
* If the length is less than or equal to the original string's length, or if the padding character is an empty string,
* the original string is returned unchanged.
*
* @param str - The string to pad.
* @param [length] - The length of the resulting string once padded.
* @param [chars] - The character(s) to use for padding.
* @returns The padded string, or the original string if padding is not required.
*
* @example
* const result1 = padEnd('abc', 6);          // result will be 'abc   '
* const result2 = padEnd('abc', 6, '_-');    // result will be 'abc_-_'
* const result3 = padEnd('abc', 3);          // result will be 'abc'
* const result4 = padEnd('abc', 2);          // result will be 'abc'
*/
function padEnd(str, length = 0, chars = " ") {
	return toString(str).padEnd(length, chars);
}
//#endregion
export { padEnd };
