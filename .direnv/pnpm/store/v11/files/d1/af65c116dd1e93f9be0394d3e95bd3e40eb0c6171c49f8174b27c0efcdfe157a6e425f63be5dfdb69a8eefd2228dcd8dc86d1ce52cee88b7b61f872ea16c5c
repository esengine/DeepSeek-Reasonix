//#region src/fp/array/find.d.ts
/**
 * Creates a function that returns the first value accepted by a type guard.
 *
 * The predicate receives the value, index, and full input array. The returned
 * function returns undefined when no value matches.
 *
 * @template T - The type of elements in the array.
 * @template S - The narrowed element type accepted by the type guard.
 * @param predicate - Type guard called with each value, index, and array until it returns true.
 * @returns A function that maps a readonly array to the first matching value, or undefined.
 *
 * @example
 * import { find, pipe } from 'es-toolkit/fp';
 *
 * const isString = (value: string | number): value is string => typeof value === 'string';
 * pipe([1, 'a', 2], find(isString));
 * // => 'a'
 */
declare function find<T, S extends T>(predicate: (value: T, index: number, array: readonly T[]) => value is S): (array: readonly T[]) => S | undefined;
/**
 * Creates a function that returns the first value matching a predicate.
 *
 * The predicate receives the value, index, and full input array. The returned
 * function returns undefined when no value matches.
 *
 * @template T - The type of elements in the array.
 * @param predicate - Predicate called with each value, index, and array until it returns true.
 * @returns A function that maps a readonly array to the first matching value, or undefined.
 *
 * @example
 * import { find, pipe } from 'es-toolkit/fp';
 *
 * pipe([{ id: 1 }, { id: 2 }], find(item => item.id === 2));
 * // => { id: 2 }
 */
declare function find<T>(predicate: (value: T, index: number, array: readonly T[]) => boolean): (array: readonly T[]) => T | undefined;
//#endregion
export { find };