const require_isUnsafeProperty = require("../../_internal/isUnsafeProperty.js");
const require_isDeepKey = require("../_internal/isDeepKey.js");
const require_toKey = require("../_internal/toKey.js");
const require_toPath = require("../util/toPath.js");
const require_get = require("./get.js");
//#region src/compat/object/unset.ts
/**
* Removes the property at the given path of the object.
*
* @param obj - The object to modify.
* @param path - The path of the property to unset.
* @returns Returns true if the property is deleted, else false.
*
* @example
* const obj = { a: { b: { c: 42 } } };
* unset(obj, 'a.b.c'); // true
* console.log(obj); // { a: { b: {} } }
*
* @example
* const obj = { a: { b: { c: 42 } } };
* unset(obj, ['a', 'b', 'c']); // true
* console.log(obj); // { a: { b: {} } }
*/
function unset(obj, path) {
	if (obj == null) return true;
	switch (typeof path) {
		case "symbol":
		case "number":
		case "object":
			if (Array.isArray(path)) return unsetWithPath(obj, path);
			if (typeof path === "number") path = require_toKey.toKey(path);
			else if (typeof path === "object") if (Object.is(path?.valueOf(), -0)) path = "-0";
			else path = String(path);
			if (require_isUnsafeProperty.isUnsafeProperty(path)) return false;
			if (obj?.[path] === void 0) return true;
			try {
				delete obj[path];
				return true;
			} catch {
				return false;
			}
		case "string":
			if (obj?.[path] === void 0 && require_isDeepKey.isDeepKey(path)) return unsetWithPath(obj, require_toPath.toPath(path));
			if (require_isUnsafeProperty.isUnsafeProperty(path)) return false;
			try {
				delete obj[path];
				return true;
			} catch {
				return false;
			}
	}
}
function unsetWithPath(obj, path) {
	const parent = path.length === 1 ? obj : require_get.get(obj, path.slice(0, -1));
	const lastKey = path[path.length - 1];
	if (parent?.[lastKey] === void 0) return true;
	if (require_isUnsafeProperty.isUnsafeProperty(lastKey)) return false;
	try {
		delete parent[lastKey];
		return true;
	} catch {
		return false;
	}
}
//#endregion
exports.unset = unset;
