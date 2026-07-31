//#region src/fp/array/differenceWith.d.ts
/**
 * Creates a function that returns values that are not equal to any configured value.
 *
 * Equality is decided by the provided comparator. The returned function is
 * lazy-capable inside {@link pipe}.
 *
 * @template T - The type of elements in the piped array.
 * @template U - The type of elements in the configured array.
 * @param secondArray - Values to compare against the piped array.
 * @param areItemsEqual - Returns true when a piped value equals a configured value.
 * @returns A function that maps the piped array to its custom difference.
 *
 * @example
 * import { differenceWith, pipe } from 'es-toolkit/fp';
 *
 * pipe([{ id: 1 }, { id: 2 }], differenceWith([2], (item, id) => item.id === id));
 * // => [{ id: 1 }]
 */
declare function differenceWith<T, U>(secondArray: readonly U[], areItemsEqual: (item: T, other: U) => boolean): (array: readonly T[]) => T[];
//#endregion
export { differenceWith };