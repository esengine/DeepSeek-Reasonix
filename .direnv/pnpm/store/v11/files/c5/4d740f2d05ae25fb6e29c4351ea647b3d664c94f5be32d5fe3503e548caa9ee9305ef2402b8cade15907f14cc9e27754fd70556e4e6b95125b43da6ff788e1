//#region src/compat/math/min.ts
/**
* Finds the element in an array that has the minimum value.
*
* @template T - The type of elements in the array.
* @param [items] - The array of elements to search. Defaults to an empty array.
* @returns The element with the minimum value, or undefined if the array is empty.
*/
function min(items) {
	if (!items || items.length === 0) return;
	let minResult = void 0;
	for (let i = 0; i < items.length; i++) {
		const current = items[i];
		if (current == null || Number.isNaN(current) || typeof current === "symbol") continue;
		if (minResult === void 0 || current < minResult) minResult = current;
	}
	return minResult;
}
//#endregion
export { min };
