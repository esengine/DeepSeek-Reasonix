import { invoke } from "./invoke.mjs";
//#region src/compat/util/methodOf.ts
/**
* Creates a function that invokes the method at a given path of `object` with the provided arguments.
*
* @param object - The object to query.
* @param args - The arguments to invoke the method with.
* @returns Returns a new function that takes a path and invokes the method at `path` with `args`.
*
* @example
* const object = {
*  a: {
*   b: function (x, y) {
*    return x + y;
*    }
*   }
* };
*
* const add = methodOf(object, 1, 2);
* console.log(add('a.b')); // => 3
*/
function methodOf(object, ...args) {
	return function(path) {
		return invoke(object, path, args);
	};
}
//#endregion
export { methodOf };
