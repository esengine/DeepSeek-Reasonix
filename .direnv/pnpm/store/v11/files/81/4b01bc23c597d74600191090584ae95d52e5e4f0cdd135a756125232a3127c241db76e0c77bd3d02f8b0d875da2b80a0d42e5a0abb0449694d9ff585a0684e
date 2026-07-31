//#region src/compat/object/transform.d.ts
/**
 * Traverses object values and creates a new object by accumulating them in the desired form.
 *
 * @template T - The type of object.
 * @template R - The type of accumulator.
 * @param object - The array to iterate over.
 * @param iteratee - The function invoked per iteration.
 * @param [accumulator] - The initial value.
 * @returns Returns the accumulated value.
 *
 * @example
 * const array = [2, 3, 4];
 * transform(array, (acc, value) => { acc.push(value * 2); }, []);
 * // => [4, 6, 8]
 */
declare function transform<T, R>(object: readonly T[], iteratee: (acc: R, curr: T, index: number, arr: T[]) => void, accumulator?: R): R;
/**
 * Traverses object values and creates a new object by accumulating them in the desired form.
 *
 * @template T - The type of object.
 * @template R - The type of accumulator.
 * @param object - The object to iterate over.
 * @param iteratee - The function invoked per iteration.
 * @param [accumulator] - The initial value.
 * @returns Returns the accumulated value.
 *
 * @example
 * const obj = { 'a': 1, 'b': 2, 'c': 1 };
 * transform(obj, (result, value, key) => { (result[value] || (result[value] = [])).push(key) }, {});
 * // => { '1': ['a', 'c'], '2': ['b'] }
 */
declare function transform<T, R>(object: Record<string, T>, iteratee: (acc: R, curr: T, key: string, dict: Record<string, T>) => void, accumulator?: R): R;
/**
 * Traverses object values and creates a new object by accumulating them in the desired form.
 *
 * @template T - The type of object.
 * @template R - The type of accumulator.
 * @param object - The object to iterate over.
 * @param iteratee - The function invoked per iteration.
 * @param [accumulator] - The initial value.
 * @returns Returns the accumulated value.
 *
 * @example
 * const obj = { x: 1, y: 2, z: 3 };
 * transform(obj, (acc, value, key) => { acc[key] = value * 2; }, {});
 * // => { x: 2, y: 4, z: 6 }
 */
declare function transform<T extends object, R>(object: T, iteratee: (acc: R, curr: T[keyof T], key: keyof T, dict: Record<keyof T, T[keyof T]>) => void, accumulator?: R): R;
/**
 * Traverses object values and creates a new object by accumulating them in the desired form.
 *
 * @param object - The array to iterate over.
 * @returns Returns the accumulated value.
 *
 * @example
 * const array = [1, 2, 3];
 * transform(array);
 * // => [1, 2, 3]
 */
declare function transform(object: any[]): any[];
/**
 * Traverses object values and creates a new object by accumulating them in the desired form.
 *
 * @param object - The object to iterate over.
 * @returns Returns the accumulated value.
 *
 * @example
 * const obj = { a: 1, b: 2 };
 * transform(obj);
 * // => { a: 1, b: 2 }
 */
declare function transform(object: object): Record<string, any>;
//#endregion
export { transform };