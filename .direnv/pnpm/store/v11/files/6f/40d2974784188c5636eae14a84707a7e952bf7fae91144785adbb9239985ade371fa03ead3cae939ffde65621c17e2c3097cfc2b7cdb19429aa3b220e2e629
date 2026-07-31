import { debounce } from "./debounce.mjs";
//#region src/function/throttle.ts
/**
* Creates a throttled function that only invokes the provided function at most once
* per every `throttleMs` milliseconds. Subsequent calls to the throttled function
* within the wait time will not trigger the execution of the original function.
*
* @template F - The type of function.
* @param func - The function to throttle.
* @param throttleMs - The number of milliseconds to throttle executions to.
* @returns A new throttled function that accepts the same parameters as the original function.
*
* @example
* const throttledFunction = throttle(() => {
*   console.log('Function executed');
* }, 1000);
*
* // Will log 'Function executed' immediately
* throttledFunction();
*
* // Will not log anything as it is within the throttle time
* throttledFunction();
*
* // After 1 second
* setTimeout(() => {
*   throttledFunction(); // Will log 'Function executed'
* }, 1000);
*/
function throttle(func, throttleMs, { signal, edges = ["leading", "trailing"] } = {}) {
	let pendingAt = null;
	const debounced = debounce(function(...args) {
		pendingAt = Date.now();
		func.apply(this, args);
	}, throttleMs, {
		signal,
		edges
	});
	const throttled = function(...args) {
		if (pendingAt == null) pendingAt = Date.now();
		if (Date.now() - pendingAt >= throttleMs) {
			pendingAt = Date.now();
			func.apply(this, args);
			debounced.cancel();
			debounced.schedule();
			return;
		}
		debounced.apply(this, args);
	};
	throttled.cancel = debounced.cancel;
	throttled.flush = debounced.flush;
	return throttled;
}
//#endregion
export { throttle };
