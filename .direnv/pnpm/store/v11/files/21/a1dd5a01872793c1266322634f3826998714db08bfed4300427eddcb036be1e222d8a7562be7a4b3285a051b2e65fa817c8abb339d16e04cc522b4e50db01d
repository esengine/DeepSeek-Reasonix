const require_isPlainObject = require("./isPlainObject.js");
//#region src/predicate/isJSONValue.ts
/**
* The functions isJSONValue, isJSONArray, and isJSONObject are grouped in this file
* to prevent any circular dependency issues.
*/
/**
* Checks if a given value is a valid JSON value.
*
* A valid JSON value can be:
* - null
* - a JSON object (an object with string keys and valid JSON values)
* - a JSON array (an array of valid JSON values)
* - a string
* - a number
* - a boolean
*
* @param value - The value to check.
* @returns True if the value is a valid JSON value, otherwise false.
*
* @example
* console.log(isJSONValue(null)); // true
* console.log(isJSONValue({ key: "value" })); // true
* console.log(isJSONValue([1, 2, 3])); // true
* console.log(isJSONValue("Hello")); // true
* console.log(isJSONValue(42)); // true
* console.log(isJSONValue(true)); // true
* console.log(isJSONValue(undefined)); // false
* console.log(isJSONValue(() => {})); // false
*/
function isJSONValue(value) {
	switch (typeof value) {
		case "object": return value === null || isJSONArray(value) || isJSONObject(value);
		case "string":
		case "number":
		case "boolean": return true;
		default: return false;
	}
}
/**
* Checks if a given value is a valid JSON array.
*
* A valid JSON array is defined as an array where all items are valid JSON values.
*
* @param value - The value to check.
* @returns True if the value is a valid JSON array, otherwise false.
*
* @example
* console.log(isJSONArray([1, 2, 3])); // true
* console.log(isJSONArray(["string", null, true])); // true
* console.log(isJSONArray([1, 2, () => {}])); // false
* console.log(isJSONArray("not an array")); // false
*/
function isJSONArray(value) {
	if (!Array.isArray(value)) return false;
	return value.every((item) => isJSONValue(item));
}
/**
* Checks if a value is a JSON object.
*
* A valid JSON object is defined as an object with string keys and valid JSON values.
*
* @param obj The value to check.
* @returns True if `obj` is a JSON object, false otherwise.
*
* @example
* isJSONObject({ nested: { boolean: true, array: [1, 2, 3], string: 'test', null: null } }); // true
* isJSONObject({ regexp: /test/ }); // false
* isJSONObject(123); // false
*/
function isJSONObject(obj) {
	if (!require_isPlainObject.isPlainObject(obj)) return false;
	const keys = Reflect.ownKeys(obj);
	for (let i = 0; i < keys.length; i++) {
		const key = keys[i];
		const value = obj[key];
		if (typeof key !== "string") return false;
		if (!isJSONValue(value)) return false;
	}
	return true;
}
//#endregion
exports.isJSONArray = isJSONArray;
exports.isJSONObject = isJSONObject;
exports.isJSONValue = isJSONValue;
