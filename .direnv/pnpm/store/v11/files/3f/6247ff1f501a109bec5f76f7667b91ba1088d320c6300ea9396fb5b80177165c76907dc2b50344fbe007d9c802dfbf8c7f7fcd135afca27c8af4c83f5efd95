const require_findKey = require("../../object/findKey.js");
const require_isObject = require("../predicate/isObject.js");
const require_iteratee = require("../util/iteratee.js");
const require_identity = require("../function/identity.js");
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
	if (!require_isObject.isObject(obj)) return;
	const iteratee$1 = require_iteratee.iteratee(predicate ?? require_identity.identity);
	return require_findKey.findKey(obj, iteratee$1);
}
//#endregion
exports.findKey = findKey;
