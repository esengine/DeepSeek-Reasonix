//#region src/fp/array/unionWith.d.ts
/**
 * Creates a function that returns unique values using a custom equality function.
 *
 * Values from the piped array are considered before values from secondArray. The equality
 * function decides whether two values represent the same item.
 *
 * @template T - The type of elements in the arrays.
 * @param secondArray - Values to include after the piped array.
 * @param areItemsEqual - Returns true when two values are equal.
 * @returns A function that maps a readonly array to its union with secondArray.
 *
 * @example
 * import { pipe, unionWith } from 'es-toolkit/fp';
 *
 * pipe([{ id: 1 }], unionWith([{ id: 1 }, { id: 2 }], (a, b) => a.id === b.id));
 * // => [{ id: 1 }, { id: 2 }]
 */
declare function unionWith<T>(secondArray: readonly T[], areItemsEqual: (item: T, other: T) => boolean): (array: readonly T[]) => T[];
//#endregion
export { unionWith };