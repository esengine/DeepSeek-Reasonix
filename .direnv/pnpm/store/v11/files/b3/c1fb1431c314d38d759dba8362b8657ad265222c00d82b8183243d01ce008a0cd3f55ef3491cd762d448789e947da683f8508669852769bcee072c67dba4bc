import { ValueKeyIteratee } from "../_internal/ValueKeyIteratee.mjs";

//#region src/compat/object/omitBy.d.ts
/**
 * Creates a new object composed of the properties that do not satisfy the predicate function.
 *
 * @template T
 * @param object - The source object.
 * @param predicate - The function invoked per property.
 * @returns Returns the new object.
 *
 * @example
 * omitBy({ 'a': 1, 'b': '2', 'c': 3 }, isString);
 * // => { 'a': 1, 'c': 3 }
 */
declare function omitBy<T>(object: Record<string, T> | null | undefined, predicate?: ValueKeyIteratee<T>): Record<string, T>;
/**
 * Creates a new object composed of the properties that do not satisfy the predicate function.
 *
 * @template T
 * @param object - The source object.
 * @param predicate - The function invoked per property.
 * @returns Returns the new object.
 *
 * @example
 * omitBy({ 0: 1, 1: '2', 2: 3 }, isString);
 * // => { 0: 1, 2: 3 }
 */
declare function omitBy<T>(object: Record<number, T> | null | undefined, predicate?: ValueKeyIteratee<T>): Record<number, T>;
/**
 * Creates a new object composed of the properties that do not satisfy the predicate function.
 *
 * @template T
 * @param object - The source object.
 * @param predicate - The function invoked per property.
 * @returns Returns the new object.
 *
 * @example
 * omitBy({ 'a': 1, 'b': '2', 'c': 3 }, isString);
 * // => { 'a': 1, 'c': 3 }
 */
declare function omitBy<T extends object>(object: T | null | undefined, predicate: ValueKeyIteratee<T[keyof T]>): Partial<T>;
//#endregion
export { omitBy };