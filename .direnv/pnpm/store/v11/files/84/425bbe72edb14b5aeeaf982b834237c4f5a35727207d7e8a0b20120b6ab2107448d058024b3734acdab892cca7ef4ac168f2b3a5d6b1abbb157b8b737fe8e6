const require_trimStart = require("../../string/trimStart.js");
//#region src/compat/string/trimStart.ts
/**
* Removes leading whitespace or specified characters from a string.
*
* @param str - The string from which leading characters will be trimmed.
* @param chars - The character(s) to remove from the start of the string.
* @param guard - Enables use as an iteratee for methods like `map`.
* @returns Returns the trimmed string.
*
* @example
* trimStart('  abc  ');
* // => 'abc  '
*
* trimStart('-_-abc-_-', '_-');
* // => 'abc-_-'
*/
function trimStart(str, chars, guard) {
	if (str == null) return "";
	if (guard != null || chars == null) return str.toString().trimStart();
	return require_trimStart.trimStart(str, chars.toString().split(""));
}
//#endregion
exports.trimStart = trimStart;
