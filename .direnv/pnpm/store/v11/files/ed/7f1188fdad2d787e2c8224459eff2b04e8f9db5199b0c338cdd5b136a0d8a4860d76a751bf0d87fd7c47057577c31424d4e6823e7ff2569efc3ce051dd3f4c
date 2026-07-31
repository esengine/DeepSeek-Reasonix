const require_toString = require("../util/toString.js");
const require_toInteger = require("../util/toInteger.js");
const require_isIterateeCall = require("../_internal/isIterateeCall.js");
const require_MAX_SAFE_INTEGER = require("../_internal/MAX_SAFE_INTEGER.js");
//#region src/compat/string/repeat.ts
/**
* Repeats the given string n times.
*
* If n is less than 1, an empty string is returned, or if the string is an empty string,
* the original string is returned unchanged.
*
* @param str - The string to repeat.
* @param n - The number of times to repeat the string.
* @returns The repeated string, or an empty string if n is less than 1.
*
* @example
* repeat('abc', 0); // ''
* repeat('abc', 2); // 'abcabc'
*/
function repeat(str, n, guard) {
	if (guard ? require_isIterateeCall.isIterateeCall(str, n, guard) : n === void 0) n = 1;
	else n = require_toInteger.toInteger(n);
	if (n < 1 || n > require_MAX_SAFE_INTEGER.MAX_SAFE_INTEGER) return "";
	return require_toString.toString(str).repeat(n);
}
//#endregion
exports.repeat = repeat;
