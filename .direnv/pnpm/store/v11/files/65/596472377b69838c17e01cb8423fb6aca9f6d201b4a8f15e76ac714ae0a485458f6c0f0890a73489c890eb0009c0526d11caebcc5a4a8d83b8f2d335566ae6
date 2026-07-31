//#region src/fp/array/uniqWith.d.ts
/**
 * Creates a function that removes duplicate values using a custom equality function.
 *
 * The first value in each equality group is preserved. The returned function is
 * lazy-capable inside {@link pipe}.
 *
 * @template T - The type of elements in the array.
 * @param areItemsEqual - Returns true when two values should be treated as equal.
 * @returns A function that maps the piped array to unique values.
 *
 * @example
 * import { pipe, uniqWith } from 'es-toolkit/fp';
 *
 * pipe([{ id: 1 }, { id: 1 }, { id: 2 }], uniqWith((a, b) => a.id === b.id));
 * // => [{ id: 1 }, { id: 2 }]
 */
declare function uniqWith<T>(areItemsEqual: (item: T, other: T) => boolean): (array: readonly T[]) => T[];
//#endregion
export { uniqWith };