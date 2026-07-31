import { PropertyPath } from "../_internal/PropertyPath.js";

//#region src/compat/object/set.d.ts
/**
 * Sets the value at the specified path of the given object. If any part of the path does not exist, it will be created.
 *
 * @template T - The type of the object.
 * @param object - The object to modify.
 * @param path - The path of the property to set.
 * @param value - The value to set.
 * @returns The modified object.
 *
 * @example
 * // Set a value in a nested object
 * const obj = { a: { b: { c: 3 } } };
 * set(obj, 'a.b.c', 4);
 * console.log(obj.a.b.c); // 4
 *
 * @example
 * // Set a value in an array
 * const arr = [1, 2, 3];
 * set(arr, 1, 4);
 * console.log(arr[1]); // 4
 *
 * @example
 * // Create non-existent path and set value
 * const obj = {};
 * set(obj, 'a.b.c', 4);
 * console.log(obj); // { a: { b: { c: 4 } } }
 */
declare function set<T extends object>(object: T, path: PropertyPath, value: any): T;
/**
 * Sets the value at the specified path of the given object. If any part of the path does not exist, it will be created.
 *
 * @template R - The return type.
 * @param object - The object to modify.
 * @param path - The path of the property to set.
 * @param value - The value to set.
 * @returns The modified object.
 *
 * @example
 * // Set a value in a nested object
 * const obj = { a: { b: { c: 3 } } };
 * set(obj, 'a.b.c', 4);
 * console.log(obj.a.b.c); // 4
 *
 * @example
 * // Set a value in an array
 * const arr = [1, 2, 3];
 * set(arr, 1, 4);
 * console.log(arr[1]); // 4
 *
 * @example
 * // Create non-existent path and set value
 * const obj = {};
 * set(obj, 'a.b.c', 4);
 * console.log(obj); // { a: { b: { c: 4 } } }
 */
declare function set<R>(object: object, path: PropertyPath, value: any): R;
//#endregion
export { set };