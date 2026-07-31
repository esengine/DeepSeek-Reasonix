//#region src/array/differenceBy.ts
/**
* Computes the difference between two arrays after mapping their elements through a provided function.
*
* This function takes two arrays and a mapper function. It returns a new array containing the elements
* that are present in the first array but not in the second array, based on the identity calculated
* by the mapper function.
*
* Essentially, it filters out any elements from the first array that, when
* mapped, match an element in the mapped version of the second array.
*
* @template T, U
* @param firstArr - The primary array from which to derive the difference.
* @param secondArr - The array containing elements to be excluded from the first array.
* @param mapper - The function to map the elements of both arrays. This function
* is applied to each element in both arrays, and the comparison is made based on the mapped values.
* @returns A new array containing the elements from the first array that do not have a corresponding
* mapped identity in the second array.
*
* @example
* const array1 = [{ id: 1 }, { id: 2 }, { id: 3 }];
* const array2 = [{ id: 2 }, { id: 4 }];
* const mapper = item => item.id;
* const result = differenceBy(array1, array2, mapper);
* // result will be [{ id: 1 }, { id: 3 }] since the elements with id 2 are in both arrays and are excluded from the result.
*
* @example
* const array1 = [{ id: 1 }, { id: 2 }, { id: 3 }];
* const array2 = [2, 4];
* const mapper = item => (typeof item === 'object' ? item.id : item);
* const result = differenceBy(array1, array2, mapper);
* // result will be [{ id: 1 }, { id: 3 }] since 2 is present in both arrays after mapping, and is excluded from the result.
*/
function differenceBy(firstArr, secondArr, mapper) {
	const mappedSecondSet = new Set(secondArr.map((item) => mapper(item)));
	return firstArr.filter((item) => {
		return !mappedSecondSet.has(mapper(item));
	});
}
//#endregion
exports.differenceBy = differenceBy;
