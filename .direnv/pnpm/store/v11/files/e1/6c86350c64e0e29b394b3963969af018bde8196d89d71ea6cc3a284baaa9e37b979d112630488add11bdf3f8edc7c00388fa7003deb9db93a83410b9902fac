const require_isArrayLike = require("../predicate/isArrayLike.js");
//#region src/compat/array/flatten.ts
function flatten(value, depth = 1) {
	const result = [];
	const flooredDepth = Math.floor(depth);
	if (!require_isArrayLike.isArrayLike(value)) return result;
	const recursive = (arr, currentDepth) => {
		for (let i = 0; i < arr.length; i++) {
			const item = arr[i];
			if (currentDepth < flooredDepth && (Array.isArray(item) || Boolean(item?.[Symbol.isConcatSpreadable]) || item !== null && typeof item === "object" && Object.prototype.toString.call(item) === "[object Arguments]")) if (Array.isArray(item)) recursive(item, currentDepth + 1);
			else recursive(Array.from(item), currentDepth + 1);
			else result.push(item);
		}
	};
	recursive(Array.from(value), 0);
	return result;
}
//#endregion
exports.flatten = flatten;
