//#region src/math/sumBy.ts
/**
* Calculates the sum of an array of numbers when applying
* the `getValue` function to each element.
*
* If the array is empty, this function returns `0`.
*
* @template T - The type of elements in the array.
* @param items - An array to calculate the sum.
* @param getValue - A function that selects a numeric value from each element.
*   It receives the element and its zero‑based index in the array.
* @returns The sum of all the numbers as determined by the `getValue` function.
*
* @example
* sumBy([{ a: 1 }, { a: 2 }, { a: 3 }], (x, i) => x.a * i); // Returns: 8
* sumBy([], () => 1); // Returns: 0
*/
function sumBy(items, getValue) {
	let result = 0;
	for (let i = 0; i < items.length; i++) result += getValue(items[i], i);
	return result;
}
//#endregion
exports.sumBy = sumBy;
