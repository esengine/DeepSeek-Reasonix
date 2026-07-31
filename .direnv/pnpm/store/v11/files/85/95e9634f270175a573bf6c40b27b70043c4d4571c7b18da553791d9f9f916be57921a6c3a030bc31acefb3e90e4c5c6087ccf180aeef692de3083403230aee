import { ListIteratee } from "../_internal/ListIteratee.mjs";

//#region src/compat/array/dropRightWhile.d.ts
/**
 * Creates a slice of array excluding elements dropped from the end until predicate returns falsey.
 * The predicate is invoked with three arguments: (value, index, array).
 *
 * @template T - The type of elements in the array.
 * @param array - The array to query.
 * @param [predicate] - The function invoked per iteration.
 * @returns Returns the slice of array.
 * @example
 *
 * const users = [
 *   { user: 'barney', active: true },
 *   { user: 'fred', active: false },
 *   { user: 'pebbles', active: false }
 * ];
 *
 * // Using function predicate
 * dropRightWhile(users, user => !user.active);
 * // => [{ user: 'barney', active: true }]
 *
 * // Using matches shorthand
 * dropRightWhile(users, { user: 'pebbles', active: false });
 * // => [{ user: 'barney', active: true }, { user: 'fred', active: false }]
 *
 * // Using matchesProperty shorthand
 * dropRightWhile(users, ['active', false]);
 * // => [{ user: 'barney', active: true }]
 *
 * // Using property shorthand
 * dropRightWhile(users, 'active');
 * // => [{ user: 'barney', active: true }, { user: 'fred', active: false }, { user: 'pebbles', active: false }]
 */
declare function dropRightWhile<T>(array: ArrayLike<T> | null | undefined, predicate?: ListIteratee<T>): T[];
//#endregion
export { dropRightWhile };