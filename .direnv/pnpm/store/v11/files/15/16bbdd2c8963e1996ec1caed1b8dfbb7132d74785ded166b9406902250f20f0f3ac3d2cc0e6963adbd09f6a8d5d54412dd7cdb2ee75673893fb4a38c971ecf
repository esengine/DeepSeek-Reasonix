import { limitAsync } from "./limitAsync.mjs";
//#region src/array/filterAsync.ts
/**
* Filters an array asynchronously using an async predicate function.
*
* Returns a promise that resolves to a new array containing only the elements
* for which the predicate function returns a truthy value.
*
* @template T - The type of elements in the array.
* @param array The array to filter.
* @param predicate An async function that tests each element.
* @param [options] Optional configuration object.
* @param [options.concurrency] Maximum number of concurrent async operations. If not specified, all operations run concurrently.
* @returns A promise that resolves to the filtered array.
* @example
* const users = [{ id: 1, active: true }, { id: 2, active: false }, { id: 3, active: true }];
* const activeUsers = await filterAsync(users, async (user) => {
*   return await checkUserStatus(user.id);
* });
* // Returns: [{ id: 1, active: true }, { id: 3, active: true }]
*
* @example
* // With concurrency limit
* const numbers = [1, 2, 3, 4, 5];
* const evenNumbers = await filterAsync(
*   numbers,
*   async (n) => await isEvenAsync(n),
*   { concurrency: 2 }
* );
* // Processes at most 2 operations concurrently
*/
async function filterAsync(array, predicate, options) {
	if (options?.concurrency != null) predicate = limitAsync(predicate, options.concurrency);
	const results = await Promise.all(array.map(predicate));
	return array.filter((_, index) => results[index]);
}
//#endregion
export { filterAsync };
