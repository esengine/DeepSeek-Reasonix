//#region src/fp/array/sortBy.d.ts
/**
 * Creates a function that sorts an array of objects in ascending order by the
 * given `criteria`. Each criterion is either an object key or a function
 * returning the value to compare. When two elements tie on a criterion, the
 * next criterion breaks the tie. The sort is stable. Use it with {@link pipe}.
 *
 * @template T - The type of the objects in the array.
 * @param criteria - Object keys and/or selector functions used for comparison, applied in order.
 * @returns A function that maps a `readonly T[]` to a new, sorted `T[]`.
 *
 * @example
 * import { pipe, sortBy } from 'es-toolkit/fp';
 *
 * const users = [
 *   { user: 'foo', age: 24 },
 *   { user: 'bar', age: 7 },
 *   { user: 'foo', age: 8 },
 * ];
 *
 * pipe(users, sortBy(['user', 'age']));
 * // => [{ user: 'bar', age: 7 }, { user: 'foo', age: 8 }, { user: 'foo', age: 24 }]
 */
declare function sortBy<T extends object>(criteria: Array<((item: T) => unknown) | keyof T>): (array: readonly T[]) => T[];
//#endregion
export { sortBy };