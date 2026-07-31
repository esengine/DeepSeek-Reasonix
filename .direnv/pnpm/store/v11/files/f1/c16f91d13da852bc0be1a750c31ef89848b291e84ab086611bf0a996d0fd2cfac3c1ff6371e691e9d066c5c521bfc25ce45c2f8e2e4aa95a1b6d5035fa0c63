const require_isPlainObject = require("../predicate/isPlainObject.js");
const require_isUnsafeProperty = require("../_internal/isUnsafeProperty.js");
//#region src/object/mergeWith.ts
/**
* Merges the properties of the source object into the target object.
*
* You can provide a custom `merge` function to control how properties are merged. It should return the value to be set in the target object.
*
* If it returns `undefined`, a default deep merge will be applied for arrays and objects:
*
* - If a property in the source object is an array or an object and the corresponding property in the target object is also an array or object, they will be merged.
* - If a property in the source object is undefined, it will not overwrite a defined property in the target object.
*
* Note that this function mutates the target object.
*
* @param target - The target object into which the source object properties will be merged. This object is modified in place.
* @param source - The source object whose properties will be merged into the target object.
* @param merge - A custom merge function that defines how properties should be combined. It receives the following arguments:
*   - `targetValue`: The current value of the property in the target object.
*   - `sourceValue`: The value of the property in the source object.
*   - `key`: The key of the property being merged.
*   - `target`: The target object.
*   - `source`: The source object.
*
* @returns The updated target object with properties from the source object merged in.
*
* @template T - Type of the target object.
* @template S - Type of the source object.
*
* @example
* const target = { a: 1, b: 2 };
* const source = { b: 3, c: 4 };
*
* mergeWith(target, source, (targetValue, sourceValue) => {
*   if (typeof targetValue === 'number' && typeof sourceValue === 'number') {
*     return targetValue + sourceValue;
*   }
* });
* // Returns { a: 1, b: 5, c: 4 }
* @example
* const target = { a: [1], b: [2] };
* const source = { a: [3], b: [4] };
*
* const result = mergeWith(target, source, (objValue, srcValue) => {
*   if (Array.isArray(objValue)) {
*     return objValue.concat(srcValue);
*   }
* });
*
* expect(result).toEqual({ a: [1, 3], b: [2, 4] });
*/
function mergeWith(target, source, merge) {
	const sourceKeys = Object.keys(source);
	for (let i = 0; i < sourceKeys.length; i++) {
		const key = sourceKeys[i];
		if (require_isUnsafeProperty.isUnsafeProperty(key)) continue;
		const sourceValue = source[key];
		const targetValue = target[key];
		const merged = merge(targetValue, sourceValue, key, target, source);
		if (merged !== void 0) target[key] = merged;
		else if (Array.isArray(sourceValue)) if (Array.isArray(targetValue)) target[key] = mergeWith(targetValue, sourceValue, merge);
		else target[key] = mergeWith([], sourceValue, merge);
		else if (require_isPlainObject.isPlainObject(sourceValue)) if (require_isPlainObject.isPlainObject(targetValue)) target[key] = mergeWith(targetValue, sourceValue, merge);
		else target[key] = mergeWith({}, sourceValue, merge);
		else if (targetValue === void 0 || sourceValue !== void 0) target[key] = sourceValue;
	}
	return target;
}
//#endregion
exports.mergeWith = mergeWith;
