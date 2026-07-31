//#region src/fp/array/takeRight.d.ts
/**
 * Creates a function that returns the last count values of an array.
 *
 * The returned function follows the main {@link takeRight} behavior and returns a new array.
 * Use it with {@link pipe}.
 *
 * @template T - The type of elements in the array.
 * @param count - The number of values to take from the end.
 * @returns A function that maps a readonly array to its suffix.
 *
 * @example
 * import { pipe, takeRight } from 'es-toolkit/fp';
 *
 * pipe([1, 2, 3, 4], takeRight(2));
 * // => [3, 4]
 */
declare function takeRight<T>(count: number): (array: readonly T[]) => T[];
//#endregion
export { takeRight };