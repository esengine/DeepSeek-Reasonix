//#region src/fp/array/partition.d.ts
/**
 * Creates a function that splits an array with a type-guard predicate.
 *
 * Values for which the predicate returns true are placed in the first array and
 * narrowed to U. The remaining values are placed in the second array as Exclude<T, U>.
 *
 * @template T - The type of elements in the array.
 * @template U - The narrowed element type accepted by the type guard.
 * @param predicate - Type guard called with each value, index, and array.
 * @returns A function that maps a readonly array to truthy and falsy partitions.
 *
 * @example
 * import { partition, pipe } from 'es-toolkit/fp';
 *
 * const isString = (value: string | number): value is string => typeof value === 'string';
 * pipe([1, 'a', 2, 'b'], partition(isString));
 * // => [['a', 'b'], [1, 2]]
 */
declare function partition<T, U extends T>(predicate: (value: T, index: number, array: readonly T[]) => value is U): (array: readonly T[]) => [truthy: U[], falsy: Array<Exclude<T, U>>];
/**
 * Creates a function that splits an array by a predicate.
 *
 * Values for which the predicate returns true are placed in the first array; all
 * other values are placed in the second array.
 *
 * @template T - The type of elements in the array.
 * @param predicate - Predicate called with each value, index, and array.
 * @returns A function that maps a readonly array to truthy and falsy partitions.
 *
 * @example
 * import { partition, pipe } from 'es-toolkit/fp';
 *
 * pipe([1, 2, 3, 4], partition(value => value % 2 === 0));
 * // => [[2, 4], [1, 3]]
 */
declare function partition<T>(predicate: (value: T, index: number, array: readonly T[]) => boolean): (array: readonly T[]) => [truthy: T[], falsy: T[]];
//#endregion
export { partition };