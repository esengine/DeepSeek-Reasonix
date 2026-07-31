//#region src/fp/array/minBy.d.ts
/**
 * Creates a function that returns the element with the minimum selected number from a non-empty array.
 *
 * If the selector returns NaN, the element that produced NaN is returned, matching
 * the main {@link minBy} behavior.
 *
 * @template T - The type of elements in the array.
 * @param getValue - Selects a numeric value from each element, index, and array.
 * @returns A function that maps a non-empty readonly array to the minimum element.
 *
 * @example
 * import { minBy, pipe } from 'es-toolkit/fp';
 *
 * pipe([{ score: 10 }, { score: 20 }] as const, minBy(item => item.score));
 * // => { score: 10 }
 */
declare function minBy<T>(getValue: (element: T, index: number, array: readonly T[]) => number): (array: readonly [T, ...T[]]) => T;
/**
 * Creates a function that returns the element with the minimum selected number.
 *
 * Empty arrays return undefined. If the selector returns NaN, the element that
 * produced NaN is returned, matching the main {@link minBy} behavior.
 *
 * @template T - The type of elements in the array.
 * @param getValue - Selects a numeric value from each element, index, and array.
 * @returns A function that maps a readonly array to the minimum element, or undefined.
 *
 * @example
 * import { minBy, pipe } from 'es-toolkit/fp';
 *
 * pipe([] as Array<{ score: number }>, minBy(item => item.score));
 * // => undefined
 */
declare function minBy<T>(getValue: (element: T, index: number, array: readonly T[]) => number): (array: readonly T[]) => T | undefined;
//#endregion
export { minBy };