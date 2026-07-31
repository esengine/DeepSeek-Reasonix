const require_isPlainObject = require("../predicate/isPlainObject.js");
const require_isUnsafeProperty = require("../_internal/isUnsafeProperty.js");
//#region src/object/merge.ts
/**
* Merges the properties of the source object into the target object.
*
* This function performs a deep merge, meaning nested objects and arrays are merged recursively.
* If a property in the source object is an array or an object and the corresponding property in the target object is also an array or object, they will be merged.
* If a property in the source object is undefined, it will not overwrite a defined property in the target object.
*
* Note that this function mutates the target object.
*
* @param target - The target object into which the source object properties will be merged. This object is modified in place.
* @param source - The source object whose properties will be merged into the target object.
* @returns The updated target object with properties from the source object merged in.
*
* @template T - Type of the target object.
* @template S - Type of the source object.
*
* @example
* const target = { a: 1, b: { x: 1, y: 2 } };
* const source = { b: { y: 3, z: 4 }, c: 5 };
*
* const result = merge(target, source);
* console.log(result);
* // Output: { a: 1, b: { x: 1, y: 3, z: 4 }, c: 5 }
*
* @example
* const target = { a: [1, 2], b: { x: 1 } };
* const source = { a: [3], b: { y: 2 } };
*
* const result = merge(target, source);
* console.log(result);
* // Output: { a: [3, 2], b: { x: 1, y: 2 } }
*
* @example
* const target = { a: null };
* const source = { a: [1, 2, 3] };
*
* const result = merge(target, source);
* console.log(result);
* // Output: { a: [1, 2, 3] }
*/
function merge(target, source) {
	const sourceKeys = Object.keys(source);
	for (let i = 0; i < sourceKeys.length; i++) {
		const key = sourceKeys[i];
		if (require_isUnsafeProperty.isUnsafeProperty(key)) continue;
		const sourceValue = source[key];
		const targetValue = target[key];
		if (isMergeableValue(sourceValue) && isMergeableValue(targetValue)) target[key] = merge(targetValue, sourceValue);
		else if (Array.isArray(sourceValue)) target[key] = merge([], sourceValue);
		else if (require_isPlainObject.isPlainObject(sourceValue)) target[key] = merge({}, sourceValue);
		else if (targetValue === void 0 || sourceValue !== void 0) target[key] = sourceValue;
	}
	return target;
}
function isMergeableValue(value) {
	return require_isPlainObject.isPlainObject(value) || Array.isArray(value);
}
//#endregion
exports.merge = merge;
