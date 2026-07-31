const require_AbortError = require("../error/AbortError.js");
//#region src/promise/delay.ts
/**
* Delays the execution of code for a specified number of milliseconds.
*
* This function returns a Promise that resolves after the specified delay, allowing you to use it
* with async/await to pause execution.
*
* @param ms - The number of milliseconds to delay.
* @param options - The options object.
* @param options.signal - An optional AbortSignal to cancel the delay.
* @returns A Promise that resolves after the specified delay.
*
* @example
* async function foo() {
*   console.log('Start');
*   await delay(1000); // Delays execution for 1 second
*   console.log('End');
* }
*
* foo();
*
* // With AbortSignal
* const controller = new AbortController();
* const { signal } = controller;
*
* setTimeout(() => controller.abort(), 50); // Will cancel the delay after 50ms
* try {
*   await delay(100, { signal });
*  } catch (error) {
*   console.error(error); // Will log 'AbortError'
*  }
* }
*/
function delay(ms, { signal } = {}) {
	return new Promise((resolve, reject) => {
		const abortError = () => {
			reject(new require_AbortError.AbortError());
		};
		const abortHandler = () => {
			clearTimeout(timeoutId);
			abortError();
		};
		if (signal?.aborted) return abortError();
		const timeoutId = setTimeout(() => {
			signal?.removeEventListener("abort", abortHandler);
			resolve();
		}, ms);
		signal?.addEventListener("abort", abortHandler, { once: true });
	});
}
//#endregion
exports.delay = delay;
