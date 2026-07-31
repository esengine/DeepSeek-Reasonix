//#region src/predicate/isPrimitive.d.ts
/**
 * Checks whether a value is a JavaScript primitive.
 * JavaScript primitives include null, undefined, strings, numbers, booleans, symbols, and bigints.
 *
 * @param value The value to check.
 * @returns Returns true if `value` is a primitive, false otherwise.
 *
 * @example
 * isPrimitive(null); // true
 * isPrimitive(undefined); // true
 * isPrimitive('123'); // true
 * isPrimitive(false); // true
 * isPrimitive(true); // true
 * isPrimitive(Symbol('a')); // true
 * isPrimitive(123n); // true
 * isPrimitive({}); // false
 * isPrimitive(new Date()); // false
 * isPrimitive(new Map()); // false
 * isPrimitive(new Set()); // false
 * isPrimitive([1, 2, 3]); // false
 */
declare function isPrimitive(value: unknown): value is null | undefined | string | number | boolean | symbol | bigint;
//#endregion
export { isPrimitive };