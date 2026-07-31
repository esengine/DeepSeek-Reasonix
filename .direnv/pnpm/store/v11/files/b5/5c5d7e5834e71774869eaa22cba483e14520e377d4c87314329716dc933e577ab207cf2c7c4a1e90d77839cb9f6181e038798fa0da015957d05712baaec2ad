//#region src/object/omitBy.ts
/**
* Creates a new object composed of the properties that do not satisfy the predicate function.
*
* This function takes an object and a predicate function, and returns a new object that
* includes only the properties for which the predicate function returns false.
*
* @template T - The type of object.
* @param obj - The object to omit properties from.
* @param shouldOmit - A predicate function that determines
* whether a property should be omitted. It takes the property's key and value as arguments and returns `true`
* if the property should be omitted, and `false` otherwise.
* @returns A new object with the properties that do not satisfy the predicate function.
*
* @example
* const obj = { a: 1, b: 'omit', c: 3 };
* const shouldOmit = (value) => typeof value === 'string';
* const result = omitBy(obj, shouldOmit);
* // result will be { a: 1, c: 3 }
*/
function omitBy(obj, shouldOmit) {
	const result = {};
	const keys = Object.keys(obj);
	for (let i = 0; i < keys.length; i++) {
		const key = keys[i];
		const value = obj[key];
		if (!shouldOmit(value, key)) result[key] = value;
	}
	return result;
}
//#endregion
export { omitBy };
