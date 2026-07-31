//#region src/compat/math/max.ts
/**
* Finds the element in an array that has the maximum value.
*
* @template T - The type of elements in the array.
* @param [items] - The array of elements to search. Defaults to an empty array.
* @returns The element with the maximum value, or undefined if the array is empty.
*/
function max(items) {
	if (!items || items.length === 0) return;
	let maxResult = void 0;
	for (let i = 0; i < items.length; i++) {
		const current = items[i];
		if (current == null || Number.isNaN(current) || typeof current === "symbol") continue;
		if (maxResult === void 0 || current > maxResult) maxResult = current;
	}
	return maxResult;
}
//#endregion
exports.max = max;
