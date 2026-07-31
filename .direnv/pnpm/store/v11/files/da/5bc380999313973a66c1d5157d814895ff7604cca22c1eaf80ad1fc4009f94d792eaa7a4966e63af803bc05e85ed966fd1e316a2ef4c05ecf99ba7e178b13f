//#region src/function/partial.ts
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
	return partialImpl(func, placeholderSymbol, ...partialArgs);
}
function partialImpl(func, placeholder, ...partialArgs) {
	const partialed = function(...providedArgs) {
		let providedArgsIndex = 0;
		const substitutedArgs = partialArgs.slice().map((arg) => arg === placeholder ? providedArgs[providedArgsIndex++] : arg);
		const remainingArgs = providedArgs.slice(providedArgsIndex);
		return func.apply(this, substitutedArgs.concat(remainingArgs));
	};
	if (func.prototype) partialed.prototype = Object.create(func.prototype);
	return partialed;
}
const placeholderSymbol = Symbol("partial.placeholder");
partial.placeholder = placeholderSymbol;
//#endregion
export { partial, partialImpl };
