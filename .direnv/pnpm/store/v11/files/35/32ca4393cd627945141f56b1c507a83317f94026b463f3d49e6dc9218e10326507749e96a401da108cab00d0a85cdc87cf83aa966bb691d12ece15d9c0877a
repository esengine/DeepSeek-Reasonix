//#region src/compat/util/defaultTo.ts
/**
* Returns the default value for `null`, `undefined`, and `NaN`.
*
* @template T - The type of the value parameter
* @template D - The type of the defaultValue parameter
* @param value - The value to check.
* @param defaultValue - The default value to return if the first value is null, undefined, or NaN.
* @returns Returns either the first value or the default value.
*
* @example
* defaultTo(null, 'default') // returns 'default'
* defaultTo(undefined, 42) // returns 42
* defaultTo(NaN, 0) // returns 0
* defaultTo('actual', 'default') // returns 'actual'
* defaultTo(123, 0) // returns 123
*/
function defaultTo(value, defaultValue) {
	if (value == null || Number.isNaN(value)) return defaultValue;
	return value;
}
//#endregion
export { defaultTo };
