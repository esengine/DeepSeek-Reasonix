const require_isArrayLikeObject = require("../predicate/isArrayLikeObject.js");
const require_toArray = require("../util/toArray.js");
//#region src/compat/array/xor.ts
/**
* Computes the symmetric difference of the provided arrays, returning an array of elements
* that exist in only one of the arrays.
*
* @template T - The type of elements in the arrays.
* @param arrays - The arrays to compare.
* @returns An array containing the elements that are present in only one of the provided `arrays`.
*
* @example
* // Returns [1, 2, 5, 6]
* xor([1, 2, 3, 4], [3, 4, 5, 6]);
*
* @example
* // Returns ['a', 'c']
* xor(['a', 'b'], ['b', 'c']);
*
* @example
* // Returns [1, 3, 5]
* xor([1, 2], [2, 3], [4, 5]);
*/
function xor(...arrays) {
	const itemCounts = /* @__PURE__ */ new Map();
	for (let i = 0; i < arrays.length; i++) {
		const array = arrays[i];
		if (!require_isArrayLikeObject.isArrayLikeObject(array)) continue;
		const itemSet = new Set(require_toArray.toArray(array));
		for (const item of itemSet) if (!itemCounts.has(item)) itemCounts.set(item, 1);
		else itemCounts.set(item, itemCounts.get(item) + 1);
	}
	const result = [];
	for (const [item, count] of itemCounts) if (count === 1) result.push(item);
	return result;
}
//#endregion
exports.xor = xor;
