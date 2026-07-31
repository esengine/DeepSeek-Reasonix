const require_isPrimitive = require("../../predicate/isPrimitive.js");
const require_isEqualsSameValueZero = require("../../_internal/isEqualsSameValueZero.js");
require("../util/eq.js");
const require_isObject = require("./isObject.js");
//#region src/compat/predicate/isMatchWith.ts
/**
* Performs a deep comparison between a target value and a source pattern to determine if they match,
* using a custom comparison function for fine-grained control over the matching logic.
*
* This function recursively traverses both values, calling the custom compare function for each
* property/element pair. If the compare function returns a boolean, that result is used directly.
* If it returns undefined, the default matching behavior continues recursively.
*
* The matching behavior varies by data type:
* - **Objects**: Matches if all properties in the source exist in the target and match
* - **Arrays**: Matches if all elements in the source array can be found in the target array (order-independent)
* - **Maps**: Matches if all key-value pairs in the source Map exist and match in the target Map
* - **Sets**: Matches if all elements in the source Set can be found in the target Set
* - **Functions**: Matches using strict equality, or object comparison if the function has properties
* - **Primitives**: Matches using strict equality
*
* Special cases:
* - Empty objects, arrays, Maps, and Sets always match any target
* - `null` and `undefined` source values have specific matching rules
* - Circular references are handled using an internal stack to prevent infinite recursion
*
* @param target - The value to be tested for matching
* @param source - The pattern/template to match against
* @param [compare] - Optional custom comparison function that receives:
*   - `objValue` - The value from the target at the current path
*   - `srcValue` - The value from the source at the current path
*   - `key` - The property key or array index being compared
*   - `object` - The parent object/array from the target
*   - `source` - The parent object/array from the source
*   - `stack` - Internal Map used for circular reference detection
*   Should return `true` for a match, `false` for no match, or `undefined` to continue with default behavior
*
* @returns `true` if the target matches the source pattern, `false` otherwise
*
* @example
* // Basic matching without custom comparator
* isMatchWith({ a: 1, b: 2 }, { a: 1 }); // true
* isMatchWith([1, 2, 3], [1, 3]); // true
*
* @example
* // Custom comparison for case-insensitive string matching
* const caseInsensitiveCompare = (objVal, srcVal) => {
*   if (typeof objVal === 'string' && typeof srcVal === 'string') {
*     return objVal.toLowerCase() === srcVal.toLowerCase();
*   }
*   return undefined; // Use default behavior for non-strings
* };
*
* isMatchWith(
*   { name: 'JOHN', age: 30 },
*   { name: 'john' },
*   caseInsensitiveCompare
* ); // true
*
* @example
* // Custom comparison for range matching
* const rangeCompare = (objVal, srcVal, key) => {
*   if (key === 'age' && typeof srcVal === 'object' && srcVal.min !== undefined) {
*     return objVal >= srcVal.min && objVal <= srcVal.max;
*   }
*   return undefined;
* };
*
* isMatchWith(
*   { name: 'John', age: 25 },
*   { age: { min: 18, max: 30 } },
*   rangeCompare
* ); // true
*/
function isMatchWith(target, source, compare) {
	if (typeof compare !== "function") return isMatchWith(target, source, () => void 0);
	return isMatchWithInternal(target, source, function doesMatch(objValue, srcValue, key, object, source, stack) {
		const isEqual = compare(objValue, srcValue, key, object, source, stack);
		if (isEqual !== void 0) return Boolean(isEqual);
		return isMatchWithInternal(objValue, srcValue, doesMatch, stack, false);
	}, /* @__PURE__ */ new Map(), true);
}
function isMatchWithInternal(target, source, compare, stack, isRoot = false) {
	if (source === target) return true;
	switch (typeof source) {
		case "object": return isObjectMatch(target, source, compare, stack);
		case "function":
			if (Object.keys(source).length > 0) return isMatchWithInternal(target, { ...source }, compare, stack, isRoot);
			return require_isEqualsSameValueZero.isEqualsSameValueZero(target, source);
		default:
			if (!require_isObject.isObject(target)) return require_isEqualsSameValueZero.isEqualsSameValueZero(target, source);
			if (isRoot) {
				if (typeof source === "string") return source === "";
				return true;
			}
			return require_isEqualsSameValueZero.isEqualsSameValueZero(target, source);
	}
}
function isObjectMatch(target, source, compare, stack) {
	if (source == null) return true;
	if (Array.isArray(source)) return isArrayMatch(target, source, compare, stack);
	if (source instanceof Map) return isMapMatch(target, source, compare, stack);
	if (source instanceof Set) return isSetMatch(target, source, compare, stack);
	const keys = Object.keys(source);
	if (target == null || require_isPrimitive.isPrimitive(target)) return keys.length === 0;
	if (keys.length === 0) return true;
	if (stack?.has(source)) return stack.get(source) === target;
	stack?.set(source, target);
	try {
		for (let i = 0; i < keys.length; i++) {
			const key = keys[i];
			if (!require_isPrimitive.isPrimitive(target) && !(key in target)) return false;
			if (source[key] === void 0 && target[key] !== void 0) return false;
			if (source[key] === null && target[key] !== null) return false;
			if (!compare(target[key], source[key], key, target, source, stack)) return false;
		}
		return true;
	} finally {
		stack?.delete(source);
	}
}
function isMapMatch(target, source, compare, stack) {
	if (source.size === 0) return true;
	if (!(target instanceof Map)) return false;
	for (const [key, sourceValue] of source.entries()) if (compare(target.get(key), sourceValue, key, target, source, stack) === false) return false;
	return true;
}
function isArrayMatch(target, source, compare, stack) {
	if (source.length === 0) return true;
	if (!Array.isArray(target)) return false;
	const countedIndex = /* @__PURE__ */ new Set();
	for (let i = 0; i < source.length; i++) {
		const sourceItem = source[i];
		let found = false;
		for (let j = 0; j < target.length; j++) {
			if (countedIndex.has(j)) continue;
			const targetItem = target[j];
			let matches = false;
			if (compare(targetItem, sourceItem, i, target, source, stack)) matches = true;
			if (matches) {
				countedIndex.add(j);
				found = true;
				break;
			}
		}
		if (!found) return false;
	}
	return true;
}
function isSetMatch(target, source, compare, stack) {
	if (source.size === 0) return true;
	if (!(target instanceof Set)) return false;
	return isArrayMatch([...target], [...source], compare, stack);
}
//#endregion
exports.isMatchWith = isMatchWith;
