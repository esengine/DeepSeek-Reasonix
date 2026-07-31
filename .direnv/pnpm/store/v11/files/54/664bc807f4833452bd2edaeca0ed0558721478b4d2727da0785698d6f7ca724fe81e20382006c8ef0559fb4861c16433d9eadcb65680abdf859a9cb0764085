//#region src/fp/array/isSubsetWith.d.ts
/**
 * Creates a predicate that checks subset membership with a custom equality function.
 *
 * Every value in the piped array must match at least one value in superset according
 * to areItemsEqual.
 *
 * @template T - The type of elements in the arrays.
 * @param superset - The array that may contain all values from the piped array.
 * @param areItemsEqual - Returns true when a piped value and a superset value are equal.
 * @returns A predicate for the piped array.
 *
 * @example
 * import { isSubsetWith, pipe } from 'es-toolkit/fp';
 *
 * pipe([{ id: 1 }], isSubsetWith([{ id: 1 }, { id: 2 }], (a, b) => a.id === b.id));
 * // => true
 */
declare function isSubsetWith<T>(superset: readonly T[], areItemsEqual: (item: T, other: T) => boolean): (array: readonly T[]) => boolean;
//#endregion
export { isSubsetWith };