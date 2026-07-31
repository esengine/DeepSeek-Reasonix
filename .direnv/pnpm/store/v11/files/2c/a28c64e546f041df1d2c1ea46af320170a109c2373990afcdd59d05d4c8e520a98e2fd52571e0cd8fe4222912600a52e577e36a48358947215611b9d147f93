//#region src/array/unzipWith.d.ts
/**
 * Unzips an array of arrays, applying an `iteratee` function to regrouped elements.
 *
 * @template T, R
 * @param target - The nested array to unzip. This is an array of arrays,
 * where each inner array contains elements to be unzipped.
 * @param iteratee - A function to transform the unzipped elements.
 * @returns A new array of unzipped and transformed elements.
 *
 * @example
 * const nestedArray = [[1, 2], [3, 4], [5, 6]];
 * const result = unzipWith(nestedArray, (item, item2, item3) => item + item2 + item3);
 * // result will be [9, 12]
 */
declare function unzipWith<T, R>(target: readonly T[][], iteratee: (...args: T[]) => R): R[];
//#endregion
export { unzipWith };