//#region src/array/groupBy.ts
/**
* Groups the elements of an array based on a provided key-generating function.
*
* This function takes an array and a function that generates a key from each element. It returns
* an object where the keys are the generated keys and the values are arrays of elements that share
* the same key.
*
* @template T - The type of elements in the array.
* @template K - The type of keys.
* @param arr - The array to group.
* @param getKeyFromItem - A function that generates a key from an element, its index, and the array.
* @returns An object where each key is associated with an array of elements that
* share that key.
*
* @example
* const array = [
*   { category: 'fruit', name: 'apple' },
*   { category: 'fruit', name: 'banana' },
*   { category: 'vegetable', name: 'carrot' }
* ];
* const result = groupBy(array, item => item.category);
* // result will be:
* // {
* //   fruit: [
* //     { category: 'fruit', name: 'apple' },
* //     { category: 'fruit', name: 'banana' }
* //   ],
* //   vegetable: [
* //     { category: 'vegetable', name: 'carrot' }
* //   ]
* // }
*
* @example
* // Using index parameter
* const items = ['a', 'b', 'c', 'd'];
* const result = groupBy(items, (item, index) => index % 2 === 0 ? 'even' : 'odd');
* // result will be: { even: ['a', 'c'], odd: ['b', 'd'] }
*/
function groupBy(arr, getKeyFromItem) {
	const result = {};
	for (let i = 0; i < arr.length; i++) {
		const item = arr[i];
		const key = getKeyFromItem(item, i, arr);
		if (!Object.hasOwn(result, key)) result[key] = [];
		result[key].push(item);
	}
	return result;
}
//#endregion
export { groupBy };
