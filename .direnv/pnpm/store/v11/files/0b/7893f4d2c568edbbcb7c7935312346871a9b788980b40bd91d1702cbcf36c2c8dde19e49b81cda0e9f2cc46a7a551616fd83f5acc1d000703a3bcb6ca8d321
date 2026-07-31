//#region src/compat/util/toSafeInteger.d.ts
/**
 * Converts `value` to a safe integer.
 *
 * A safe integer can be compared and represented correctly.
 *
 * @param value - The value to convert.
 * @returns Returns the value converted to a safe integer.
 *
 * @example
 * toSafeInteger(3.2); // => 3
 * toSafeInteger(Number.MAX_VALUE); // => 9007199254740991
 * toSafeInteger(Infinity); // => 9007199254740991
 * toSafeInteger('3.2'); // => 3
 * toSafeInteger(NaN); // => 0
 * toSafeInteger(null); // => 0
 * toSafeInteger(-Infinity); // => -9007199254740991
 */
declare function toSafeInteger(value: any): number;
//#endregion
export { toSafeInteger };