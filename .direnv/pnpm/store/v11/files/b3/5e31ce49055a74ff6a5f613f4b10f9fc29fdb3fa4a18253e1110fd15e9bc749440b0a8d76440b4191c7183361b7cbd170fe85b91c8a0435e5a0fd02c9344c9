//#region src/map/some.d.ts
/**
 * Tests whether at least one entry in a Map satisfies the provided predicate function.
 *
 * This function iterates through the entries of the Map and checks if the predicate function
 * returns true for at least one entry. It returns true if any entry satisfies the predicate,
 * and false otherwise.
 *
 * @template K - The type of keys in the Map.
 * @template V - The type of values in the Map.
 * @param map - The Map to test.
 * @param doesMatch - A predicate function that tests each entry.
 * @returns true if at least one entry satisfies the predicate, false otherwise.
 *
 * @example
 * const map = new Map([
 *   ['a', 1],
 *   ['b', 2],
 *   ['c', 3]
 * ]);
 * const result = some(map, (value) => value > 2);
 * // result will be: true
 *
 * const result2 = some(map, (value) => value > 5);
 * // result2 will be: false
 */
declare function some<K, V>(map: Map<K, V>, doesMatch: (value: V, key: K, map: Map<K, V>) => boolean): boolean;
//#endregion
export { some };