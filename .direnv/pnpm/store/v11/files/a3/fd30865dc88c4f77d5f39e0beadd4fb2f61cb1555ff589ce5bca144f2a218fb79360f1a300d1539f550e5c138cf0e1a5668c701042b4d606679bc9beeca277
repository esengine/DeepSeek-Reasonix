//#region src/fp/array/dropRightWhile.d.ts
/**
 * Creates a function that drops trailing values while a predicate returns true.
 *
 * The predicate is evaluated from right to left and receives the value, index, and full
 * input array. Use the returned function with {@link pipe}.
 *
 * @template T - The type of elements in the array.
 * @param predicate - Called with each value, index, and array while dropping from the end.
 * @returns A function that maps a readonly array to the remaining prefix.
 *
 * @example
 * import { dropRightWhile, pipe } from 'es-toolkit/fp';
 *
 * pipe([1, 2, 3, 4], dropRightWhile(value => value > 2));
 * // => [1, 2]
 */
declare function dropRightWhile<T>(predicate: (item: T, index: number, array: readonly T[]) => boolean): (array: readonly T[]) => T[];
//#endregion
export { dropRightWhile };