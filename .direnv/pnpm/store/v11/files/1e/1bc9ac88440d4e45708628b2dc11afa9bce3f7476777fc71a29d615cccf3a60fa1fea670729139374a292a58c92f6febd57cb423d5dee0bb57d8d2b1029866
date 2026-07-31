//#region src/compat/function/bindKey.ts
/**
* Creates a function that invokes the method at `object[key]` with `partialArgs` prepended to the arguments it receives.
*
* This method differs from `bind` by allowing bound functions to reference methods that may be redefined or don't yet exist.
*
* The `bindKey.placeholder` value, which defaults to a `symbol`, may be used as a placeholder for partially applied arguments.
*
* @template T - The type of the object to bind.
* @template K - The type of the key to bind.
* @param object - The object to invoke the method on.
* @param key - The key of the method.
* @param partialArgs - The arguments to be partially applied.
* @returns Returns the new bound function.
*
* @example
* const object = {
*   user: 'fred',
*   greet: function (greeting, punctuation) {
*     return greeting + ' ' + this.user + punctuation;
*   },
* };
*
* let bound = bindKey(object, 'greet', 'hi');
* bound('!');
* // => 'hi fred!'
*
* object.greet = function (greeting, punctuation) {
*   return greeting + 'ya ' + this.user + punctuation;
* };
*
* bound('!');
* // => 'hiya fred!'
*
* // Bound with placeholders.
* bound = bindKey(object, 'greet', bindKey.placeholder, '!');
* bound('hi');
* // => 'hiya fred!'
*/
function bindKey(object, key, ...partialArgs) {
	const bound = function(...providedArgs) {
		const args = [];
		let startIndex = 0;
		for (let i = 0; i < partialArgs.length; i++) {
			const arg = partialArgs[i];
			if (arg === bindKey.placeholder) args.push(providedArgs[startIndex++]);
			else args.push(arg);
		}
		for (let i = startIndex; i < providedArgs.length; i++) args.push(providedArgs[i]);
		if (this instanceof bound) return new object[key](...args);
		return object[key].apply(object, args);
	};
	return bound;
}
bindKey.placeholder = Symbol("bindKey.placeholder");
//#endregion
export { bindKey };
