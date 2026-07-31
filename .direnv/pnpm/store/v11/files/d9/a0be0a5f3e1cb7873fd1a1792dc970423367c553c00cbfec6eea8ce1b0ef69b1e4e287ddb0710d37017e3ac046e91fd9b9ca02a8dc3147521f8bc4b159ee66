import { partialImpl } from "../../function/partial.mjs";
//#region src/compat/function/partial.ts
/**
* Creates a function that invokes `func` with `partialArgs` prepended to the arguments it receives. This method is like `bind` except it does not alter the `this` binding.
*
* The partial.placeholder value, which defaults to a `symbol`, may be used as a placeholder for partially applied arguments.
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
* const sayHelloTo = partial(greet, 'hello');
* sayHelloTo('fred');
* // => 'hello fred'
*
* // Partially applied with placeholders.
* const greetFred = partial(greet, partial.placeholder, 'fred');
* greetFred('hi');
* // => 'hi fred'
*/
function partial(func, ...partialArgs) {
	return partialImpl(func, partial.placeholder, ...partialArgs);
}
partial.placeholder = Symbol("compat.partial.placeholder");
//#endregion
export { partial };
