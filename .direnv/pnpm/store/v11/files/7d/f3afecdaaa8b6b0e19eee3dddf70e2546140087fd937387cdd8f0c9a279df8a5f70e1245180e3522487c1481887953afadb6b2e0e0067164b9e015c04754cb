//#region src/fp/array/orderBy.d.ts
/**
 * Creates a function that sorts objects by criteria and order directions.
 *
 * Criteria can be object keys or selector functions. The returned function does not mutate
 * the input array; it returns a sorted copy following the main {@link orderBy} behavior.
 *
 * @template T - The object type in the array.
 * @param criteria - Keys or selector functions used for comparison.
 * @param orders - Sort directions corresponding to the criteria.
 * @returns A function that maps a readonly array to a sorted copy.
 *
 * @example
 * import { orderBy, pipe } from 'es-toolkit/fp';
 *
 * pipe([{ name: 'a', age: 2 }, { name: 'b', age: 1 }], orderBy(['age'], ['asc']));
 * // => [{ name: 'b', age: 1 }, { name: 'a', age: 2 }]
 */
declare function orderBy<T extends object>(criteria: Array<((item: T) => unknown) | keyof T>, orders: Array<'asc' | 'desc'>): (array: readonly T[]) => T[];
//#endregion
export { orderBy };