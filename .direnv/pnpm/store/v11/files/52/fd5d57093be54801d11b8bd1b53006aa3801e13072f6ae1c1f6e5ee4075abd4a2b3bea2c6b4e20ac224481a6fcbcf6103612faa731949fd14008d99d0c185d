const require_toKey = require("../_internal/toKey.js");
const require_toString = require("../util/toString.js");
const require_toPath = require("../util/toPath.js");
const require_isKey = require("../_internal/isKey.js");
//#region src/compat/object/result.ts
/**
* Retrieves the value at a given path of an object.
* If the resolved value is a function, it is invoked with the object as its `this` context.
* If the value is `undefined`, the `defaultValue` is returned.
*
* @template T - The type of object.
* @template R - The type of the value to return.
* @param object - The object to query.
* @param path - The path of the property to get.
* @param [defaultValue] - The value returned if the resolved value is `undefined`.
* @returns Returns the resolved value.
*
* @example
* const obj = { a: { b: { c: 3 } } };
* result(obj, 'a.b.c');
* // => 3
*
* @example
* const obj = { a: () => 5 };
* result(obj, 'a');
* // => 5 (calls the function `a` and returns its result)
*
* @example
* const obj = { a: { b: null } };
* result(obj, 'a.b.c', 'default');
* // => 'default'
*
* @example
* const obj = { a: { b: { c: 3 } } };
* result(obj, 'a.b.d', () => 'default');
* // => 'default'
*/
function result(object, path, defaultValue) {
	if (require_isKey.isKey(path, object)) path = [path];
	else if (!Array.isArray(path)) path = require_toPath.toPath(require_toString.toString(path));
	const pathLength = Math.max(path.length, 1);
	for (let index = 0; index < pathLength; index++) {
		const value = object == null ? void 0 : object[require_toKey.toKey(path[index])];
		if (value === void 0) return typeof defaultValue === "function" ? defaultValue.call(object) : defaultValue;
		object = typeof value === "function" ? value.call(object) : value;
	}
	return object;
}
//#endregion
exports.result = result;
