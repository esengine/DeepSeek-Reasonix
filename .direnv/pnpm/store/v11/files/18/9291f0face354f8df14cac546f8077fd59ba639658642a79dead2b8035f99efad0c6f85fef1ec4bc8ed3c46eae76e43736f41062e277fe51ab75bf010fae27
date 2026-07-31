//#region src/map/forEach.d.ts
/**
 * Executes a provided function once for each entry in a Map.
 *
 * This function iterates through all entries of the Map and executes the callback function
 * for each entry. The callback receives the value, key, and the Map itself as arguments.
 *
 * @template K - The type of keys in the Map.
 * @template V - The type of values in the Map.
 * @param map - The Map to iterate over.
 * @param callback - A function to execute for each entry.
 *
 * @example
 * const map = new Map([
 *   ['a', 1],
 *   ['b', 2],
 *   ['c', 3]
 * ]);
 * forEach(map, (value, key) => {
 *   console.log(`${key}: ${value}`);
 * });
 * // Output:
 * // a: 1
 * // b: 2
 * // c: 3
 */
declare function forEach<K, V>(map: Map<K, V>, callback: (value: V, key: K, map: Map<K, V>) => void): void;
//#endregion
export { forEach };