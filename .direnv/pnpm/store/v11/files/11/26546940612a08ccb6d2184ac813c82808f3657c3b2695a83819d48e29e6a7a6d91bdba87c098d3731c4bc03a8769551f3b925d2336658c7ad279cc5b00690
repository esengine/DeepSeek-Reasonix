import { toInteger } from "../util/toInteger.mjs";
//#region src/compat/function/after.ts
/**
* The opposite of `_.before`; this method creates a function that invokes
* `func` once it's called `n` or more times.
*
* @template TFunc - The type of the function to be invoked.
* @param n - The number of calls before `func` is invoked.
* @param func - The function to restrict.
* @returns Returns the new restricted function.
* @throws {TypeError} - If `func` is not a function.
*
* @example
* const saves = ['profile', 'settings'];
* const done = after(saves.length, () => {
*   console.log('done saving!');
* });
*
* saves.forEach(type => {
*   asyncSave({ 'type': type, 'complete': done });
* });
* // => Logs 'done saving!' after the two async saves have completed.
*/
function after(n, func) {
	if (typeof func !== "function") throw new TypeError("Expected a function");
	n = toInteger(n);
	return function(...args) {
		if (--n < 1) return func.apply(this, args);
	};
}
//#endregion
export { after };
