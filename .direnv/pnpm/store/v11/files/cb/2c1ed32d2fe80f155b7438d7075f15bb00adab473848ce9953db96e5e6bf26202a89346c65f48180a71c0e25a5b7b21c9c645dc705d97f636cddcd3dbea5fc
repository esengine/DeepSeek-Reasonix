//#region src/compat/array/unionWith.d.ts
/**
 * This method is like `union` except that it accepts `comparator` which
 * is invoked to compare elements of `arrays`. The comparator is invoked
 * with two arguments: (arrVal, othVal).
 *
 * @template T
 * @param arrays - The arrays to inspect.
 * @param [comparator] - The comparator invoked per element.
 * @returns Returns the new array of combined values.
 *
 * @example
 * const objects = [{ 'x': 1, 'y': 2 }, { 'x': 2, 'y': 1 }];
 * const others = [{ 'x': 1, 'y': 1 }, { 'x': 1, 'y': 2 }];
 * unionWith(objects, others, isEqual);
 * // => [{ 'x': 1, 'y': 2 }, { 'x': 2, 'y': 1 }, { 'x': 1, 'y': 1 }]
 */
declare function unionWith<T>(arrays: ArrayLike<T> | null | undefined, comparator?: (a: T, b: T) => boolean): T[];
/**
 * This method is like `union` except that it accepts `comparator` which
 * is invoked to compare elements of `arrays`. The comparator is invoked
 * with two arguments: (arrVal, othVal).
 *
 * @template T
 * @param arrays - The first array to inspect.
 * @param arrays2 - The second array to inspect.
 * @param [comparator] - The comparator invoked per element.
 * @returns Returns the new array of combined values.
 *
 * @example
 * unionWith([1, 2], [2, 3], (a, b) => a === b);
 * // => [1, 2, 3]
 */
declare function unionWith<T>(arrays: ArrayLike<T> | null | undefined, arrays2: ArrayLike<T> | null | undefined, comparator?: (a: T, b: T) => boolean): T[];
/**
 * This method is like `union` except that it accepts `comparator` which
 * is invoked to compare elements of `arrays`. The comparator is invoked
 * with two arguments: (arrVal, othVal).
 *
 * @template T
 * @param arrays - The first array to inspect.
 * @param arrays2 - The second array to inspect.
 * @param arrays3 - The third array to inspect.
 * @param comparator - The comparator invoked per element.
 * @returns Returns the new array of combined values.
 *
 * @example
 * unionWith([1], [2], [3], (a, b) => a === b);
 * // => [1, 2, 3]
 */
declare function unionWith<T>(arrays: ArrayLike<T> | null | undefined, arrays2: ArrayLike<T> | null | undefined, arrays3: ArrayLike<T> | null | undefined, ...comparator: Array<((a: T, b: T) => boolean) | ArrayLike<T> | null | undefined>): T[];
//#endregion
export { unionWith };