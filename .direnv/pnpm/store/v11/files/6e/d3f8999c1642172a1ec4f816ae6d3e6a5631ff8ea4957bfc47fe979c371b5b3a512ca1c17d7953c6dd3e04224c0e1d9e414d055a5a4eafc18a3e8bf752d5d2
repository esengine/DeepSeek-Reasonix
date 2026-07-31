const require_TimeoutError = require("../error/TimeoutError.js");
//#region src/promise/timeout.ts
/**
* Returns a promise that rejects with a `TimeoutError` after a specified delay.
*
* You can pass an `AbortSignal` to cancel the timeout. Unlike most `AbortSignal`-aware
* APIs, aborting does **not** reject the promise. A `timeout` only exists to lose a
* `Promise.race`, so cancelling it leaves the promise pending forever, allowing the
* operation it guards to settle on its own. The underlying timer and abort listener
* are cleared on abort, so nothing is leaked.
*
* @param ms - The delay duration in milliseconds.
* @param options - The options object.
* @param options.signal - An optional AbortSignal to cancel the timeout. When aborted, the returned promise never settles.
* @returns A promise that rejects with a `TimeoutError` after the specified delay, or never settles if aborted.
* @throws {TimeoutError} Throws a `TimeoutError` after the specified delay.
*
* @example
* try {
*   await timeout(1000); // Timeout exception after 1 second
* } catch (error) {
*   console.error(error); // Will log 'The operation was timed out'
* }
*
* @example
* // Cancelling the timeout lifts the time limit instead of throwing.
* const controller = new AbortController();
* setTimeout(() => controller.abort(), 50);
*
* const result = await Promise.race([
*   doWork(),
*   timeout(1000, { signal: controller.signal }), // never rejects once aborted
* ]);
*/
function timeout(ms, { signal } = {}) {
	return new Promise((_resolve, reject) => {
		const abortHandler = () => {
			clearTimeout(timeoutId);
		};
		if (signal?.aborted) return;
		const timeoutId = setTimeout(() => {
			signal?.removeEventListener("abort", abortHandler);
			reject(new require_TimeoutError.TimeoutError());
		}, ms);
		signal?.addEventListener("abort", abortHandler, { once: true });
	});
}
//#endregion
exports.timeout = timeout;
