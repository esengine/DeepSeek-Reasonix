const require_timeout = require("./timeout.js");
//#region src/promise/withTimeout.ts
/**
* Executes an async function and enforces a timeout.
*
* If the promise does not resolve within the specified time,
* the timeout will trigger and the returned promise will be rejected.
*
* You can pass an `AbortSignal` to cancel the timeout. Aborting the signal lifts the
* time limit: the timeout stops counting and `run`'s promise is awaited without a
* deadline. It does not reject the returned promise or abort `run` itself — pass the
* same signal into `run` if you also want to cancel the underlying work.
*
* @template T
* @param run - A function that returns a promise to be executed.
* @param ms - The timeout duration in milliseconds.
* @param options - The options object.
* @param options.signal - An optional AbortSignal to cancel the timeout. When aborted, the time limit is lifted.
* @returns A promise that resolves with the result of the `run` function or rejects if the timeout is reached.
*
* @example
* async function fetchData() {
*   const response = await fetch('https://example.com/data');
*   return response.json();
* }
*
* try {
*   const data = await withTimeout(fetchData, 1000);
*   console.log(data); // Logs the fetched data if `fetchData` is resolved within 1 second.
* } catch (error) {
*   console.error(error); // Will log 'TimeoutError' if `fetchData` is not resolved within 1 second.
* }
*
* @example
* // Lift the time limit when the user opts to keep waiting.
* const controller = new AbortController();
* keepWaitingButton.onclick = () => controller.abort();
*
* const data = await withTimeout(fetchData, 1000, { signal: controller.signal });
*/
async function withTimeout(run, ms, { signal } = {}) {
	return Promise.race([run(), require_timeout.timeout(ms, { signal })]);
}
//#endregion
exports.withTimeout = withTimeout;
