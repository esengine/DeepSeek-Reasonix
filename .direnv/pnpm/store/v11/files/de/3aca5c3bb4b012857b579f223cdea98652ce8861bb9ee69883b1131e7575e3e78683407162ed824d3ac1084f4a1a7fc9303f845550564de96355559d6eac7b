//#region src/map/countBy.d.ts
/**
 * Counts the occurrences of items in a Map based on a transformation function.
 *
 * This function takes a Map and a function that generates a key from each value-key pair.
 * It returns an object with the generated keys as properties and their counts as values.
 * The count is incremented for each entry for which the transformation produces the same key.
 *
 * @template K - The type of the Map's keys.
 * @template V - The type of the Map's values.
 * @template K2 - The type of keys produced by the transformation function.
 * @param map - The Map to count occurrences from.
 * @param mapper - The function to produce a key for counting.
 * @returns An object containing the mapped keys and their counts.
 *
 * @example
 * const map = new Map([
 *   ['a', 1],
 *   ['b', 2],
 *   ['c', 1]
 * ]);
 * const result = countBy(map, (value) => value);
 * // result will be { 1: 2, 2: 1 }
 *
 * @example
 * const map = new Map([
 *   ['alice', 20],
 *   ['bob', 30],
 *   ['carol', 20]
 * ]);
 * const result = countBy(map, (value, key) => key[0]);
 * // result will be { a: 1, b: 1, c: 1 }
 */
declare function countBy<K, V, K2 extends PropertyKey>(map: Map<K, V>, mapper: (value: V, key: K, object: Map<K, V>) => K2): Map<K2, number>;
//#endregion
export { countBy };