const require_isFunction = require("../../predicate/isFunction.js");
const require_isNil = require("../../predicate/isNil.js");
require("../../predicate/index.js");
const require_isArrayLike = require("../predicate/isArrayLike.js");
const require_get = require("../object/get.js");
//#region src/compat/array/invokeMap.ts
/**
* Invokes the method at path of each element in collection.
*
* @template T, R
* @param collection - The collection to iterate over.
* @param path - The path of the method to invoke or the method to invoke.
* @param args - The arguments to invoke each method with.
* @returns Returns the array of results.
*
* @example
* invokeMap([[5, 1, 7], [3, 2, 1]], 'sort');
* // => [[1, 5, 7], [1, 2, 3]]
*/
function invokeMap(collection, path, ...args) {
	if (require_isNil.isNil(collection)) return [];
	const values = require_isArrayLike.isArrayLike(collection) ? Array.from(collection) : Object.values(collection);
	const result = [];
	for (let i = 0; i < values.length; i++) {
		const value = values[i];
		if (require_isFunction.isFunction(path)) {
			result.push(path.apply(value, args));
			continue;
		}
		const method = require_get.get(value, path);
		let thisContext = value;
		if (Array.isArray(path)) {
			const pathExceptLast = path.slice(0, -1);
			if (pathExceptLast.length > 0) thisContext = require_get.get(value, pathExceptLast);
		} else if (typeof path === "string" && path.includes(".")) {
			const pathExceptLast = path.split(".").slice(0, -1).join(".");
			thisContext = require_get.get(value, pathExceptLast);
		}
		result.push(method == null ? void 0 : method.apply(thisContext, args));
	}
	return result;
}
//#endregion
exports.invokeMap = invokeMap;
