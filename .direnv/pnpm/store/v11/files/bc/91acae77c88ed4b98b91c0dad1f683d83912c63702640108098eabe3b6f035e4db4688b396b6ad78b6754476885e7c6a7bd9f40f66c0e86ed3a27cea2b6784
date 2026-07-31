//#region src/fp/array/dropWhile.d.ts
/**
 * Creates a function that removes leading values while a predicate returns true.
 *
 * Once the predicate returns false, that value and all following values are
 * emitted. The returned function is lazy-capable inside {@link pipe}.
 *
 * @template T - The type of elements in the array.
 * @param predicate - Called with each leading value and index while values are being dropped.
 * @returns A function that maps the piped array to the remaining suffix.
 *
 * @example
 * import { dropWhile, pipe } from 'es-toolkit/fp';
 *
 * pipe([1, 2, 3, 1], dropWhile(value => value < 3));
 * // => [3, 1]
 */
declare function dropWhile<T>(predicate: (item: T, index: number) => boolean): (array: readonly T[]) => T[];
//#endregion
export { dropWhile };