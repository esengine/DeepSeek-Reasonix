const require_isPrimitive = require("../../predicate/isPrimitive.js");
const require_clone = require("../../object/clone.js");
const require_getSymbols = require("../_internal/getSymbols.js");
const require_isBuffer = require("../../predicate/isBuffer.js");
const require_isUnsafeProperty = require("../../_internal/isUnsafeProperty.js");
const require_isPlainObject = require("../predicate/isPlainObject.js");
const require_cloneDeep = require("./cloneDeep.js");
const require_isArguments = require("../predicate/isArguments.js");
const require_isObjectLike = require("../predicate/isObjectLike.js");
const require_isArrayLikeObject = require("../predicate/isArrayLikeObject.js");
const require_isTypedArray = require("../predicate/isTypedArray.js");
//#region src/compat/object/mergeWith.ts
/**
* Merges the properties of one or more source objects into the target object using a customizer function.
*
* This function performs a deep merge, recursively merging nested objects and arrays.
* If a property in the source object is an array or object and the corresponding property in the target object is also an array or object, they will be merged.
* If a property in the source object is `undefined`, it will not overwrite a defined property in the target object.
*
* You can provide a custom `merge` function to control how properties are merged. The `merge` function is called for each property that is being merged and receives the following arguments:
*
* - `targetValue`: The current value of the property in the target object.
* - `sourceValue`: The value of the property in the source object.
* - `key`: The key of the property being merged.
* - `target`: The target object.
* - `source`: The source object.
* - `stack`: A `Map` used to keep track of objects that have already been processed to handle circular references.
*
* The `merge` function should return the value to be set in the target object. If it returns `undefined`, a default deep merge will be applied for arrays and objects.
*
* The function can handle multiple source objects and will merge them all into the target object.
*
* @param object - The target object into which the source object properties will be merged. This object is modified in place.
* @param otherArgs - Additional source objects to merge into the target object, including the custom `merge` function.
* @returns The updated target object with properties from the source object(s) merged in.
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
function mergeWith(object, ...otherArgs) {
	const sources = otherArgs.slice(0, -1);
	const merge = otherArgs[otherArgs.length - 1];
	let result = object;
	for (let i = 0; i < sources.length; i++) {
		const source = sources[i];
		result = mergeWithDeep(result, source, merge, /* @__PURE__ */ new Map());
	}
	return result;
}
function mergeWithDeep(target, source, merge, stack) {
	if (require_isPrimitive.isPrimitive(target)) target = Object(target);
	if (source == null || typeof source !== "object") return target;
	if (stack.has(source)) return require_clone.clone(stack.get(source));
	stack.set(source, target);
	if (Array.isArray(source)) {
		source = source.slice();
		for (let i = 0; i < source.length; i++) source[i] = source[i] ?? void 0;
	}
	const sourceKeys = [...Object.keys(source), ...require_getSymbols.getSymbols(source)];
	for (let i = 0; i < sourceKeys.length; i++) {
		const key = sourceKeys[i];
		if (require_isUnsafeProperty.isUnsafeProperty(key)) continue;
		let sourceValue = source[key];
		let targetValue = target[key];
		if (require_isArguments.isArguments(sourceValue)) sourceValue = { ...sourceValue };
		if (require_isArguments.isArguments(targetValue)) targetValue = { ...targetValue };
		if (require_isBuffer.isBuffer(sourceValue)) sourceValue = require_cloneDeep.cloneDeep(sourceValue);
		if (Array.isArray(sourceValue)) if (Array.isArray(targetValue)) {
			const cloned = [];
			const targetKeys = Reflect.ownKeys(targetValue);
			for (let i = 0; i < targetKeys.length; i++) {
				const targetKey = targetKeys[i];
				cloned[targetKey] = targetValue[targetKey];
			}
			targetValue = cloned;
		} else if (require_isArrayLikeObject.isArrayLikeObject(targetValue)) {
			const cloned = [];
			for (let i = 0; i < targetValue.length; i++) cloned[i] = targetValue[i];
			targetValue = cloned;
		} else targetValue = [];
		const merged = merge(targetValue, sourceValue, key, target, source, stack);
		if (merged !== void 0) target[key] = merged;
		else if (Array.isArray(sourceValue)) target[key] = mergeWithDeep(targetValue, sourceValue, merge, stack);
		else if (require_isObjectLike.isObjectLike(targetValue) && require_isObjectLike.isObjectLike(sourceValue) && (require_isPlainObject.isPlainObject(targetValue) || require_isPlainObject.isPlainObject(sourceValue) || require_isTypedArray.isTypedArray(targetValue) || require_isTypedArray.isTypedArray(sourceValue))) target[key] = mergeWithDeep(targetValue, sourceValue, merge, stack);
		else if (targetValue == null && require_isPlainObject.isPlainObject(sourceValue)) target[key] = mergeWithDeep({}, sourceValue, merge, stack);
		else if (targetValue == null && require_isTypedArray.isTypedArray(sourceValue)) target[key] = require_cloneDeep.cloneDeep(sourceValue);
		else if (targetValue === void 0 || sourceValue !== void 0) target[key] = sourceValue;
	}
	return target;
}
//#endregion
exports.mergeWith = mergeWith;
