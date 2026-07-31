//#region src/compat/object/assign.d.ts
/**
 * Assigns properties from one source object to a target object.
 *
 * @template T - The type of the target object.
 * @template U - The type of the source object.
 * @param object - The target object to which properties will be assigned.
 * @param source - The source object whose properties will be assigned to the target object.
 * @returns The updated target object with properties from the source object assigned.
 *
 * @example
 * const target = { a: 1, b: 2 };
 * const source = { b: 3, c: 4 };
 * const result = assign(target, source);
 * // => { a: 1, b: 3, c: 4 }
 */
declare function assign<T, U>(object: T, source: U): T & U;
/**
 * Assigns properties from two source objects to a target object.
 *
 * @template T - The type of the target object.
 * @template U - The type of the first source object.
 * @template V - The type of the second source object.
 * @param object - The target object to which properties will be assigned.
 * @param source1 - The first source object whose properties will be assigned to the target object.
 * @param source2 - The second source object whose properties will be assigned to the target object.
 * @returns The updated target object with properties from the source objects assigned.
 *
 * @example
 * const target = { a: 1 };
 * const source1 = { b: 2 };
 * const source2 = { c: 3 };
 * const result = assign(target, source1, source2);
 * // => { a: 1, b: 2, c: 3 }
 */
declare function assign<T, U, V>(object: T, source1: U, source2: V): T & U & V;
/**
 * Assigns properties from three source objects to a target object.
 *
 * @template T - The type of the target object.
 * @template U - The type of the first source object.
 * @template V - The type of the second source object.
 * @template W - The type of the third source object.
 * @param object - The target object to which properties will be assigned.
 * @param source1 - The first source object whose properties will be assigned to the target object.
 * @param source2 - The second source object whose properties will be assigned to the target object.
 * @param source3 - The third source object whose properties will be assigned to the target object.
 * @returns The updated target object with properties from the source objects assigned.
 *
 * @example
 * const target = { a: 1 };
 * const source1 = { b: 2 };
 * const source2 = { c: 3 };
 * const source3 = { d: 4 };
 * const result = assign(target, source1, source2, source3);
 * // => { a: 1, b: 2, c: 3, d: 4 }
 */
declare function assign<T, U, V, W>(object: T, source1: U, source2: V, source3: W): T & U & V & W;
/**
 * Assigns properties from four source objects to a target object.
 *
 * @template T - The type of the target object.
 * @template U - The type of the first source object.
 * @template V - The type of the second source object.
 * @template W - The type of the third source object.
 * @template X - The type of the fourth source object.
 * @param object - The target object to which properties will be assigned.
 * @param source1 - The first source object whose properties will be assigned to the target object.
 * @param source2 - The second source object whose properties will be assigned to the target object.
 * @param source3 - The third source object whose properties will be assigned to the target object.
 * @param source4 - The fourth source object whose properties will be assigned to the target object.
 * @returns The updated target object with properties from the source objects assigned.
 *
 * @example
 * const target = { a: 1 };
 * const source1 = { b: 2 };
 * const source2 = { c: 3 };
 * const source3 = { d: 4 };
 * const source4 = { e: 5 };
 * const result = assign(target, source1, source2, source3, source4);
 * // => { a: 1, b: 2, c: 3, d: 4, e: 5 }
 */
declare function assign<T, U, V, W, X>(object: T, source1: U, source2: V, source3: W, source4: X): T & U & V & W & X;
/**
 * Assigns properties from a target object to itself.
 *
 * @template T - The type of the target object.
 * @param object - The target object.
 * @returns The target object.
 *
 * @example
 * const target = { a: 1, b: 2 };
 * const result = assign(target);
 * // => { a: 1, b: 2 }
 */
declare function assign<T>(object: T): T;
/**
 * Assigns properties from multiple source objects to a target object.
 *
 * @param object - The target object to which properties will be assigned.
 * @param otherArgs - The source objects whose properties will be assigned to the target object.
 * @returns The updated target object with properties from the source objects assigned.
 *
 * @example
 * const target = { a: 1 };
 * const result = assign(target, { b: 2 }, { c: 3 }, { d: 4 });
 * // => { a: 1, b: 2, c: 3, d: 4 }
 */
declare function assign(object: any, ...otherArgs: any[]): any;
//#endregion
export { assign };