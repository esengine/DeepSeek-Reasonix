//#region src/fp/array/takeWhile.d.ts
/**
 * Creates a function that takes leading values while a predicate returns true.
 *
 * The returned function is lazy-capable and short-circuiting inside
 * {@link pipe}; once the predicate returns false, upstream lazy operators stop
 * processing further input.
 *
 * @template T - The type of elements in the array.
 * @param predicate - Called with each value and index until it returns false.
 * @returns A function that maps the piped array to the matching prefix.
 *
 * @example
 * import { pipe, takeWhile } from 'es-toolkit/fp';
 *
 * pipe([1, 2, 3, 1], takeWhile(value => value < 3));
 * // => [1, 2]
 */
declare function takeWhile<T>(predicate: (element: T, index: number) => boolean): (array: readonly T[]) => T[];
//#endregion
export { takeWhile };