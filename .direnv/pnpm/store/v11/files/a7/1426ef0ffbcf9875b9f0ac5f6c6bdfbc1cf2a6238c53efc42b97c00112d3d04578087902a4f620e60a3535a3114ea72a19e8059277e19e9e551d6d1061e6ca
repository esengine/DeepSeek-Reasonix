//#region src/function/partialRight.ts
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
	return partialRightImpl(func, placeholderSymbol, ...partialArgs);
}
function partialRightImpl(func, placeholder, ...partialArgs) {
	const partialedRight = function(...providedArgs) {
		const placeholderLength = partialArgs.filter((arg) => arg === placeholder).length;
		const rangeLength = Math.max(providedArgs.length - placeholderLength, 0);
		const remainingArgs = providedArgs.slice(0, rangeLength);
		let providedArgsIndex = rangeLength;
		const substitutedArgs = partialArgs.slice().map((arg) => arg === placeholder ? providedArgs[providedArgsIndex++] : arg);
		return func.apply(this, remainingArgs.concat(substitutedArgs));
	};
	if (func.prototype) partialedRight.prototype = Object.create(func.prototype);
	return partialedRight;
}
const placeholderSymbol = Symbol("partialRight.placeholder");
partialRight.placeholder = placeholderSymbol;
//#endregion
export { partialRight, partialRightImpl };
