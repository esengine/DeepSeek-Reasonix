//#region src/function/once.ts
/**
* Creates a function that is restricted to invoking the provided function `func` once.
* Repeated calls to the function will return the value from the first invocation.
*
* @template F - The type of function.
* @param func - The function to restrict.
* @returns A new function that invokes `func` once and caches the result.
*
* @example
* const initialize = once(() => {
*   console.log('Initialized!');
*   return true;
* });
*
* initialize(); // Logs: 'Initialized!' and returns true
* initialize(); // Returns true without logging
*/
function once(func) {
	let called = false;
	let cache;
	return function(...args) {
		if (!called) {
			called = true;
			cache = func(...args);
		}
		return cache;
	};
}
//#endregion
export { once };
