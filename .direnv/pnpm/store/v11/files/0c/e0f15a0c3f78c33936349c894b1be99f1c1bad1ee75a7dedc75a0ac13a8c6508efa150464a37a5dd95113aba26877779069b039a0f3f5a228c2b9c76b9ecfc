//#region src/compat/util/toLength.d.ts
/**
 * Converts the value to a valid index. A valid index is an integer that is greater than or equal to `0` and less than or equal to `2^32 - 1`.
 *
 * It converts the given value to an integer. If the value is less than `0`, it returns `0`. If the value exceeds `2^32 - 1`, it returns `2^32 - 1`. Values that cannot be converted to a number (such as `NaN` or non-numeric strings) return `0`.
 *
 * @param value - The value to convert to a valid index.
 * @returns The converted value.
 *
 * @example
 * toLength(3.2)  // => 3
 * toLength(-1)   // => 0
 * toLength(1.9)  // => 1
 * toLength('42') // => 42
 * toLength(null) // => 0
 * toLength('a')  // => 0
 */
declare function toLength(value: any): number;
//#endregion
export { toLength };