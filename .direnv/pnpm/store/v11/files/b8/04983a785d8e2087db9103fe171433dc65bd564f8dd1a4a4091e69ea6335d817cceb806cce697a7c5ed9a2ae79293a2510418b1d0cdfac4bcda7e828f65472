//#region src/compat/object/unset.d.ts
/**
 * Removes the property at the given path of the object.
 *
 * @param obj - The object to modify.
 * @param path - The path of the property to unset.
 * @returns Returns true if the property is deleted, else false.
 *
 * @example
 * const obj = { a: { b: { c: 42 } } };
 * unset(obj, 'a.b.c'); // true
 * console.log(obj); // { a: { b: {} } }
 *
 * @example
 * const obj = { a: { b: { c: 42 } } };
 * unset(obj, ['a', 'b', 'c']); // true
 * console.log(obj); // { a: { b: {} } }
 */
declare function unset(obj: any, path: PropertyKey | readonly PropertyKey[]): boolean;
//#endregion
export { unset };