const require_semaphore = require("../promise/semaphore.js");
//#region src/array/limitAsync.ts
/**
* Wraps an async function to limit the number of concurrent executions.
*
* This function creates a wrapper around an async callback that ensures at most
* `concurrency` number of executions can run simultaneously. Additional calls will
* wait until a slot becomes available.
*
* @template F - The type of the async function to wrap.
* @param callback The async function to wrap with concurrency control.
* @param concurrency Maximum number of concurrent executions allowed.
* @returns A wrapped version of the callback with concurrency limiting.
* @example
* const limitedFetch = limitAsync(async (url) => {
*   return await fetch(url);
* }, 3);
*
* // Only 3 fetches will run concurrently
* const urls = ['url1', 'url2', 'url3', 'url4', 'url5'];
* await Promise.all(urls.map(url => limitedFetch(url)));
*
* @example
* const processItem = async (item) => {
*   // Expensive async operation
*   return await heavyComputation(item);
* };
*
* const limitedProcess = limitAsync(processItem, 2);
* const items = [1, 2, 3, 4, 5];
* // At most 2 items will be processed concurrently
* await Promise.all(items.map(item => limitedProcess(item)));
*/
function limitAsync(callback, concurrency) {
	const semaphore = new require_semaphore.Semaphore(concurrency);
	return async function(...args) {
		try {
			await semaphore.acquire();
			return await callback.apply(this, args);
		} finally {
			semaphore.release();
		}
	};
}
//#endregion
exports.limitAsync = limitAsync;
