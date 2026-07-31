//#region src/fp/array/drop.d.ts
/**
 * Creates a function that removes a number of values from the start of an array.
 *
 * Negative counts are treated as 0, matching the main {@link drop}
 * implementation. The returned function is lazy-capable inside {@link pipe}.
 *
 * @template T - The type of elements in the array.
 * @param count - The number of values to skip from the start.
 * @returns A function that maps the piped array to the remaining suffix.
 *
 * @example
 * import { drop, pipe } from 'es-toolkit/fp';
 *
 * pipe([1, 2, 3, 4], drop(2));
 * // => [3, 4]
 */
declare function drop<T>(count: number): (array: readonly T[]) => T[];
//#endregion
export { drop };