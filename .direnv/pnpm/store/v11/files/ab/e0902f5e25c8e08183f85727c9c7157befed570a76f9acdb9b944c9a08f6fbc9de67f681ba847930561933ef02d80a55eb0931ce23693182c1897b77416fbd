//#region src/compat/function/curry.ts
/**
* Creates a function that accepts arguments of `func` and either invokes `func` returning its result, if at least `arity` number of arguments have been provided, or returns a function that accepts the remaining `func` arguments, and so on.
* The arity of `func` may be specified if `func.length` is not sufficient.
*
* The `curry.placeholder` value, which defaults to a `symbol`, may be used as a placeholder for partially applied arguments.
*
* Note: This method doesn't set the `length` property of curried functions.
*
* @param func - The function to curry.
* @param arity - The arity of func.
* @param guard - Enables use as an iteratee for methods like `Array#map`.
* @returns} - Returns the new curried function.
*
* @example
* const abc = function(a, b, c) {
*   return Array.from(arguments);
* };
*
* let curried = curry(abc);
*
* curried(1)(2)(3);
* // => [1, 2, 3]
*
* curried(1, 2)(3);
* // => [1, 2, 3]
*
* curried(1, 2, 3);
* // => [1, 2, 3]
*
* // Curried with placeholders.
* curried(1)(curry.placeholder, 3)(2);
* // => [1, 2, 3]
*
* // Curried with arity.
* curried = curry(abc, 2);
*
* curried(1)(2);
* // => [1, 2]
*/
function curry(func, arity = func.length, guard) {
	arity = guard ? func.length : arity;
	arity = Number.parseInt(arity, 10);
	if (Number.isNaN(arity) || arity < 1) arity = 0;
	const wrapper = function(...partialArgs) {
		const holders = partialArgs.filter((item) => item === curry.placeholder);
		const length = partialArgs.length - holders.length;
		if (length < arity) return makeCurry(func, arity - length, partialArgs);
		if (this instanceof wrapper) return new func(...partialArgs);
		return func.apply(this, partialArgs);
	};
	wrapper.placeholder = curryPlaceholder;
	return wrapper;
}
function makeCurry(func, arity, partialArgs) {
	function wrapper(...providedArgs) {
		const holders = providedArgs.filter((item) => item === curry.placeholder);
		const length = providedArgs.length - holders.length;
		providedArgs = composeArgs(providedArgs, partialArgs);
		if (length < arity) return makeCurry(func, arity - length, providedArgs);
		if (this instanceof wrapper) return new func(...providedArgs);
		return func.apply(this, providedArgs);
	}
	wrapper.placeholder = curryPlaceholder;
	return wrapper;
}
function composeArgs(providedArgs, partialArgs) {
	const args = [];
	let startIndex = 0;
	for (let i = 0; i < partialArgs.length; i++) {
		const arg = partialArgs[i];
		if (arg === curry.placeholder && startIndex < providedArgs.length) args.push(providedArgs[startIndex++]);
		else args.push(arg);
	}
	for (let i = startIndex; i < providedArgs.length; i++) args.push(providedArgs[i]);
	return args;
}
const curryPlaceholder = Symbol("curry.placeholder");
curry.placeholder = curryPlaceholder;
//#endregion
exports.curry = curry;
