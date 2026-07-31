//#region src/fp/array/zipObject.d.ts
/**
 * Creates a function that builds an object from the piped keys and configured values.
 *
 * Keys are read from the piped array and paired with values by index, matching the main
 * {@link zipObject} behavior.
 *
 * @template P - The property-key type from the piped keys.
 * @template V - The type of configured values.
 * @param values - Values assigned to the piped keys by index.
 * @returns A function that maps keys to an object.
 *
 * @example
 * import { pipe, zipObject } from 'es-toolkit/fp';
 *
 * pipe(['a', 'b'] as const, zipObject([1, 2]));
 * // => { a: 1, b: 2 }
 */
declare function zipObject<P extends PropertyKey, V>(values: readonly V[]): (keys: readonly P[]) => Record<P, V>;
//#endregion
export { zipObject };