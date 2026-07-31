import { isPlainObject } from "../predicate/isPlainObject.mjs";
import { isArray } from "../compat/predicate/isArray.mjs";
import { camelCase } from "../string/camelCase.mjs";
//#region src/object/toCamelCaseKeys.ts
/**
* Creates a new object composed of the properties with keys converted to camelCase.
*
* This function takes an object and returns a new object that includes the same properties,
* but with all keys converted to camelCase format.
*
* @template T - The type of object.
* @param obj - The object to convert keys from.
* @returns A new object with all keys converted to camelCase.
*
* @example
* // Example with objects
* const obj = { user_id: 1, first_name: 'John' };
* const result = toCamelCaseKeys(obj);
* // result will be { userId: 1, firstName: 'John' }
*
* // Example with arrays of objects
* const arr = [
*   { user_id: 1, first_name: 'John' },
*   { user_id: 2, first_name: 'Jane' }
* ];
* const arrResult = toCamelCaseKeys(arr);
* // arrResult will be [{ userId: 1, firstName: 'John' }, { userId: 2, firstName: 'Jane' }]
*
* // Example with nested objects
* const nested = {
*   user_data: {
*     user_id: 1,
*     user_address: {
*       street_name: 'Main St',
*       zip_code: '12345'
*     }
*   }
* };
* const nestedResult = toCamelCaseKeys(nested);
* // nestedResult will be:
* // {
* //   userData: {
* //     userId: 1,
* //     userAddress: {
* //       streetName: 'Main St',
* //       zipCode: '12345'
* //     }
* //   }
* // }
*/
function toCamelCaseKeys(obj) {
	if (isArray(obj)) return obj.map((item) => toCamelCaseKeys(item));
	if (isPlainObject(obj)) {
		const result = {};
		const keys = Object.keys(obj);
		for (let i = 0; i < keys.length; i++) {
			const key = keys[i];
			const camelKey = camelCase(key);
			result[camelKey] = toCamelCaseKeys(obj[key]);
		}
		return result;
	}
	return obj;
}
//#endregion
export { toCamelCaseKeys };
