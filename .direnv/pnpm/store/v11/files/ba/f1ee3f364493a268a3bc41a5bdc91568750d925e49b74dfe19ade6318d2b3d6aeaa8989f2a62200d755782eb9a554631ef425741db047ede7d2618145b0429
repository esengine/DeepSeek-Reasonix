//#region src/compat/array/slice.d.ts
/**
 * Create a slice of `array` from `start` up to, but not including, `end`.
 *
 * It does not return a dense array for sparse arrays unlike the native `Array.prototype.slice`.
 *
 * @template T - The type of the array elements.
 * @param array - The array to slice.
 * @param [start=0] - The start position.
 * @param [end=array.length] - The end position.
 * @returns Returns the slice of `array`.
 *
 * @example
 * slice([1, 2, 3], 1, 2); // => [2]
 * slice(new Array(3)); // => [undefined, undefined, undefined]
 */
declare function slice<T>(array: ArrayLike<T> | null | undefined, start?: number, end?: number): T[];
//#endregion
export { slice };