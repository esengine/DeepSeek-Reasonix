import { identity } from "../../function/identity.mjs";
import { isFunction } from "../../predicate/isFunction.mjs";
//#region src/compat/function/wrap.ts
/**
* Creates a new function that wraps the given function `func`.
* In this process, you can apply additional logic defined in the `wrapper` function before and after the execution of the original function.
*
* If a `value` is provided instead of a function, this value is passed as the first argument to the `wrapper` function.
*
* @template T - The type of the value being wrapped.
* @template U - The type of the arguments being passed to the `wrapper` function.
* @template V - The type of the return value of the `wrapper` function.
* @param value - The value to be wrapped.
* @param wrapper - The function to wrap the value with.
* @returns A new function that wraps the value with the `wrapper` function.
*
* @example
* // Wrap a function
* const greet = (name: string) => `Hi, ${name}`;
* const wrapped = wrap(greet, (value, name) => `[LOG] ${value(name)}`);
* wrapped('Bob'); // => "[LOG] Hi, Bob"
*
* @example
* // Wrap a primitive value
* const wrapped = wrap('value', v => `<p>${v}</p>`);
* wrapped(); // => "<p>value</p>"
*/
function wrap(value, wrapper) {
	return function(...args) {
		return (isFunction(wrapper) ? wrapper : identity).apply(this, [value, ...args]);
	};
}
//#endregion
export { wrap };
