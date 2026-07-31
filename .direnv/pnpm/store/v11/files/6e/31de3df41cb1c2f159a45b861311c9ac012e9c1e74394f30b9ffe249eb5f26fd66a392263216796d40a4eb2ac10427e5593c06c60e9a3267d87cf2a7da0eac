//#region src/array/minBy.ts
/**
* Finds the element in an array that has the minimum value when applying
* the `getValue` function to each element.
*
* @template T - The type of elements in the array.
* @param items The array of elements to search.
* @param getValue A function that selects a numeric value from each element.
* @returns The element with the minimum value as determined by the `getValue` function,
* or `undefined` if the array is empty.
* @example
* minBy([{ a: 1 }, { a: 2 }, { a: 3 }], x => x.a); // Returns: { a: 1 }
* minBy([], x => x.a); // Returns: undefined
* minBy([3, NaN, 1], x => x); // Returns: NaN (NaN propagates, matching Math.min)
* minBy(
*   [
*     { name: 'john', age: 30 },
*     { name: 'jane', age: 28 },
*     { name: 'joe', age: 26 },
*   ],
*   x => x.age
* ); // Returns: { name: 'joe', age: 26 }
*/
function minBy(items, getValue) {
	if (items.length === 0) return;
	let minElement = items[0];
	let min = Infinity;
	for (let i = 0; i < items.length; i++) {
		const element = items[i];
		const value = getValue(element, i, items);
		if (Number.isNaN(value)) return element;
		if (value < min) {
			min = value;
			minElement = element;
		}
	}
	return minElement;
}
//#endregion
exports.minBy = minBy;
