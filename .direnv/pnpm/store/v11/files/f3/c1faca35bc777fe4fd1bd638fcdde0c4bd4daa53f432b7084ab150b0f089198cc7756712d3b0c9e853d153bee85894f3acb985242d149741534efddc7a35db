//#region src/fp/array/union.d.ts
/**
 * Creates a function that returns unique values from the piped array and secondArray.
 *
 * Values keep their first occurrence order, matching the main {@link union} behavior.
 * Use the returned function with {@link pipe}.
 *
 * @template T - The type of elements in the arrays.
 * @param secondArray - Values to include after the piped array.
 * @returns A function that maps a readonly array to its union with secondArray.
 *
 * @example
 * import { pipe, union } from 'es-toolkit/fp';
 *
 * pipe([1, 2], union([2, 3]));
 * // => [1, 2, 3]
 */
declare function union<T>(secondArray: readonly T[]): (array: readonly T[]) => T[];
//#endregion
export { union };