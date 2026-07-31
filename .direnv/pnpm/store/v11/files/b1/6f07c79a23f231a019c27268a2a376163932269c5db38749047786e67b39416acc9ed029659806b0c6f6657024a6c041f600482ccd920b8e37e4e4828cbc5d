const require_getTag = require("../_internal/getTag.js");
const require_tags = require("../_internal/tags.js");
const require_cloneDeepWith = require("../../object/cloneDeepWith.js");
//#region src/compat/object/cloneDeepWith.ts
/**
* Creates a deep clone of the given object using a customizer function.
*
* @template T - The type of the object.
* @param obj - The object to clone.
* @param [cloneValue] - A function to customize the cloning process.
* @returns A deep clone of the given object.
*
* @example
* // Clone a primitive value
* const num = 29;
* const clonedNum = cloneDeepWith(num);
* console.log(clonedNum); // 29
* console.log(clonedNum === num); // true
*
* @example
* // Clone an object with a customizer
* const obj = { a: 1, b: 2 };
* const clonedObj = cloneDeepWith(obj, (value) => {
*   if (typeof value === 'number') {
*     return value * 2; // Double the number
*   }
* });
* console.log(clonedObj); // { a: 2, b: 4 }
* console.log(clonedObj === obj); // false
*
* @example
* // Clone an array with a customizer
* const arr = [1, 2, 3];
* const clonedArr = cloneDeepWith(arr, (value) => {
*   return value + 1; // Increment each value
* });
* console.log(clonedArr); // [2, 3, 4]
* console.log(clonedArr === arr); // false
*/
function cloneDeepWith(obj, customizer) {
	return require_cloneDeepWith.cloneDeepWith(obj, (value, key, object, stack) => {
		const cloned = customizer?.(value, key, object, stack);
		if (cloned !== void 0) return cloned;
		if (typeof obj !== "object") return;
		if (require_getTag.getTag(obj) === "[object Object]" && typeof obj.constructor !== "function") {
			const result = {};
			stack.set(obj, result);
			require_cloneDeepWith.copyProperties(result, obj, object, stack);
			return result;
		}
		switch (Object.prototype.toString.call(obj)) {
			case require_tags.numberTag:
			case require_tags.stringTag:
			case require_tags.booleanTag: {
				const result = new obj.constructor(obj?.valueOf());
				require_cloneDeepWith.copyProperties(result, obj);
				return result;
			}
			case require_tags.argumentsTag: {
				const result = {};
				require_cloneDeepWith.copyProperties(result, obj);
				result.length = obj.length;
				result[Symbol.iterator] = obj[Symbol.iterator];
				return result;
			}
			default: return;
		}
	});
}
//#endregion
exports.cloneDeepWith = cloneDeepWith;
