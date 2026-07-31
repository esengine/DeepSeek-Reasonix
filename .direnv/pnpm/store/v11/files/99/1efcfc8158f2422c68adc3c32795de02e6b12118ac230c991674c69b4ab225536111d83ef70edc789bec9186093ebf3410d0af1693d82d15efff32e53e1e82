const require_isPlainObject = require("../predicate/isPlainObject.js");
//#region src/object/flattenObject.ts
/**
* Flattens a nested object into a single level object with delimiter-separated keys.
*
* @param object - The object to flatten.
* @param [options.delimiter='.'] - The delimiter to use between nested keys.
* @returns The flattened object.
*
* @example
* const nestedObject = {
*   a: {
*     b: {
*       c: 1
*     }
*   },
*   d: [2, 3]
* };
*
* const flattened = flattenObject(nestedObject);
* console.log(flattened);
* // Output:
* // {
* //   'a.b.c': 1,
* //   'd.0': 2,
* //   'd.1': 3
* // }
*/
function flattenObject(object, { delimiter = "." } = {}) {
	return flattenObjectImpl(object, "", delimiter);
}
function flattenObjectImpl(object, prefix, delimiter) {
	const result = {};
	const keys = Object.keys(object);
	for (let i = 0; i < keys.length; i++) {
		const key = keys[i];
		const value = object[key];
		const prefixedKey = prefix ? `${prefix}${delimiter}${key}` : key;
		if (require_isPlainObject.isPlainObject(value) && Object.keys(value).length > 0) {
			Object.assign(result, flattenObjectImpl(value, prefixedKey, delimiter));
			continue;
		}
		if (Array.isArray(value) && value.length > 0) {
			Object.assign(result, flattenObjectImpl(value, prefixedKey, delimiter));
			continue;
		}
		result[prefixedKey] = value;
	}
	return result;
}
//#endregion
exports.flattenObject = flattenObject;
