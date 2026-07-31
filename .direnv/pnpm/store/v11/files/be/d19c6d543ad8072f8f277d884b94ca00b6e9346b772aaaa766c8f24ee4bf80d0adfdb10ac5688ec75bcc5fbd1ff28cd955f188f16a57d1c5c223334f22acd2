//#region src/compat/function/bind.ts
/**
* Creates a function that invokes `func` with the `this` binding of `thisArg` and `partials` prepended to the arguments it receives.
*
* The `bind.placeholder` value, which defaults to a `symbol`, may be used as a placeholder for partially applied arguments.
*
* Note: Unlike native `Function#bind`, this method doesn't set the `length` property of bound functions.
*
* @param func - The function to bind.
* @param thisObj - The `this` binding of `func`.
* @param partialArgs - The arguments to be partially applied.
* @returns Returns the new bound function.
*
* @example
* function greet(greeting, punctuation) {
*   return greeting + ' ' + this.user + punctuation;
* }
* const object = { user: 'fred' };
* let bound = bind(greet, object, 'hi');
* bound('!');
* // => 'hi fred!'
*
* bound = bind(greet, object, bind.placeholder, '!');
* bound('hi');
* // => 'hi fred!'
*/
function bind(func, thisObj, ...partialArgs) {
	const bound = function(...providedArgs) {
		const args = [];
		let startIndex = 0;
		for (let i = 0; i < partialArgs.length; i++) {
			const arg = partialArgs[i];
			if (arg === bind.placeholder) args.push(providedArgs[startIndex++]);
			else args.push(arg);
		}
		for (let i = startIndex; i < providedArgs.length; i++) args.push(providedArgs[i]);
		if (this instanceof bound) return new func(...args);
		return func.apply(thisObj, args);
	};
	return bound;
}
bind.placeholder = Symbol("bind.placeholder");
//#endregion
exports.bind = bind;
