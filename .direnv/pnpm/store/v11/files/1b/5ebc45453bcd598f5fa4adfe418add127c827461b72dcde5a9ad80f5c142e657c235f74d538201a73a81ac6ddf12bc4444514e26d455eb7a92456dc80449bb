//#region src/compat/util/method.d.ts
/**
 * Creates a function that invokes the method at `path` of a given object with the provided arguments.
 *
 * @param path - The path of the method to invoke.
 * @param args - The arguments to invoke the method with.
 * @returns Returns a new function that takes an object and invokes the method at `path` with `args`.
 *
 * @example
 * const object = {
 *   a: {
 *     b: function (x, y) {
 *       return x + y;
 *     }
 *   }
 * };
 *
 * const add = method('a.b', 1, 2);
 * console.log(add(object)); // => 3
 */
declare function method(path: PropertyKey | readonly PropertyKey[], ...args: any[]): (object: any) => any;
//#endregion
export { method };