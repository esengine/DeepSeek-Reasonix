//#region src/fp/array/differenceBy.d.ts
/**
 * Creates a function that returns values whose mapped identity is absent from another array.
 *
 * The mapper is applied to values from both arrays. The returned function is
 * lazy-capable inside {@link pipe}.
 *
 * @template T - The type of elements in the piped array.
 * @template U - The type of elements in the configured array.
 * @param secondArray - Values to exclude from the piped array after mapping.
 * @param mapper - Maps values from both arrays to comparison keys.
 * @returns A function that maps the piped array to its mapped difference.
 *
 * @example
 * import { differenceBy, pipe } from 'es-toolkit/fp';
 *
 * pipe([{ id: 1 }, { id: 2 }], differenceBy([2], value => typeof value === 'number' ? value : value.id));
 * // => [{ id: 1 }]
 */
declare function differenceBy<T, U>(secondArray: readonly U[], mapper: (value: T | U) => unknown): (array: readonly T[]) => T[];
//#endregion
export { differenceBy };