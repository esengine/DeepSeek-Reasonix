//#region src/fp/array/keyBy.d.ts
/**
 * Creates a function that indexes values by a derived key.
 *
 * The key selector receives each value, index, and full input array. When multiple values
 * produce the same key, the last value wins, matching the main {@link keyBy} behavior.
 *
 * @template T - The type of elements in the array.
 * @template K - The property-key type produced by the selector.
 * @param getKey - Called with each value, index, and array to produce an object key.
 * @returns A function that maps a readonly array to an object keyed by the derived keys.
 *
 * @example
 * import { keyBy, pipe } from 'es-toolkit/fp';
 *
 * pipe([{ id: 'a', value: 1 }, { id: 'b', value: 2 }], keyBy(item => item.id));
 * // => { a: { id: 'a', value: 1 }, b: { id: 'b', value: 2 } }
 */
declare function keyBy<T, K extends PropertyKey>(getKey: (item: T, index: number, array: readonly T[]) => K): (array: readonly T[]) => Record<K, T>;
//#endregion
export { keyBy };