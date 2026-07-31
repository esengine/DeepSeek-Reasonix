//#region src/function/debounce.ts
/**
* Creates a debounced function that delays invoking the provided function until after `debounceMs` milliseconds
* have elapsed since the last time the debounced function was invoked. The debounced function also has a `cancel`
* method to cancel any pending execution.
*
* @template F - The type of function.
* @param func - The function to debounce.
* @param debounceMs - The number of milliseconds to delay.
* @param options - The options object
* @param options.signal - An optional AbortSignal to cancel the debounced function.
* @param options.edges - An optional array specifying whether the function should be invoked on the leading edge, trailing edge, or both.
* @returns A new debounced function with a `cancel` method.
*
* @example
* const debouncedFunction = debounce(() => {
*   console.log('Function executed');
* }, 1000);
*
* // Will log 'Function executed' after 1 second if not called again in that time
* debouncedFunction();
*
* // Will not log anything as the previous call is canceled
* debouncedFunction.cancel();
*
* // With AbortSignal
* const controller = new AbortController();
* const signal = controller.signal;
* const debouncedWithSignal = debounce(() => {
*  console.log('Function executed');
* }, 1000, { signal });
*
* debouncedWithSignal();
*
* // Will cancel the debounced function call
* controller.abort();
*/
function debounce(func, debounceMs, { signal, edges } = {}) {
	let pendingThis = void 0;
	let pendingArgs = null;
	const leading = edges != null && edges.includes("leading");
	const trailing = edges == null || edges.includes("trailing");
	const invoke = () => {
		if (pendingArgs !== null) {
			func.apply(pendingThis, pendingArgs);
			pendingThis = void 0;
			pendingArgs = null;
		}
	};
	const onTimerEnd = () => {
		if (trailing) invoke();
		cancel();
	};
	let timeoutId = null;
	const schedule = () => {
		if (timeoutId != null) clearTimeout(timeoutId);
		timeoutId = setTimeout(() => {
			timeoutId = null;
			onTimerEnd();
		}, debounceMs);
	};
	const cancelTimer = () => {
		if (timeoutId !== null) {
			clearTimeout(timeoutId);
			timeoutId = null;
		}
	};
	const cancel = () => {
		cancelTimer();
		pendingThis = void 0;
		pendingArgs = null;
	};
	const flush = () => {
		invoke();
	};
	const debounced = function(...args) {
		if (signal?.aborted) return;
		pendingThis = this;
		pendingArgs = args;
		const isFirstCall = timeoutId == null;
		schedule();
		if (leading && isFirstCall) invoke();
	};
	debounced.schedule = schedule;
	debounced.cancel = cancel;
	debounced.flush = flush;
	signal?.addEventListener("abort", cancel, { once: true });
	return debounced;
}
//#endregion
exports.debounce = debounce;
