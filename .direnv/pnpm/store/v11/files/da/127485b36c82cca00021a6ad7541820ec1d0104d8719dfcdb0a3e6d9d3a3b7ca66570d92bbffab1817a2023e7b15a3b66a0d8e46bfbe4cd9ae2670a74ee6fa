//#region src/object/cloneDeepWith.d.ts
/**
 * Deeply clones the given object.
 *
 * You can customize the deep cloning process using the `cloneValue` function.
 * The function takes the current value `value`, the property name `key`, and the entire object `obj` as arguments.
 * If the function returns a value, that value is used;
 * if it returns `undefined`, the default cloning method is used.
 *
 * @template T - The type of the object.
 * @param obj - The object to clone.
 * @param [cloneValue] - A function to customize the cloning process.
 * @returns A deep clone of the given object.
 *
 * @example
 * // Clone a primitive value
 * const num = 29;
 * const clonedNum = cloneDeepWith(num);
 * console.log(clonedNum); // 29
 * console.log(clonedNum === num); // true
 *
 * @example
 * // Clone an object with a customizer
 * const obj = { a: 1, b: 2 };
 * const clonedObj = cloneDeepWith(obj, (value) => {
 *   if (typeof value === 'number') {
 *     return value * 2; // Double the number
 *   }
 * });
 * console.log(clonedObj); // { a: 2, b: 4 }
 * console.log(clonedObj === obj); // false
 *
 * @example
 * // Clone an array with a customizer
 * const arr = [1, 2, 3];
 * const clonedArr = cloneDeepWith(arr, (value) => {
 *   return value + 1; // Increment each value
 * });
 * console.log(clonedArr); // [2, 3, 4]
 * console.log(clonedArr === arr); // false
 */
declare function cloneDeepWith<T>(obj: T, cloneValue: (value: any, key: PropertyKey | undefined, obj: T, stack: Map<any, any>) => any): T;
declare function cloneDeepWithImpl<T>(valueToClone: any, keyToClone: PropertyKey | undefined, objectToClone: T, stack?: Map<any, any>, cloneValue?: ((value: any, key: PropertyKey | undefined, obj: T, stack: Map<any, any>) => any) | undefined): T;
declare function copyProperties<T>(target: any, source: any, objectToClone?: T, stack?: Map<any, any> | undefined, cloneValue?: ((value: any, key: PropertyKey | undefined, obj: T, stack: Map<any, any>) => any) | undefined): void;
//#endregion
export { cloneDeepWith };