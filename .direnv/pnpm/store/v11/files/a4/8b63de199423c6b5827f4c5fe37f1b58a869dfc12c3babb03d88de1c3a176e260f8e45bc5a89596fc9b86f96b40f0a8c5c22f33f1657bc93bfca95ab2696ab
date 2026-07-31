const require_isFunction = require("../../predicate/isFunction.js");
//#region src/compat/object/functionsIn.ts
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
function functionsIn(object) {
	if (object == null) return [];
	const result = [];
	for (const key in object) if (require_isFunction.isFunction(object[key])) result.push(key);
	return result;
}
//#endregion
exports.functionsIn = functionsIn;
