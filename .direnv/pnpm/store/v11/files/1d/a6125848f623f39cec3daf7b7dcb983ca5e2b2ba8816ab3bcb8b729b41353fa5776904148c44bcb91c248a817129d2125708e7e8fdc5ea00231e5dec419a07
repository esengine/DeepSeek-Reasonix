//#region src/fp/array/chunkBy.d.ts
/**
 * Creates a function that splits consecutive elements whenever the derived key changes.
 *
 * The iteratee is called from left to right. A new chunk starts when its key differs
 * from the previous key by strict inequality. Use the returned function with {@link pipe}.
 *
 * @template T - The type of elements in the array.
 * @param iteratee - Called for each value to produce the comparison key.
 * @returns A function that maps a readonly array to consecutive same-key chunks.
 *
 * @example
 * import { chunkBy, pipe } from 'es-toolkit/fp';
 *
 * pipe([1, 1, 2, 3, 3], chunkBy(value => value));
 * // => [[1, 1], [2], [3, 3]]
 */
declare function chunkBy<T>(iteratee: (value: T) => unknown): (array: readonly T[]) => T[][];
//#endregion
export { chunkBy };