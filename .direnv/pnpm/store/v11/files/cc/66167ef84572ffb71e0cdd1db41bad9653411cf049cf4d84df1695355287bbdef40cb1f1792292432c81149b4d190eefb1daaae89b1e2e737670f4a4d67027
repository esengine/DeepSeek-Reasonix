//#region src/fp/array/compact.d.ts
type NotFalsey<T> = Exclude<T, false | null | 0 | 0n | '' | undefined>;
/**
 * Creates a function that removes falsey values from an array. Use it with {@link pipe}.
 *
 * Falsey values include false, null, 0, -0, 0n, an empty string, undefined,
 * and NaN. The returned function is lazy-capable inside {@link pipe}, so a
 * trailing short-circuiting operator such as {@link take} can stop once enough
 * truthy values have been emitted.
 *
 * @template T - The type of elements in the array.
 * @returns A function that maps a readonly array to a new array with falsey values removed.
 *
 * @example
 * import { compact, pipe } from 'es-toolkit/fp';
 *
 * pipe([0, 1, false, 2, '', 3], compact());
 * // => [1, 2, 3]
 */
declare function compact<T>(): (array: readonly T[]) => Array<NotFalsey<T>>;
//#endregion
export { compact };