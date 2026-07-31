//#region src/fp/array/findLast.d.ts
/**
 * Creates a function that returns the last value accepted by a type guard.
 *
 * The predicate receives the value, index, and full input array while scanning
 * from right to left. The returned function returns undefined when no value matches.
 *
 * @template T - The type of elements in the array.
 * @template S - The narrowed element type accepted by the type guard.
 * @param predicate - Type guard called with each value, index, and array from right to left.
 * @returns A function that maps a readonly array to the last matching value, or undefined.
 *
 * @example
 * import { findLast, pipe } from 'es-toolkit/fp';
 *
 * const isString = (value: string | number): value is string => typeof value === 'string';
 * pipe([1, 'a', 2, 'b'], findLast(isString));
 * // => 'b'
 */
declare function findLast<T, S extends T>(predicate: (value: T, index: number, array: readonly T[]) => value is S): (array: readonly T[]) => S | undefined;
/**
 * Creates a function that returns the last value matching a predicate.
 *
 * The predicate receives the value, index, and full input array while scanning
 * from right to left. The returned function returns undefined when no value matches.
 *
 * @template T - The type of elements in the array.
 * @param predicate - Predicate called with each value, index, and array from right to left.
 * @returns A function that maps a readonly array to the last matching value, or undefined.
 *
 * @example
 * import { findLast, pipe } from 'es-toolkit/fp';
 *
 * pipe([{ id: 1 }, { id: 2 }, { id: 1 }], findLast(item => item.id === 1));
 * // => { id: 1 }
 */
declare function findLast<T>(predicate: (value: T, index: number, array: readonly T[]) => boolean): (array: readonly T[]) => T | undefined;
//#endregion
export { findLast };