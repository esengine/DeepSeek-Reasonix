import { iteratee } from "../util/iteratee.mjs";
import { isSymbol } from "../predicate/isSymbol.mjs";
import { identity } from "../function/identity.mjs";
import { keysIn } from "./keysIn.mjs";
import { getSymbolsIn } from "../_internal/getSymbolsIn.mjs";
//#region src/compat/object/omitBy.ts
/**
* Creates a new object composed of the properties that do not satisfy the predicate function.
*
* This function takes an object and a predicate function, and returns a new object that
* includes only the properties for which the predicate function returns false.
*
* @template T - The type of object.
* @param obj - The object to omit properties from.
* @param shouldOmit - A predicate function that determines
* whether a property should be omitted. It takes the property's value, key, and the source object as arguments and returns `true`
* if the property should be omitted, and `false` otherwise.
* @returns Returns the new object.
*
* @example
* const obj = { a: 1, b: 'omit', c: 3 };
* const shouldOmit = (value) => typeof value === 'string';
* const result = omitBy(obj, shouldOmit);
* // result will be { a: 1, c: 3 }
*/
function omitBy(object, shouldOmit) {
	if (object == null) return {};
	const result = {};
	const predicate = iteratee(shouldOmit ?? identity);
	const keys = [...keysIn(object), ...getSymbolsIn(object)];
	for (let i = 0; i < keys.length; i++) {
		const key = isSymbol(keys[i]) ? keys[i] : keys[i].toString();
		const value = object[key];
		if (!predicate(value, key, object)) result[key] = value;
	}
	return result;
}
//#endregion
export { omitBy };
