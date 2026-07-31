//#region src/fp/array/xorBy.d.ts
/**
 * Creates a function that returns the symmetric difference by mapped identity.
 *
 * The mapper is used to decide which values appear in exactly one array, matching the main
 * {@link xorBy} behavior.
 *
 * @template T - The type of elements in the arrays.
 * @template U - The type of comparison keys returned by the mapper.
 * @param secondArray - Values to compare with the piped array.
 * @param mapper - Called for values from both arrays to produce comparison keys.
 * @returns A function that maps a readonly array to its symmetric difference.
 *
 * @example
 * import { pipe, xorBy } from 'es-toolkit/fp';
 *
 * pipe([{ id: 1 }, { id: 2 }], xorBy([{ id: 2 }, { id: 3 }], item => item.id));
 * // => [{ id: 1 }, { id: 3 }]
 */
declare function xorBy<T, U>(secondArray: readonly T[], mapper: (item: T) => U): (array: readonly T[]) => T[];
//#endregion
export { xorBy };