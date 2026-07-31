import { toString } from "../util/toString.mjs";
import { toInteger } from "../util/toInteger.mjs";
import { isIterateeCall } from "../_internal/isIterateeCall.mjs";
import { MAX_SAFE_INTEGER } from "../_internal/MAX_SAFE_INTEGER.mjs";
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
	if (guard ? isIterateeCall(str, n, guard) : n === void 0) n = 1;
	else n = toInteger(n);
	if (n < 1 || n > MAX_SAFE_INTEGER) return "";
	return toString(str).repeat(n);
}
//#endregion
export { repeat };
