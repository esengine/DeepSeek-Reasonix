import { toKey } from "../_internal/toKey.mjs";
import { toString } from "../util/toString.mjs";
import { toPath } from "../util/toPath.mjs";
import { isKey } from "../_internal/isKey.mjs";
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
	if (isKey(path, object)) path = [path];
	else if (!Array.isArray(path)) path = toPath(toString(path));
	const pathLength = Math.max(path.length, 1);
	for (let index = 0; index < pathLength; index++) {
		const value = object == null ? void 0 : object[toKey(path[index])];
		if (value === void 0) return typeof defaultValue === "function" ? defaultValue.call(object) : defaultValue;
		object = typeof value === "function" ? value.call(object) : value;
	}
	return object;
}
//#endregion
export { result };
