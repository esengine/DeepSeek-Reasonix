const require_isUnsafeProperty = require("../../_internal/isUnsafeProperty.js");
const require_toKey = require("../_internal/toKey.js");
const require_toPath = require("../util/toPath.js");
const require_get = require("./get.js");
const require_isObject = require("../predicate/isObject.js");
const require_isIndex = require("../_internal/isIndex.js");
const require_isKey = require("../_internal/isKey.js");
const require_assignValue = require("../_internal/assignValue.js");
//#region src/compat/object/updateWith.ts
/**
* Updates the value at the specified path of the given object using an updater function and a customizer.
* If any part of the path does not exist, it will be created.
*
* @template T - The type of the object.
* @template R - The type of the return value.
* @param obj - The object to modify.
* @param path - The path of the property to update.
* @param updater - The function to produce the updated value.
* @param customizer - The function to customize the update process.
* @returns The modified object.
*
* @example
* const object = { 'a': [{ 'b': { 'c': 3 } }] };
* updateWith(object, 'a[0].b.c', (n) => n * n);
* // => { 'a': [{ 'b': { 'c': 9 } }] }
*/
function updateWith(obj, path, updater, customizer) {
	if (obj == null && !require_isObject.isObject(obj)) return obj;
	let resolvedPath;
	if (require_isKey.isKey(path, obj)) resolvedPath = [path];
	else if (Array.isArray(path)) resolvedPath = path;
	else resolvedPath = require_toPath.toPath(path);
	const updateValue = updater(require_get.get(obj, resolvedPath));
	let current = obj;
	for (let i = 0; i < resolvedPath.length && current != null; i++) {
		const key = require_toKey.toKey(resolvedPath[i]);
		if (require_isUnsafeProperty.isUnsafeProperty(key)) continue;
		let newValue;
		if (i === resolvedPath.length - 1) newValue = updateValue;
		else {
			const objValue = current[key];
			const customizerResult = customizer?.(objValue, key, obj);
			newValue = customizerResult !== void 0 ? customizerResult : require_isObject.isObject(objValue) ? objValue : require_isIndex.isIndex(resolvedPath[i + 1]) ? [] : {};
		}
		require_assignValue.assignValue(current, key, newValue);
		current = current[key];
	}
	return obj;
}
//#endregion
exports.updateWith = updateWith;
