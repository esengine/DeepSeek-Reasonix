const require_partialRight = require("../../function/partialRight.js");
//#region src/compat/function/partialRight.ts
/**
* This method is like `partial` except that partially applied arguments are appended to the arguments it receives.
*
* The partialRight.placeholder value, which defaults to a `symbol`, may be used as a placeholder for partially applied arguments.
*
* Note: This method doesn't set the `length` property of partially applied functions.
*
* @template F The type of the function to partially apply.
* @param func The function to partially apply arguments to.
* @param partialArgs The arguments to be partially applied.
* @returns Returns the new partially applied function.
*
* @example
* function greet(greeting, name) {
*   return greeting + ' ' + name;
* }
*
* const greetFred = partialRight(greet, 'fred');
* greetFred('hi');
* // => 'hi fred'
*
* // Partially applied with placeholders.
* const sayHelloTo = partialRight(greet, 'hello', partialRight.placeholder);
* sayHelloTo('fred');
* // => 'hello fred'
*/
function partialRight(func, ...partialArgs) {
	return require_partialRight.partialRightImpl(func, partialRight.placeholder, ...partialArgs);
}
partialRight.placeholder = Symbol("compat.partialRight.placeholder");
//#endregion
exports.partialRight = partialRight;
