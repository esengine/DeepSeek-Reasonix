import { median } from "./median.mjs";
//#region src/math/medianBy.ts
/**
* Calculates the median of an array of elements when applying
* the `getValue` function to each element.
*
* The median is the middle value of a sorted array.
* If the array has an odd number of elements, the median is the middle value.
* If the array has an even number of elements, it returns the average of the two middle values.
*
* If the array is empty, this function returns `NaN`.
*
* @template T - The type of elements in the array.
* @param items An array to calculate the median.
* @param getValue A function that selects a numeric value from each element.
* @returns The median of all the numbers as determined by the `getValue` function.
*
* @example
* medianBy([{ a: 1 }, { a: 2 }, { a: 3 }, { a: 4 }, { a: 5 }], x => x.a); // Returns: 3
* medianBy([{ a: 1 }, { a: 2 }, { a: 3 }, { a: 4 }], x => x.a); // Returns: 2.5
* medianBy([], x => x.a); // Returns: NaN
*/
function medianBy(items, getValue) {
	return median(items.map((x) => getValue(x)));
}
//#endregion
export { medianBy };
