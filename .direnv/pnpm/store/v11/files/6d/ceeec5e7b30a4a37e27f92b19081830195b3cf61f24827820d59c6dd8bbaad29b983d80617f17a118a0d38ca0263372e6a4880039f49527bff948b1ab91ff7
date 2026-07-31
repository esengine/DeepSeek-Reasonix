//#region src/array/uniqBy.ts
/**
* Returns a new array containing only the unique elements from the original array,
* based on the values returned by the mapper function.
*
* When duplicates are found, the first occurrence is kept and the rest are discarded.
*
* @template T - The type of elements in the array.
* @template U - The type of mapped elements.
* @param arr - The array to process.
* @param mapper - The function used to convert the array elements.
* @returns A new array containing only the unique elements from the original array, based on the values returned by the mapper function.
*
* @example
* ```ts
* uniqBy([1.2, 1.5, 2.1, 3.2, 5.7, 5.3, 7.19], Math.floor);
* // [1.2, 2.1, 3.2, 5.7, 7.19]
* ```
*
* @example
* const array = [
*   { category: 'fruit', name: 'apple' },
*   { category: 'fruit', name: 'banana' },
*   { category: 'vegetable', name: 'carrot' },
* ];
* uniqBy(array, item => item.category).length
* // 2
* ```
*/
function uniqBy(arr, mapper) {
	const map = /* @__PURE__ */ new Map();
	for (let i = 0; i < arr.length; i++) {
		const item = arr[i];
		const key = mapper(item, i, arr);
		if (!map.has(key)) map.set(key, item);
	}
	return Array.from(map.values());
}
//#endregion
exports.uniqBy = uniqBy;
