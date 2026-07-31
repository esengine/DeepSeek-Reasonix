//#region src/compat/object/functions.d.ts
/**
 * Creates an array of property names from an object where the property values are functions.
 *
 * @param object - The object to inspect.
 * @returns An array of function property names.
 *
 * @example
 * function Foo() {
 *   this.a = () => 'a';
 *   this.b = () => 'b';
 * }
 *
 * Foo.prototype.c = () => 'c';
 *
 * functions(new Foo);
 * // => ['a', 'b']
 */
declare function functions(object: any): string[];
//#endregion
export { functions };