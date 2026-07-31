//#region src/compat/object/create.d.ts
/**
 * Creates an object that inherits from the prototype object.
 *
 * If `properties` are provided, they will be added to the new object.
 * Only string-keyed enumerable properties directly owned by the `properties` object are copied.
 * Inherited properties or those with `Symbol` keys are not copied.
 *
 * @template T - The prototype object type.
 * @template U - The properties object type.
 * @param prototype - The object to inherit from.
 * @param properties - The properties to assign to the created object.
 * @returns The new object.
 */
declare function create<T extends object, U extends object>(prototype: T, properties?: U): T & U;
//#endregion
export { create };