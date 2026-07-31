import { isObject } from "../predicate/isObject.mjs";
import { iteratee } from "../util/iteratee.mjs";
import { identity } from "../function/identity.mjs";
//#region src/compat/object/findLastKey.ts
/**
* Finds the key of the last element that matches the given predicate.
*
* This function determines the type of the predicate and delegates the search
* to the appropriate helper function. It supports predicates as functions, objects,
* arrays, or strings.
*
* @template T - The type of the object.
* @param obj - The object to inspect.
* @param predicate - The predicate to match.
* @returns Returns the key of the matched element, else `undefined`.
*/
function findLastKey(obj, predicate) {
	if (!isObject(obj)) return;
	const iteratee$1 = iteratee(predicate ?? identity);
	return Object.keys(obj).findLast((key) => iteratee$1(obj[key], key, obj));
}
//#endregion
export { findLastKey };
