import { keys } from "./keys.mjs";
//#region src/compat/object/functions.ts
/**
* Creates an array of property names from an object where the property values are functions.
*
* Only checks for own properties with string keys. Inherited properties or
* properties with Symbol keys are not included.
*
* @param object The object to inspect.
* @returns An array of function property names.
*
* @example
*
* function Foo() {
*   this.a = () => 'a'
*   this.b = () => 'b'
* }
*
* Foo.prototype.c = () => 'c'
*
* functions(new Foo)
* // => ['a', 'b']
*/
function functions(object) {
	if (object == null) return [];
	return keys(object).filter((key) => typeof object[key] === "function");
}
//#endregion
export { functions };
