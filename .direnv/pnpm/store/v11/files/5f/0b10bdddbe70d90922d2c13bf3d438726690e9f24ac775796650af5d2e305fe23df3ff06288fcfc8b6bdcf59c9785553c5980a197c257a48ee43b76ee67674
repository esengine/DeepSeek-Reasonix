//#region src/compat/function/after.d.ts
/**
 * The opposite of `_.before`; this method creates a function that invokes
 * `func` once it's called `n` or more times.
 *
 * @template TFunc - The type of the function to be invoked.
 * @param n - The number of calls before `func` is invoked.
 * @param func - The function to restrict.
 * @returns Returns the new restricted function.
 * @throws {TypeError} - If `func` is not a function.
 *
 * @example
 * const saves = ['profile', 'settings'];
 * const done = after(saves.length, () => {
 *   console.log('done saving!');
 * });
 *
 * saves.forEach(type => {
 *   asyncSave({ 'type': type, 'complete': done });
 * });
 * // => Logs 'done saving!' after the two async saves have completed.
 */
declare function after<TFunc extends (...args: any[]) => any>(n: number, func: TFunc): TFunc;
//#endregion
export { after };