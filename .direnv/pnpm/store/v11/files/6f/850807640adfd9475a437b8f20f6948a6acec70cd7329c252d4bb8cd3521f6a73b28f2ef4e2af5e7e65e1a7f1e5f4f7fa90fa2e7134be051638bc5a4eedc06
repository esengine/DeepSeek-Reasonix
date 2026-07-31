//#region src/compat/util/iteratee.d.ts
/**
 * Returns the provided function as-is when it is a function type.
 *
 * @template F - The function type
 * @param func - The function to return
 * @returns Returns the provided function unchanged
 *
 * @example
 * const fn = (x: number) => x * 2;
 * const iterateeFn = iteratee(fn);
 * iterateeFn(4); // => 8
 */
declare function iteratee<F extends (...args: any[]) => any>(func: F): F;
/**
 * Creates an iteratee function based on the provided property key or object.
 * If given a property key, returns a function that gets that property from objects.
 * If given an object, returns a function that matches objects against the provided one.
 *
 * @param func - The value to convert to an iteratee
 * @returns Returns the iteratee function
 *
 * @example
 * // With property key
 * const getLength = iteratee('length');
 * getLength([1,2,3]); // => 3
 *
 * // With object
 * const matchObj = iteratee({ x: 1, y: 2 });
 * matchObj({ x: 1, y: 2, z: 3 }); // => true
 */
declare function iteratee(func: PropertyKey | object): (...args: any[]) => any;
//#endregion
export { iteratee };