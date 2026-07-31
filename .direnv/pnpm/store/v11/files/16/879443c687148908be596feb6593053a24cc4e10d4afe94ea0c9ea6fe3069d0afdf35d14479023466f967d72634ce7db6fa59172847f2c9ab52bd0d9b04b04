import { isUnsafeProperty } from "../../_internal/isUnsafeProperty.mjs";
import { toKey } from "../_internal/toKey.mjs";
import { toPath } from "../util/toPath.mjs";
import { get } from "./get.mjs";
import { isObject } from "../predicate/isObject.mjs";
import { isIndex } from "../_internal/isIndex.mjs";
import { isKey } from "../_internal/isKey.mjs";
import { assignValue } from "../_internal/assignValue.mjs";
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
	if (obj == null && !isObject(obj)) return obj;
	let resolvedPath;
	if (isKey(path, obj)) resolvedPath = [path];
	else if (Array.isArray(path)) resolvedPath = path;
	else resolvedPath = toPath(path);
	const updateValue = updater(get(obj, resolvedPath));
	let current = obj;
	for (let i = 0; i < resolvedPath.length && current != null; i++) {
		const key = toKey(resolvedPath[i]);
		if (isUnsafeProperty(key)) continue;
		let newValue;
		if (i === resolvedPath.length - 1) newValue = updateValue;
		else {
			const objValue = current[key];
			const customizerResult = customizer?.(objValue, key, obj);
			newValue = customizerResult !== void 0 ? customizerResult : isObject(objValue) ? objValue : isIndex(resolvedPath[i + 1]) ? [] : {};
		}
		assignValue(current, key, newValue);
		current = current[key];
	}
	return obj;
}
//#endregion
export { updateWith };
