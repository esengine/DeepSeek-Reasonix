//#region src/fp/array/tail.d.ts
/**
 * Creates a function that returns every element except the first one.
 *
 * Empty and single-element arrays return an empty array, matching the main {@link tail}
 * behavior. Use the returned function with {@link pipe}.
 *
 * @template T - The type of elements in the array.
 * @returns A function that maps a readonly array to its tail elements.
 *
 * @example
 * import { pipe, tail } from 'es-toolkit/fp';
 *
 * pipe([1, 2, 3], tail());
 * // => [2, 3]
 */
declare function tail<T>(): (array: readonly T[]) => T[];
//#endregion
export { tail };