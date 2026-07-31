const require_isObject = require("../predicate/isObject.js");
const require_iteratee = require("../util/iteratee.js");
const require_identity = require("../function/identity.js");
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
	if (!require_isObject.isObject(obj)) return;
	const iteratee$1 = require_iteratee.iteratee(predicate ?? require_identity.identity);
	return Object.keys(obj).findLast((key) => iteratee$1(obj[key], key, obj));
}
//#endregion
exports.findLastKey = findLastKey;
