//#region src/compat/array/shuffle.d.ts
/**
 * Randomizes the order of elements in an `array` using the Fisher-Yates algorithm.
 *
 * This function takes an `array` and returns a new `array` with its elements shuffled in a random order.
 *
 * @template T - The type of elements in the `array`.
 * @param array - The `array` to shuffle.
 * @returns A new `array` with its elements shuffled in random order.
 */
declare function shuffle<T>(array: ArrayLike<T> | null | undefined): T[];
/**
 * Randomizes the order of elements in an `object` using the Fisher-Yates algorithm.
 *
 * This function takes an `object` and returns a new `object` with its values shuffled in a random order.
 *
 * @template T - The type of elements in the `object`.
 * @param object - The `object` to shuffle.
 * @returns A new `Array` with the values of the `object` shuffled in a random order.
 */
declare function shuffle<T extends object>(object: T | null | undefined): Array<T[keyof T]>;
//#endregion
export { shuffle };