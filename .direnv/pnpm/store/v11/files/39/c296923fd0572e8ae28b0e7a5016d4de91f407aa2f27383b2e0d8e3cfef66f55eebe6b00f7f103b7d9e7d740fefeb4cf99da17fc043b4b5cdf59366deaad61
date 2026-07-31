import { trimEnd as trimEnd$1 } from "../../string/trimEnd.mjs";
//#region src/compat/string/trimEnd.ts
/**
* Removes trailing whitespace or specified characters from a string.
*
* @param str - The string from which trailing characters will be trimmed.
* @param chars - The character(s) to remove from the end of the string.
* @param guard - Enables use as an iteratee for methods like `map`.
* @returns Returns the trimmed string.
*
* @example
* trimEnd('  abc  ');
* // => '  abc'
*
* trimEnd('-_-abc-_-', '_-');
* // => '-_-abc'
*/
function trimEnd(str, chars, guard) {
	if (str == null) return "";
	if (guard != null || chars == null) return str.toString().trimEnd();
	return trimEnd$1(str, chars.toString().split(""));
}
//#endregion
export { trimEnd };
