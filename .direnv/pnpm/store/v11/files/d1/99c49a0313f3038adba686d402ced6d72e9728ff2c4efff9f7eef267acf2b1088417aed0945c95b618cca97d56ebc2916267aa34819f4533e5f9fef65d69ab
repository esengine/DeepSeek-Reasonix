import { isEqualsSameValueZero } from "../../_internal/isEqualsSameValueZero.mjs";
import "../util/eq.mjs";
import { toInteger } from "../util/toInteger.mjs";
import { isString } from "../predicate/isString.mjs";
//#region src/compat/array/includes.ts
/**
* Checks if a specified value exists within a given source, which can be an array, an object, or a string.
*
* The comparison uses SameValueZero to check for inclusion.
*
* @param source - The source to search in. It can be an array, an object, or a string.
* @param [target] - The value to search for in the source.
* @param [fromIndex=0] - The index to start searching from. If negative, it is treated as an offset from the end of the source.
* @returns `true` if the value is found in the source, `false` otherwise.
*
* @example
* includes([1, 2, 3], 2); // true
* includes({ a: 1, b: 'a', c: NaN }, 'a'); // true
* includes('hello world', 'world'); // true
* includes('hello world', 'test'); // false
*/
function includes(source, target, fromIndex, guard) {
	if (source == null) return false;
	if (guard || !fromIndex) fromIndex = 0;
	else fromIndex = toInteger(fromIndex);
	if (isString(source)) {
		if (fromIndex > source.length || target instanceof RegExp) return false;
		if (fromIndex < 0) fromIndex = Math.max(0, source.length + fromIndex);
		return source.includes(target, fromIndex);
	}
	if (Array.isArray(source)) return source.includes(target, fromIndex);
	const keys = Object.keys(source);
	if (fromIndex < 0) fromIndex = Math.max(0, keys.length + fromIndex);
	for (let i = fromIndex; i < keys.length; i++) if (isEqualsSameValueZero(Reflect.get(source, keys[i]), target)) return true;
	return false;
}
//#endregion
export { includes };
