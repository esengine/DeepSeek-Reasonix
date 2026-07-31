const require_isArray = require("../compat/predicate/isArray.js");
const require_isPlainObject = require("../compat/predicate/isPlainObject.js");
const require_snakeCase = require("../string/snakeCase.js");
//#region src/object/toSnakeCaseKeys.ts
/**
* Creates a new object composed of the properties with keys converted to snake_case.
*
* This function takes an object and returns a new object that includes the same properties,
* but with all keys converted to snake_case format.
*
* @template T - The type of object.
* @param obj - The object to convert keys from.
* @returns A new object with all keys converted to snake_case.
*
* @example
* // Example with objects
* const obj = { userId: 1, firstName: 'John' };
* const result = toSnakeCaseKeys(obj);
* // result will be { user_id: 1, first_name: 'John' }
*
* // Example with arrays of objects
* const arr = [
*   { userId: 1, firstName: 'John' },
*   { userId: 2, firstName: 'Jane' }
* ];
* const arrResult = toSnakeCaseKeys(arr);
* // arrResult will be [{ user_id: 1, first_name: 'John' }, { user_id: 2, first_name: 'Jane' }]
*
* // Example with nested objects
* const nested = {
*   userData: {
*     userId: 1,
*     userAddress: {
*       streetName: 'Main St',
*       zipCode: '12345'
*     }
*   }
* };
* const nestedResult = toSnakeCaseKeys(nested);
* // nestedResult will be:
* // {
* //   user_data: {
* //     user_id: 1,
* //     user_address: {
* //       street_name: 'Main St',
* //       zip_code: '12345'
* //     }
* //   }
* // }
*/
function toSnakeCaseKeys(obj) {
	if (require_isArray.isArray(obj)) return obj.map((item) => toSnakeCaseKeys(item));
	if (require_isPlainObject.isPlainObject(obj)) {
		const result = {};
		const keys = Object.keys(obj);
		for (let i = 0; i < keys.length; i++) {
			const key = keys[i];
			const snakeKey = require_snakeCase.snakeCase(key);
			result[snakeKey] = toSnakeCaseKeys(obj[key]);
		}
		return result;
	}
	return obj;
}
//#endregion
exports.toSnakeCaseKeys = toSnakeCaseKeys;
