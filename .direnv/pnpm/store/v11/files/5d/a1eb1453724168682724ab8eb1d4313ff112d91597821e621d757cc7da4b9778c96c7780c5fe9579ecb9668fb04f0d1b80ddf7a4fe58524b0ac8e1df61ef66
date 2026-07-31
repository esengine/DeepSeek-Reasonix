//#region src/fp/array/last.d.ts
/**
 * Creates a function that returns the last element of a non-empty array.
 *
 * This overload preserves the non-empty tuple guarantee: the returned function
 * always returns T when the piped array has at least one element.
 *
 * @template T - The type of elements in the array.
 * @returns A function that maps a non-empty readonly array to its last element.
 *
 * @example
 * import { last, pipe } from 'es-toolkit/fp';
 *
 * pipe([1, 2, 3] as const, last());
 * // => 3
 */
declare function last<T>(): (array: readonly [...T[], T]) => T;
/**
 * Creates a function that returns the last element of an array, or undefined.
 *
 * Empty arrays return undefined, matching the main {@link last} behavior. Use
 * the returned function with {@link pipe}.
 *
 * @template T - The type of elements in the array.
 * @returns A function that maps a readonly array to its last element, or undefined.
 *
 * @example
 * import { last, pipe } from 'es-toolkit/fp';
 *
 * pipe([] as number[], last());
 * // => undefined
 */
declare function last<T>(): (array: readonly T[]) => T | undefined;
//#endregion
export { last };