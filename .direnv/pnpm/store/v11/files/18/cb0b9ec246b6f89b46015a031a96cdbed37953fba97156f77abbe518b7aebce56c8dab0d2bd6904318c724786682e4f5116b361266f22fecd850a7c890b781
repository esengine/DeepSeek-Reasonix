//#region src/compat/object/functionsIn.d.ts
/**
 * Returns an array of property names whose values are functions, including inherited properties.
 *
 * @param object The object to inspect.
 * @returns Returns the function names.
 * @example
 *
 * function Foo() {
 *   this.a = function() { return 'a'; };
 *   this.b = function() { return 'b'; };
 * }
 *
 * Foo.prototype.c = function() { return 'c'; };
 *
 * functionsIn(new Foo);
 * // => ['a', 'b', 'c']
 */
declare function functionsIn<T extends {}>(object: any): string[];
//#endregion
export { functionsIn };