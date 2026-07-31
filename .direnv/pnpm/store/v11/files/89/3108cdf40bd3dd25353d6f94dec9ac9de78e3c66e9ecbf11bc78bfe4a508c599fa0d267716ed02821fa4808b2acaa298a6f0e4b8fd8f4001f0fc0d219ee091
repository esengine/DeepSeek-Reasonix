//#region src/fp/array/groupBy.d.ts
/**
 * Creates a function that groups values by a derived key.
 *
 * The key selector receives each value, index, and full input array. Values that produce
 * the same key are collected in insertion order.
 *
 * @template T - The type of elements in the array.
 * @template K - The property-key type produced by the selector.
 * @param getKey - Called with each value, index, and array to produce a group key.
 * @returns A function that maps a readonly array to grouped values.
 *
 * @example
 * import { groupBy, pipe } from 'es-toolkit/fp';
 *
 * pipe(['ant', 'bear', 'cat'], groupBy(word => word.length));
 * // => { 3: ['ant', 'cat'], 4: ['bear'] }
 */
declare function groupBy<T, K extends PropertyKey>(getKey: (item: T, index: number, array: readonly T[]) => K): (array: readonly T[]) => Record<K, T[]>;
//#endregion
export { groupBy };