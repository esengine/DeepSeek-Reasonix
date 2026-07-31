import { isObject } from "../predicate/isObject.mjs";
//#region src/compat/string/truncate.ts
/**
* Used to compose unicode character classes.
* @link https://github.com/lodash/lodash/blob/4.17.21-es/_hasUnicode.js
*
* Used to detect strings with zero-width joiners or code points from the astral planes.
* @link http://eev.ee/blog/2015/09/12/dark-corners-of-unicode/
*/
const regexMultiByte = /[\u200d\ud800-\udfff\u0300-\u036f\ufe20-\ufe2f\u20d0-\u20ff\ufe0e\ufe0f]/;
/**
* This regex might more completely detect unicode, but it is slower and this project
* desires to mimic the behavior of lodash.
*/
/**
* Truncates `string` if it's longer than the given maximum string length.
* The last characters of the truncated string are replaced with the omission
* string which defaults to "...".
*
* @param [string=''] The string to truncate.
* @param [options={}] The options object.
* @param [options.length=30] The maximum string length.
* @param [options.omission='...'] The string to indicate text is omitted.
* @param [options.separator] The separator pattern to truncate to.
*
* @example
* const test = 'hi-diddly-ho there, neighborino';
* const truncatedStr1 = truncate(test) // returns 'hi-diddly-ho there, neighbo...'
* const truncatedStr2 = truncate(test, { length: 24, separator: ' ' }) // returns 'hi-diddly-ho there,...'
* const truncatedStr3 = truncate(test, { length: 24, separator: /,? +/ }) // returns 'hi-diddly-ho there...'
* const truncatedStr4 = truncate(test, { omission: ' [...]' }) // returns 'hi-diddly-ho there, neig [...]'
* const truncatedStr5 = truncate('ABC', { length: 3 }) // returns 'ABC'
* const truncatedStr6 = truncate('ABC', { length: 2 }) // returns '...'
* const truncatedStr7 = truncate('¥§✈✉🤓', { length: 5 }) // returns '¥§✈✉🤓'
* const truncatedStr8 = truncate('¥§✈✉🤓', { length: 4, omission: '…' }) // returns '¥§✈…'
*/
function truncate(string, options) {
	string = string != null ? `${string}` : "";
	let length = 30;
	let omission = "...";
	if (isObject(options)) {
		length = parseLength(options.length);
		omission = "omission" in options ? `${options.omission}` : "...";
	}
	let i = string.length;
	const lengthOmission = Array.from(omission).length;
	const lengthBase = Math.max(length - lengthOmission, 0);
	let strArray = void 0;
	if (regexMultiByte.test(string)) {
		strArray = Array.from(string);
		i = strArray.length;
	}
	if (length >= i) return string;
	if (i <= lengthOmission) return omission;
	let base = strArray === void 0 ? string.slice(0, lengthBase) : strArray?.slice(0, lengthBase).join("");
	const separator = options?.separator;
	if (!separator) {
		base += omission;
		return base;
	}
	const search = separator instanceof RegExp ? separator.source : separator;
	const flags = "u" + (separator instanceof RegExp ? separator.flags.replace("u", "") : "");
	const withoutSeparator = new RegExp(`(?<result>.*(?:(?!${search}).))(?:${search})`, flags).exec(base);
	return (!withoutSeparator?.groups ? base : withoutSeparator.groups.result) + omission;
}
function parseLength(length) {
	if (length == null) return 30;
	if (length <= 0) return 0;
	return length;
}
//#endregion
export { truncate };
