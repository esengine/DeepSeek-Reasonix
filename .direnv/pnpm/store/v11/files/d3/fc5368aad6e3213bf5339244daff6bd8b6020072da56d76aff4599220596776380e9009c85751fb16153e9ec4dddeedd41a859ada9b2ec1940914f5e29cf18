//#region src/array/forEachAsync.d.ts
interface ForEachAsyncOptions {
  concurrency?: number;
}
/**
 * Executes an async callback function for each element in an array.
 *
 * Unlike the native `forEach`, this function returns a promise that resolves
 * when all async operations complete. It supports optional concurrency limiting.
 *
 * @template T - The type of elements in the array.
 * @param array The array to iterate over.
 * @param callback An async function to execute for each element.
 * @param [options] Optional configuration object.
 * @param [options.concurrency] Maximum number of concurrent async operations. If not specified, all operations run concurrently.
 * @returns A promise that resolves when all operations complete.
 * @example
 * const users = [{ id: 1 }, { id: 2 }, { id: 3 }];
 * await forEachAsync(users, async (user) => {
 *   await updateUser(user.id);
 * });
 * // All users have been updated
 *
 * @example
 * // With concurrency limit
 * const items = [1, 2, 3, 4, 5];
 * await forEachAsync(
 *   items,
 *   async (item) => await processItem(item),
 *   { concurrency: 2 }
 * );
 * // Processes at most 2 items concurrently
 */
declare function forEachAsync<T>(array: readonly T[], callback: (item: T, index: number, array: readonly T[]) => Promise<void>, options?: ForEachAsyncOptions): Promise<void>;
//#endregion
export { forEachAsync };