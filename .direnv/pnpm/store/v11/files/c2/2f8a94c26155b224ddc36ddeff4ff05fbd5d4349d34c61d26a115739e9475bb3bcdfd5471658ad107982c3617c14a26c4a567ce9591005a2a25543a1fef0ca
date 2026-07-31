import { debounce } from "./debounce.mjs";
//#region src/compat/function/throttle.ts
/**
* Creates a throttled function that only invokes the provided function at most once
* per every `throttleMs` milliseconds. Subsequent calls to the throttled function
* within the wait time will not trigger the execution of the original function.
*
* @template F - The type of function.
* @param func - The function to throttle.
* @param throttleMs - The number of milliseconds to throttle executions to.
* @param options - The options object
* @param options.signal - An optional AbortSignal to cancel the throttled function.
* @param options.leading - If `true`, the function will be invoked on the leading edge of the timeout.
* @param options.trailing - If `true`, the function will be invoked on the trailing edge of the timeout.
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
function throttle(func, throttleMs = 0, options = {}) {
	const { leading = true, trailing = true } = options;
	return debounce(func, throttleMs, {
		leading,
		maxWait: throttleMs,
		trailing
	});
}
//#endregion
export { throttle };
