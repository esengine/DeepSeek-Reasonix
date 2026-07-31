const require_clone = require("./clone.js");
const require_cloneDeep = require("./cloneDeep.js");
const require_isPlainObject = require("../predicate/isPlainObject.js");
const require_mergeWith = require("./mergeWith.js");
//#region src/object/toMerged.ts
/**
* Merges the properties of the source object into a deep clone of the target object.
* Unlike `merge`, This function does not modify the original target object.
*
* This function performs a deep merge, meaning nested objects and arrays are merged recursively.
*
* - If a property in the source object is an array or object and the corresponding property in the target object is also an array or object, they will be merged.
* - If a property in the source object is undefined, it will not overwrite a defined property in the target object.
*
* Note that this function does not mutate the target object.
*
* @param target - The target object to be cloned and merged into. This object is not modified directly.
* @param source - The source object whose properties will be merged into the cloned target object.
* @returns A new object with properties from the source object merged into a deep clone of the target object.
*
* @template T - Type of the target object.
* @template S - Type of the source object.
*
* @example
* const target = { a: 1, b: { x: 1, y: 2 } };
* const source = { b: { y: 3, z: 4 }, c: 5 };
*
* const result = toMerged(target, source);
* console.log(result);
* // Output: { a: 1, b: { x: 1, y: 3, z: 4 }, c: 5 }
*
* @example
* const target = { a: [1, 2], b: { x: 1 } };
* const source = { a: [3], b: { y: 2 } };
*
* const result = toMerged(target, source);
* console.log(result);
* // Output: { a: [3, 2], b: { x: 1, y: 2 } }
*
* @example
* const target = { a: null };
* const source = { a: [1, 2, 3] };
*
* const result = toMerged(target, source);
* console.log(result);
* // Output: { a: [1, 2, 3] }
*/
function toMerged(target, source) {
	return require_mergeWith.mergeWith(require_cloneDeep.cloneDeep(target), source, function mergeRecursively(targetValue, sourceValue) {
		if (Array.isArray(sourceValue)) if (Array.isArray(targetValue)) return require_mergeWith.mergeWith(require_clone.clone(targetValue), sourceValue, mergeRecursively);
		else return require_mergeWith.mergeWith([], sourceValue, mergeRecursively);
		else if (require_isPlainObject.isPlainObject(sourceValue)) if (require_isPlainObject.isPlainObject(targetValue)) return require_mergeWith.mergeWith(require_clone.clone(targetValue), sourceValue, mergeRecursively);
		else return require_mergeWith.mergeWith({}, sourceValue, mergeRecursively);
	});
}
//#endregion
exports.toMerged = toMerged;
