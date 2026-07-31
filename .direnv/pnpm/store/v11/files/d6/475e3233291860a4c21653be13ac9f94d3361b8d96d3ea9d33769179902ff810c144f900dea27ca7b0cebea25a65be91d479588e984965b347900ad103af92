import { Many } from "../_internal/Many.js";
import { PropertyPath } from "../_internal/PropertyPath.js";

//#region src/compat/object/pick.d.ts
/**
 * Creates a new object composed of the picked object properties.
 *
 * @template T - The type of object.
 * @template U - The type of keys to pick.
 * @param object - The object to pick keys from.
 * @param props - An array of keys to be picked from the object.
 * @returns A new object with the specified keys picked.
 *
 * @example
 * const obj = { a: 1, b: 2, c: 3 };
 * const result = pick(obj, ['a', 'c']);
 * // result will be { a: 1, c: 3 }
 */
declare function pick<T extends object, U extends keyof T>(object: T, ...props: Array<Many<U>>): Pick<T, U>;
/**
 * Creates a new object composed of the picked object properties.
 *
 * @template T - The type of object.
 * @param object - The object to pick keys from.
 * @param props - An array of keys to be picked from the object.
 * @returns A new object with the specified keys picked.
 *
 * @example
 * const obj = { a: 1, b: 2, c: 3 };
 * const result = pick(obj, ['a', 'c']);
 * // result will be { a: 1, c: 3 }
 */
declare function pick<T>(object: T | null | undefined, ...props: Array<Many<PropertyPath>>): Partial<T>;
//#endregion
export { pick };