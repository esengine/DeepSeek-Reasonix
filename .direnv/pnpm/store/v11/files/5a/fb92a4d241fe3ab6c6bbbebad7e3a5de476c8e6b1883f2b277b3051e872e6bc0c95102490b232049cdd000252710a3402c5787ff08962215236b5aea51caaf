const require_trimEnd = require("./trimEnd.js");
const require_trimStart = require("./trimStart.js");
//#region src/string/trim.ts
/**
* Removes leading and trailing whitespace or specified characters from a string.
*
* @param str - The string from which characters will be trimmed.
* @param chars - The character(s) to remove from the string. Can be a single character or an array of characters.
* @returns The resulting string after the specified characters have been removed.
*
* @example
* trim("  hello  "); // "hello"
* trim("--hello--", "-"); // "hello"
* trim("##hello##", ["#", "o"]); // "hell"
*/
function trim(str, chars) {
	if (chars === void 0) return str.trim();
	return require_trimStart.trimStart(require_trimEnd.trimEnd(str, chars), chars);
}
//#endregion
exports.trim = trim;
