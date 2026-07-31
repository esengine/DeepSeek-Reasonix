//#region src/compat/array/indexOf.d.ts
/**
 * Finds the index of the first occurrence of a value in an array.
 *
 * This method is similar to `Array.prototype.indexOf`, but it also finds `NaN` values.
 * It uses strict equality (`===`) to compare elements.
 *
 * @template T - The type of elements in the array.
 * @param array - The array to search.
 * @param searchElement - The value to search for.
 * @param [fromIndex] - The index to start the search at.
 * @returns The index (zero-based) of the first occurrence of the value in the array, or `-1` if the value is not found.
 *
 * @example
 * const array = [1, 2, 3, NaN];
 * indexOf(array, 3); // => 2
 * indexOf(array, NaN); // => 3
 */
declare function indexOf<T>(array: ArrayLike<T> | null | undefined, searchElement: T, fromIndex?: number): number;
//#endregion
export { indexOf };