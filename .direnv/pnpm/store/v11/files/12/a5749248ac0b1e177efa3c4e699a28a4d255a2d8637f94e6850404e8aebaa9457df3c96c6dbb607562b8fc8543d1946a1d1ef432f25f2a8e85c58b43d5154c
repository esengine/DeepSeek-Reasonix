//#region src/set/find.d.ts
/**
 * Finds the first element in a Set for which the predicate function returns true.
 *
 * This function iterates through the elements of the Set and returns the first
 * element for which the predicate function returns true. If no element satisfies the predicate,
 * it returns undefined.
 *
 * @template T - The type of elements in the Set.
 * @param set - The Set to search.
 * @param doesMatch - A predicate function that tests each element.
 * @returns The first element that satisfies the predicate, or undefined if none found.
 *
 * @example
 * const set = new Set([
 *   { name: 'apple', quantity: 10 },
 *   { name: 'banana', quantity: 5 },
 *   { name: 'grape', quantity: 15 }
 * ]);
 * const result = find(set, (value) => value.quantity > 10);
 * // result will be: { name: 'grape', quantity: 15 }
 */
declare function find<T>(set: Set<T>, doesMatch: (value: T, value2: T, set: Set<T>) => boolean): T | undefined;
//#endregion
export { find };