import { WindowedOptions } from "../../array/windowed.js";

//#region src/fp/array/windowed.d.ts
/**
 * Creates a function that returns sliding windows from an array.
 *
 * By default only full windows are returned. Pass partialWindows: true to include shorter
 * trailing windows. The full-window form is lazy-capable inside {@link pipe}.
 *
 * @template T - The type of elements in the array.
 * @param size - The size of each window. Must be a positive integer.
 * @param step - The step between window starts. Defaults to 1.
 * @param options - Window options.
 * @returns A function that maps a readonly array to sliding windows.
 * @throws {Error} When size or step is not a positive integer.
 *
 * @example
 * import { pipe, windowed } from 'es-toolkit/fp';
 *
 * pipe([1, 2, 3, 4], windowed(2));
 * // => [[1, 2], [2, 3], [3, 4]]
 */
declare function windowed<T>(size: number, step?: number, options?: WindowedOptions): (array: readonly T[]) => T[][];
//#endregion
export { windowed };