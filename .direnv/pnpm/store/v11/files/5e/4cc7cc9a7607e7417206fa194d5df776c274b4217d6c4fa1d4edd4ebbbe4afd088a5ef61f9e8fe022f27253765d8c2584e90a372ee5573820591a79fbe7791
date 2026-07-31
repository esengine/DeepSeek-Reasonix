//#region src/compat/function/defer.d.ts
/**
 * Defers invoking the `func` until the current call stack has cleared. Any additional arguments are provided to func when it's invoked.
 *
 * @param func The function to defer.
 * @param args The arguments to invoke `func` with.
 * @returns Returns the timer id.
 *
 * @example
 * defer(console.log, 'deferred');
 * // => Logs 'deferred' after the current call stack has cleared.
 */
declare function defer(func: (...args: any[]) => any, ...args: any[]): number;
//#endregion
export { defer };