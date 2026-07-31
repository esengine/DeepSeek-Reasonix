//#region src/fp/array/take.d.ts
/**
 * Creates a function that returns the first `count` elements of an array. If
 * `count` is greater than the array length, the whole array is returned. Use it
 * with {@link pipe}.
 *
 * The returned function is **lazy-capable** for a non-negative integer `count`:
 * inside a {@link pipe} it stops the walk as soon as `count` elements have been
 * collected, so preceding lazy functions never process the rest of the input.
 *
 * @template T - The type of elements in the array.
 * @param count - The number of elements to take from the front of the array.
 * @returns A function that maps a `readonly T[]` to a new `T[]`.
 *
 * @example
 * import { pipe, take } from 'es-toolkit/fp';
 *
 * pipe([1, 2, 3, 4, 5], take(3)); // => [1, 2, 3]
 *
 * @example
 * // Early termination: `map` only runs three times.
 * pipe([1, 2, 3, 4, 5], map(expensive), take(3));
 */
declare function take<T>(count: number): (array: readonly T[]) => T[];
//#endregion
export { take };