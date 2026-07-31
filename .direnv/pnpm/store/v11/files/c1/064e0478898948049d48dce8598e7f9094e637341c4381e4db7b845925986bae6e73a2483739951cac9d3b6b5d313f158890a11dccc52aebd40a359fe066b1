//#region src/fp/object/omit.d.ts
/**
 * Creates a function that builds a new object with the given `keys` removed from
 * the input object. Use it with {@link pipe}.
 *
 * @template T - The type of the input object.
 * @template K - The union of keys to omit.
 * @param keys - The keys to exclude from the new object.
 * @returns A function that maps an object `T` to `Omit<T, K>`.
 *
 * @example
 * import { pipe, omit } from 'es-toolkit/fp';
 *
 * pipe({ a: 1, b: 2, c: 3 }, omit(['b', 'c'])); // => { a: 1 }
 */
declare function omit<T extends Record<string, any>, K extends keyof T>(keys: readonly K[]): (obj: T) => Omit<T, K>;
//#endregion
export { omit };