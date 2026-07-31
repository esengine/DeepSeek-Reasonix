import { isPlainObject } from "../predicate/isPlainObject.mjs";
//#region src/compat/object/defaultsDeep.ts
/**
* Recursively assigns default values to an `object`, ensuring that certain properties do not remain `undefined`.
* It sets default values for properties that are either `undefined` or inherited from `Object.prototype`.
*
* Similar to `defaults` but recursively applies default values to nested objects.
* Source objects are applied in order from left to right, and once a property has been assigned a value,
* any subsequent values for that property will be ignored.
*
* Note: This function modifies the first argument, `object`.
*
* @template T - The type of the object being processed.
* @param target - The target object that will receive default values.
* @param sources - One or more source objects that specify default values to apply.
* @returns The `object` that has been updated with default values from all sources, recursively merging nested objects.
*
* @example
* defaultsDeep({ a: { b: 2 } }, { a: { b: 3, c: 3 }, d: 4 }); // { a: { b: 2, c: 3 }, d: 4 }
* defaultsDeep({ a: { b: undefined } }, { a: { b: 1 } }); // { a: { b: 1 } }
* defaultsDeep({ a: null }, { a: { b: 1 } }); // { a: null }
*/
function defaultsDeep(target, ...sources) {
	target = Object(target);
	for (let i = 0; i < sources.length; i++) {
		const source = sources[i];
		if (source != null) defaultsDeepRecursive(target, source, /* @__PURE__ */ new WeakMap());
	}
	return target;
}
function defaultsDeepRecursive(target, source, stack) {
	for (const key in source) {
		const sourceValue = source[key];
		const targetValue = target[key];
		if (targetValue === void 0 || !Object.hasOwn(target, key)) {
			target[key] = handleMissingProperty(sourceValue, stack);
			continue;
		}
		if (stack.get(sourceValue) === targetValue) continue;
		handleExistingProperty(targetValue, sourceValue, stack);
	}
}
function handleMissingProperty(sourceValue, stack) {
	if (stack.has(sourceValue)) return stack.get(sourceValue);
	if (isPlainObject(sourceValue)) {
		const newObj = {};
		stack.set(sourceValue, newObj);
		defaultsDeepRecursive(newObj, sourceValue, stack);
		return newObj;
	}
	return sourceValue;
}
function handleExistingProperty(targetValue, sourceValue, stack) {
	if (isPlainObject(targetValue) && isPlainObject(sourceValue)) {
		stack.set(sourceValue, targetValue);
		defaultsDeepRecursive(targetValue, sourceValue, stack);
		return;
	}
	if (Array.isArray(targetValue) && Array.isArray(sourceValue)) {
		stack.set(sourceValue, targetValue);
		mergeArrays(targetValue, sourceValue, stack);
	}
}
function mergeArrays(targetArray, sourceArray, stack) {
	const minLength = Math.min(sourceArray.length, targetArray.length);
	for (let i = 0; i < minLength; i++) if (isPlainObject(targetArray[i]) && isPlainObject(sourceArray[i])) defaultsDeepRecursive(targetArray[i], sourceArray[i], stack);
	for (let i = minLength; i < sourceArray.length; i++) targetArray.push(sourceArray[i]);
}
//#endregion
export { defaultsDeep };
