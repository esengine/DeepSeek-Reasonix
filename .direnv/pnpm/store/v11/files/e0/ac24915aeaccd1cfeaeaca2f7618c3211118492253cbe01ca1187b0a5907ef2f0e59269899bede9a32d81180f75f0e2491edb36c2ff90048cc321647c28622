const require_isUnsafeProperty = require("../../_internal/isUnsafeProperty.js");
const require_isDeepKey = require("../_internal/isDeepKey.js");
const require_toKey = require("../_internal/toKey.js");
const require_toPath = require("../util/toPath.js");
//#region src/compat/object/get.ts
/**
* Retrieves the value at a given path from an object. If the resolved value is undefined, the defaultValue is returned instead.
*
* @param object - The object to query.
* @param path - The path of the property to get.
* @param [defaultValue] - The value returned if the resolved value is undefined.
* @returns Returns the resolved value.
*
* @example
* const object = { a: { b: { c: 1 } } };
* get(object, 'a.b.c');
* // => 1
*
* get(object, ['a', 'b', 'c']);
* // => 1
*
* get(object, 'a.b.d', 'default');
* // => 'default'
*/
function get(object, path, defaultValue) {
	if (object == null) return defaultValue;
	switch (typeof path) {
		case "string": {
			if (require_isUnsafeProperty.isUnsafeProperty(path)) return defaultValue;
			const result = object[path];
			if (result === void 0) if (require_isDeepKey.isDeepKey(path) && !Object.hasOwn(object, path)) return get(object, require_toPath.toPath(path), defaultValue);
			else return defaultValue;
			return result;
		}
		case "number":
		case "symbol": {
			if (typeof path === "number") path = require_toKey.toKey(path);
			const result = object[path];
			if (result === void 0) return defaultValue;
			return result;
		}
		default: {
			if (Array.isArray(path)) return getWithPath(object, path, defaultValue);
			if (Object.is(path?.valueOf(), -0)) path = "-0";
			else path = String(path);
			if (require_isUnsafeProperty.isUnsafeProperty(path)) return defaultValue;
			const result = object[path];
			if (result === void 0) return defaultValue;
			return result;
		}
	}
}
function getWithPath(object, path, defaultValue) {
	if (path.length === 0) return defaultValue;
	let current = object;
	for (let index = 0; index < path.length; index++) {
		if (current == null) return defaultValue;
		if (require_isUnsafeProperty.isUnsafeProperty(path[index])) return defaultValue;
		current = current[path[index]];
	}
	if (current === void 0) return defaultValue;
	return current;
}
//#endregion
exports.get = get;
