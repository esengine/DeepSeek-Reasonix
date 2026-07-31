const require_iteratee = require("../util/iteratee.js");
//#region src/compat/array/pullAllBy.ts
/**
* Removes all specified values from an array using an iteratee function.
*
* This function changes `arr` in place.
* If you want to remove values without modifying the original array, use `differenceBy`.
*
* @template T
* @param arr - The array to modify.
* @param valuesToRemove - The values to remove from the array.
* @param getValue - The key of the property to match against each element.
* @returns The modified array with the specified values removed.
*
* @example
* // Using a iteratee function
* const items = [{ value: 1 }, { value: 2 }, { value: 3 }, { value: 1 }];
* const result = pullAllBy(items, [{ value: 1 }, { value: 3 }], obj => obj.value);
* console.log(result); // [{ value: 2 }]
*
* // Using a property name
* const items = [{ value: 1 }, { value: 2 }, { value: 3 }, { value: 1 }];
* const result = pullAllBy(items, [{ value: 1 }, { value: 3 }], 'value');
* console.log(result); // [{ value: 2 }]
*/
function pullAllBy(arr, valuesToRemove, _getValue) {
	const getValue = require_iteratee.iteratee(_getValue);
	const valuesSet = new Set(Array.from(valuesToRemove).map((x) => getValue(x)));
	let resultIndex = 0;
	for (let i = 0; i < arr.length; i++) {
		const value = getValue(arr[i]);
		if (valuesSet.has(value)) continue;
		if (!Object.hasOwn(arr, i)) {
			delete arr[resultIndex++];
			continue;
		}
		arr[resultIndex++] = arr[i];
	}
	arr.length = resultIndex;
	return arr;
}
//#endregion
exports.pullAllBy = pullAllBy;
