//#region src/fp/array/head.d.ts
/**
 * Creates a function that returns the first element of a non-empty array.
 *
 * This overload preserves the non-empty tuple guarantee: the returned function
 * always returns T when the piped array has at least one element.
 *
 * @template T - The type of elements in the array.
 * @returns A function that maps a non-empty readonly array to its first element.
 *
 * @example
 * import { head, pipe } from 'es-toolkit/fp';
 *
 * pipe([1, 2, 3] as const, head());
 * // => 1
 */
declare function head<T>(): (array: readonly [T, ...T[]]) => T;
/**
 * Creates a function that returns the first element of an array, or undefined.
 *
 * Empty arrays return undefined, matching the main {@link head} behavior. Use
 * the returned function with {@link pipe}.
 *
 * @template T - The type of elements in the array.
 * @returns A function that maps a readonly array to its first element, or undefined.
 *
 * @example
 * import { head, pipe } from 'es-toolkit/fp';
 *
 * pipe([] as number[], head());
 * // => undefined
 */
declare function head<T>(): (array: readonly T[]) => T | undefined;
//#endregion
export { head };