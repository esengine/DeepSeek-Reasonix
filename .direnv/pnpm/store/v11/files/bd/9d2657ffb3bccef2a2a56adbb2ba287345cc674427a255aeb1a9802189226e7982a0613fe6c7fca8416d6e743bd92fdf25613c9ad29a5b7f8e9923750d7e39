//#region src/compat/function/defer.ts
/**
* Defers invoking the `func` until the current call stack has cleared. Any additional arguments are provided to func when it's invoked.
*
* @param func The function to defer.
* @param args The arguments to invoke `func` with.
* @returns Returns the timer id.
*
* @example
* defer((text) => {
*   console.log(text);
* }, 'deferred');
* // => Logs 'deferred' after the current call stack has cleared.
*/
function defer(func, ...args) {
	if (typeof func !== "function") throw new TypeError("Expected a function");
	return setTimeout(func, 1, ...args);
}
//#endregion
export { defer };
