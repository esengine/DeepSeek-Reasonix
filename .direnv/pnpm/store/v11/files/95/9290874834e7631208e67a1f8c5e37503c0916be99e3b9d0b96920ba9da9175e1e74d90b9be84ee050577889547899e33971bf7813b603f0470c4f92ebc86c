//#region src/set/forEach.d.ts
/**
 * Executes a provided function once for each element in a Set.
 *
 * This function iterates through all elements of the Set and executes the callback function
 * for each element. The callback receives the value twice (for consistency with Map.forEach)
 * and the Set itself as arguments.
 *
 * @template T - The type of elements in the Set.
 * @param set - The Set to iterate over.
 * @param callback - A function to execute for each element.
 *
 * @example
 * const set = new Set([1, 2, 3]);
 * forEach(set, (value) => {
 *   console.log(value * 2);
 * });
 * // Output:
 * // 2
 * // 4
 * // 6
 */
declare function forEach<T>(set: Set<T>, callback: (value: T, value2: T, set: Set<T>) => void): void;
//#endregion
export { forEach };