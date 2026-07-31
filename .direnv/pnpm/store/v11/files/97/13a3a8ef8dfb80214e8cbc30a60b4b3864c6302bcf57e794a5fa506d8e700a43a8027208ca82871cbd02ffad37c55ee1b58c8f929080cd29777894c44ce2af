import { PropertyPath } from "../_internal/PropertyPath.mjs";

//#region src/compat/object/result.d.ts
/**
 * Retrieves the value at a given path of an object.
 * If the resolved value is a function, it is invoked with the object as its `this` context.
 * If the value is `undefined`, the `defaultValue` is returned.
 *
 * @template T - The type of object.
 * @template R - The type of the value to return.
 * @param object - The object to query.
 * @param path - The path of the property to get.
 * @param [defaultValue] - The value returned if the resolved value is `undefined`.
 * @returns Returns the resolved value.
 *
 * @example
 * const obj = { a: { b: { c: 3 } } };
 * result(obj, 'a.b.c');
 * // => 3
 *
 * @example
 * const obj = { a: () => 5 };
 * result(obj, 'a');
 * // => 5 (calls the function `a` and returns its result)
 *
 * @example
 * const obj = { a: { b: null } };
 * result(obj, 'a.b.c', 'default');
 * // => 'default'
 *
 * @example
 * const obj = { a: { b: { c: 3 } } };
 * result(obj, 'a.b.d', () => 'default');
 * // => 'default'
 */
declare function result<R>(object: any, path: PropertyPath, defaultValue?: R | ((...args: any[]) => R)): R;
//#endregion
export { result };