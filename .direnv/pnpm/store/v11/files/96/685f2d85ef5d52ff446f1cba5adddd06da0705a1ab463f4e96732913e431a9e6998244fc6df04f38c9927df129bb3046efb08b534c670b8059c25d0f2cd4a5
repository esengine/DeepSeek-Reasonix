import { isDeepKey } from "../_internal/isDeepKey.mjs";
import { toPath } from "../util/toPath.mjs";
import { isIndex } from "../_internal/isIndex.mjs";
import { isArguments } from "../predicate/isArguments.mjs";
//#region src/compat/object/has.ts
/**
* Checks if a given path exists within an object.
*
* You can provide the path as a single property key, an array of property keys,
* or a string representing a deep path.
*
* If the path is an index and the object is an array or an arguments object, the function will verify
* if the index is valid and within the bounds of the array or arguments object, even if the array or
* arguments object is sparse (i.e., not all indexes are defined).
*
* @param object - The object to query.
* @param path - The path to check. This can be a single property key,
*        an array of property keys, or a string representing a deep path.
* @returns Returns `true` if the path exists in the object, `false` otherwise.
*
* @example
*
* const obj = { a: { b: { c: 3 } } };
*
* has(obj, 'a'); // true
* has(obj, ['a', 'b']); // true
* has(obj, ['a', 'b', 'c']); // true
* has(obj, 'a.b.c'); // true
* has(obj, 'a.b.d'); // false
* has(obj, ['a', 'b', 'c', 'd']); // false
* has([], 0); // false
* has([1, 2, 3], 2); // true
* has([1, 2, 3], 5); // false
*/
function has(object, path) {
	let resolvedPath;
	if (Array.isArray(path)) resolvedPath = path;
	else if (typeof path === "string" && isDeepKey(path) && object?.[path] == null) resolvedPath = toPath(path);
	else resolvedPath = [path];
	if (resolvedPath.length === 0) return false;
	let current = object;
	for (let i = 0; i < resolvedPath.length; i++) {
		const key = resolvedPath[i];
		if (current == null || !Object.hasOwn(current, key)) {
			if (!((Array.isArray(current) || isArguments(current)) && isIndex(key) && key < current.length)) return false;
		}
		current = current[key];
	}
	return true;
}
//#endregion
export { has };
