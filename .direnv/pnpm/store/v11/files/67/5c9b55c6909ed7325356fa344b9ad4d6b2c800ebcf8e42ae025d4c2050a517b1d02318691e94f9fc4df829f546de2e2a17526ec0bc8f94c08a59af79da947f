import { identity } from "../../function/identity.mjs";
import { isArrayLike } from "../predicate/isArrayLike.mjs";
import { property } from "../object/property.mjs";
import { matches } from "../predicate/matches.mjs";
import { matchesProperty } from "../predicate/matchesProperty.mjs";
import { isIterateeCall } from "../_internal/isIterateeCall.mjs";
//#region src/compat/array/every.ts
/**
* Checks if every item in an object has a specific property, where the property name is provided as a PropertyKey.
*
* @template T
* @param object - The object to check through.
* @param source - The source array or object to check through.
* @param doesMatch - The criteria to match. It can be a function, a partial object, a key-value pair, or a property name.
* @param propertyToCheck - The property name to check.
* @param guard - Enables use as an iteratee for methods like `_.map`.
* @returns `true` if every property value has the specified property, or `false` if at least one does not match.
*
* @example
* // Using a property name
* const obj = { a: { id: 1, name: 'Alice' }, b: { id: 2, name: 'Bob' } };
* const result = every(obj, 'name');
* console.log(result); // true
*/
function every(source, doesMatch, guard) {
	if (!source) return true;
	if (guard && isIterateeCall(source, doesMatch, guard)) doesMatch = void 0;
	if (!doesMatch) doesMatch = identity;
	let predicate;
	switch (typeof doesMatch) {
		case "function":
			predicate = doesMatch;
			break;
		case "object":
			if (Array.isArray(doesMatch) && doesMatch.length === 2) {
				const key = doesMatch[0];
				const value = doesMatch[1];
				predicate = matchesProperty(key, value);
			} else predicate = matches(doesMatch);
			break;
		case "symbol":
		case "number":
		case "string": predicate = property(doesMatch);
	}
	if (!isArrayLike(source)) {
		const keys = Object.keys(source);
		for (let i = 0; i < keys.length; i++) {
			const key = keys[i];
			const value = source[key];
			if (!predicate(value, key, source)) return false;
		}
		return true;
	}
	for (let i = 0; i < source.length; i++) if (!predicate(source[i], i, source)) return false;
	return true;
}
//#endregion
export { every };
