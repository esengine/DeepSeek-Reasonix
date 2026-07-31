//#region src/fp/array/unionBy.d.ts
/**
 * Creates a function that returns unique values by mapped identity.
 *
 * Values from the piped array are considered before values from secondArray. The mapper
 * is used to compare identities, matching the main {@link unionBy} behavior.
 *
 * @template T - The type of elements in the arrays.
 * @template U - The type of comparison keys returned by the mapper.
 * @param secondArray - Values to include after the piped array.
 * @param mapper - Called for values from both arrays to produce comparison keys.
 * @returns A function that maps a readonly array to its union with secondArray.
 *
 * @example
 * import { pipe, unionBy } from 'es-toolkit/fp';
 *
 * pipe([{ id: 1 }], unionBy([{ id: 1 }, { id: 2 }], item => item.id));
 * // => [{ id: 1 }, { id: 2 }]
 */
declare function unionBy<T, U>(secondArray: readonly T[], mapper: (item: T) => U): (array: readonly T[]) => T[];
//#endregion
export { unionBy };