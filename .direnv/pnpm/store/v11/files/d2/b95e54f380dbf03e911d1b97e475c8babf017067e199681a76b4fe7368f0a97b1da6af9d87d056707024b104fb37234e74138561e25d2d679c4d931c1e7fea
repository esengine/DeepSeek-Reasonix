import { PropertyPath } from "../_internal/PropertyPath.mjs";

//#region src/compat/object/updateWith.d.ts
/**
 * Updates the value at the specified path of the given object using an updater function and a customizer.
 * If any part of the path does not exist, it will be created.
 *
 * @template T - The type of the object.
 * @param object - The object to modify.
 * @param path - The path of the property to update.
 * @param updater - The function to produce the updated value.
 * @param customizer - The function to customize the update process.
 * @returns The modified object.
 *
 * @example
 * const object = { 'a': [{ 'b': { 'c': 3 } }] };
 * updateWith(object, 'a[0].b.c', (n) => n * n);
 * // => { 'a': [{ 'b': { 'c': 9 } }] }
 */
declare function updateWith<T extends object>(object: T, path: PropertyPath, updater: (oldValue: any) => any, customizer?: (value: any, key: string, object: T) => any): T;
/**
 * Updates the value at the specified path of the given object using an updater function and a customizer.
 * If any part of the path does not exist, it will be created.
 *
 * @template T - The type of the object.
 * @template R - The type of the return value.
 * @param object - The object to modify.
 * @param path - The path of the property to update.
 * @param updater - The function to produce the updated value.
 * @param customizer - The function to customize the update process.
 * @returns The modified object.
 *
 * @example
 * const object = { 'a': [{ 'b': { 'c': 3 } }] };
 * updateWith(object, 'a[0].b.c', (n) => n * n);
 * // => { 'a': [{ 'b': { 'c': 9 } }] }
 */
declare function updateWith<T extends object, R>(object: T, path: PropertyPath, updater: (oldValue: any) => any, customizer?: (value: any, key: string, object: T) => any): R;
//#endregion
export { updateWith };