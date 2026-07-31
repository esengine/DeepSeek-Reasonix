//#region src/fp/array/maxBy.d.ts
/**
 * Creates a function that returns the element with the maximum selected number from a non-empty array.
 *
 * If the selector returns NaN, the element that produced NaN is returned, matching
 * the main {@link maxBy} behavior.
 *
 * @template T - The type of elements in the array.
 * @param getValue - Selects a numeric value from each element, index, and array.
 * @returns A function that maps a non-empty readonly array to the maximum element.
 *
 * @example
 * import { maxBy, pipe } from 'es-toolkit/fp';
 *
 * pipe([{ score: 10 }, { score: 20 }] as const, maxBy(item => item.score));
 * // => { score: 20 }
 */
declare function maxBy<T>(getValue: (element: T, index: number, array: readonly T[]) => number): (array: readonly [T, ...T[]]) => T;
/**
 * Creates a function that returns the element with the maximum selected number.
 *
 * Empty arrays return undefined. If the selector returns NaN, the element that
 * produced NaN is returned, matching the main {@link maxBy} behavior.
 *
 * @template T - The type of elements in the array.
 * @param getValue - Selects a numeric value from each element, index, and array.
 * @returns A function that maps a readonly array to the maximum element, or undefined.
 *
 * @example
 * import { maxBy, pipe } from 'es-toolkit/fp';
 *
 * pipe([] as Array<{ score: number }>, maxBy(item => item.score));
 * // => undefined
 */
declare function maxBy<T>(getValue: (element: T, index: number, array: readonly T[]) => number): (array: readonly T[]) => T | undefined;
//#endregion
export { maxBy };