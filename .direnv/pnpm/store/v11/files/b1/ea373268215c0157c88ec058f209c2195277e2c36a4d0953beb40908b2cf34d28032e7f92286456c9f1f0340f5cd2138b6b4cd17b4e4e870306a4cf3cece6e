import { toKey } from "../_internal/toKey.mjs";
import { get } from "../object/get.mjs";
import { isMatch } from "./isMatch.mjs";
import { cloneDeep } from "../object/cloneDeep.mjs";
import { has } from "../object/has.mjs";
//#region src/compat/predicate/matchesProperty.ts
/**
* Creates a function that checks if a given target object matches a specific property value.
*
* The returned function takes a target object and determines if the property at the
* specified path within the target object is equal to the given value.
*
* @param property - The property path to check within the target object.
*     This can be a single property key or an array of property keys.
* @param source - The value to compare against the property value in the target object.
*
* @returns A function that takes a target object and returns
*     `true` if the property value at the given path in the target object matches the provided value,
*     otherwise returns `false`.
*
* @example
* // Using a single property key
* const checkName = matchesProperty('name', 'Alice');
* console.log(checkName({ name: 'Alice' })); // true
* console.log(checkName({ name: 'Bob' })); // false
*
* // Using an array of property keys
* const checkNested = matchesProperty(['address', 'city'], 'New York');
* console.log(checkNested({ address: { city: 'New York' } })); // true
* console.log(checkNested({ address: { city: 'Los Angeles' } })); // false
*/
function matchesProperty(property, source) {
	switch (typeof property) {
		case "object":
			if (Object.is(property?.valueOf(), -0)) property = "-0";
			break;
		case "number":
			property = toKey(property);
			break;
	}
	source = cloneDeep(source);
	return function(target) {
		const result = get(target, property);
		if (result === void 0) return has(target, property);
		if (source === void 0) return result === void 0;
		return isMatch(result, source);
	};
}
//#endregion
export { matchesProperty };
