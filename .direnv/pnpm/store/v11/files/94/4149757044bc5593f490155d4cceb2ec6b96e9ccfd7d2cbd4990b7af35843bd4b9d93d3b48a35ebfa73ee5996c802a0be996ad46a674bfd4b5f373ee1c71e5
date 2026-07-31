import { findKey as findKey$1 } from "../../object/findKey.mjs";
import { isObject } from "../predicate/isObject.mjs";
import { iteratee } from "../util/iteratee.mjs";
import { identity } from "../function/identity.mjs";
//#region src/compat/object/findKey.ts
/**
* Finds the key of the first element that matches the given predicate.
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
function findKey(obj, predicate) {
	if (!isObject(obj)) return;
	return findKey$1(obj, iteratee(predicate ?? identity));
}
//#endregion
export { findKey };
