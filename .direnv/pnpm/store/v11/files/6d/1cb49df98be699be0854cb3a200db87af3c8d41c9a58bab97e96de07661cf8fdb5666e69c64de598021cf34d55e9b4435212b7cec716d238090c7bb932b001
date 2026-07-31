const require_isDeepKey = require("../_internal/isDeepKey.js");
const require_toPath = require("../util/toPath.js");
const require_isIndex = require("../_internal/isIndex.js");
const require_isArguments = require("../predicate/isArguments.js");
//#region src/compat/object/hasIn.ts
/**
* Checks if a given path exists in an object, **including inherited properties**.
*
* You can provide the path as a single property key, an array of property keys,
* or a string representing a deep path.
*
* Unlike `has`, which only checks for own properties, `hasIn` also checks for properties
* in the prototype chain.
*
* If the path is an index and the object is an array or an arguments object, the function will verify
* if the index is valid and within the bounds of the array or arguments object, even if the array or
* arguments object is sparse (i.e., not all indexes are defined).
*
* @template T
* @param object - The object to query.
* @param path - The path to check. This can be a single property key,
*        an array of property keys, or a string representing a deep path.
* @returns Returns `true` if the path exists (own or inherited), `false` otherwise.
*
* @example
*
* const obj = { a: { b: { c: 3 } } };
*
* hasIn(obj, 'a'); // true
* hasIn(obj, ['a', 'b']); // true
* hasIn(obj, ['a', 'b', 'c']); // true
* hasIn(obj, 'a.b.c'); // true
* hasIn(obj, 'a.b.d'); // false
* hasIn(obj, ['a', 'b', 'c', 'd']); // false
*
* // Example with inherited properties:
* function Rectangle() {}
* Rectangle.prototype.area = function() {};
*
* const rect = new Rectangle();
* hasIn(rect, 'area'); // true
* has(rect, 'area'); // false - has only checks own properties
*/
function hasIn(object, path) {
	if (object == null) return false;
	let resolvedPath;
	if (Array.isArray(path)) resolvedPath = path;
	else if (typeof path === "string" && require_isDeepKey.isDeepKey(path) && object[path] == null) resolvedPath = require_toPath.toPath(path);
	else resolvedPath = [path];
	if (resolvedPath.length === 0) return false;
	let current = object;
	for (let i = 0; i < resolvedPath.length; i++) {
		const key = resolvedPath[i];
		if (current == null || !(key in Object(current))) {
			if (!((Array.isArray(current) || require_isArguments.isArguments(current)) && require_isIndex.isIndex(key) && key < current.length)) return false;
		}
		current = current[key];
	}
	return true;
}
//#endregion
exports.hasIn = hasIn;
