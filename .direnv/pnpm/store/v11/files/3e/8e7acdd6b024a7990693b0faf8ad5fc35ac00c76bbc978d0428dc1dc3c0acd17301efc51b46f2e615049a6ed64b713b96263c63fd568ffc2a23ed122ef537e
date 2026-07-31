//#region src/compat/array/head.d.ts
/**
 * Returns the first element of an array or `undefined` if the array is empty.
 *
 * @template T - The type of elements in the array.
 * @param array - A non-empty tuple with at least one element.
 * @returns The first element of the array.
 *
 * @example
 * const arr = [1, 2, 3] as const;
 * const first = head(arr);
 * // first will be 1
 */
declare function head<T>(array: readonly [T, ...unknown[]]): T;
/**
 * Returns the first element of an array or `undefined` if the array is empty.
 *
 * @template T - The type of elements in the array.
 * @param array - The array from which to get the first element.
 * @returns The first element of the array, or `undefined` if the array is empty/null/undefined.
 *
 * @example
 * const arr = [1, 2, 3];
 * const first = head(arr);
 * // first will be 1
 *
 * const emptyArr: number[] = [];
 * const noElement = head(emptyArr);
 * // noElement will be undefined
 */
declare function head<T>(array: ArrayLike<T> | null | undefined): T | undefined;
//#endregion
export { head };