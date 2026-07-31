//#region src/compat/function/flip.ts
/**
* Reverses the order of arguments for a given function.
*
* @template F - The type of the function being flipped.
* @param func - The function whose arguments will be reversed.
* @returns A new function that takes the
* reversed arguments and returns the result of calling `func`.
*
* @example
* function fn(a: string, b: string, c: string, d: string) {
*   return [a, b, c, d];
* }
*
* const flipped = flip(fn);
* flipped('a', 'b', 'c', 'd'); // => ['d', 'c', 'b', 'a']
*/
function flip(func) {
	return function(...args) {
		return func.apply(this, args.reverse());
	};
}
//#endregion
export { flip };
