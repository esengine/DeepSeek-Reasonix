const require_limitAsync = require("./limitAsync.js");
//#region src/array/mapAsync.ts
/**
* Transforms each element in an array using an async callback function and returns
* a promise that resolves to an array of transformed values.
*
* @template T - The type of elements in the input array.
* @template R - The type of elements in the output array.
* @param array The array to transform.
* @param callback An async function that transforms each element.
* @param [options] Optional configuration object.
* @param [options.concurrency] Maximum number of concurrent async operations. If not specified, all operations run concurrently.
* @returns A promise that resolves to an array of transformed values.
* @example
* const users = [{ id: 1 }, { id: 2 }, { id: 3 }];
* const userDetails = await mapAsync(users, async (user) => {
*   return await fetchUserDetails(user.id);
* });
* // Returns: [{ id: 1, name: '...' }, { id: 2, name: '...' }, { id: 3, name: '...' }]
*
* @example
* // With concurrency limit
* const numbers = [1, 2, 3, 4, 5];
* const results = await mapAsync(
*   numbers,
*   async (n) => await slowOperation(n),
*   { concurrency: 2 }
* );
* // Processes at most 2 operations concurrently
*/
function mapAsync(array, callback, options) {
	if (options?.concurrency != null) callback = require_limitAsync.limitAsync(callback, options.concurrency);
	return Promise.all(array.map(callback));
}
//#endregion
exports.mapAsync = mapAsync;
