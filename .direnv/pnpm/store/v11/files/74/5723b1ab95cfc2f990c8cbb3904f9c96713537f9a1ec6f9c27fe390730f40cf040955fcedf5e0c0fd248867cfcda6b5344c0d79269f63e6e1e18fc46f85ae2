//#region src/map/reduce.ts
/**
* Reduces a Map to a single value by iterating through its entries and applying a callback function.
*
* This function iterates through all entries of the Map and applies the callback function to each entry,
* accumulating the result. If an initial value is provided, it is used as the starting accumulator value.
* If no initial value is provided and the Map is empty, a TypeError is thrown.
*
* @template K - The type of keys in the Map.
* @template V - The type of values in the Map.
* @param map - The Map to reduce.
* @param callback - A function that processes each entry and updates the accumulator.
* @param [initialValue] - The initial value for the accumulator. If not provided, the first value in the Map is used.
* @returns The final accumulated value.
* @throws {TypeError} If the Map is empty and no initial value is provided.
*
* @example
* const map = new Map([
*   ['a', 1],
*   ['b', 2],
*   ['c', 3]
* ]);
* const result = reduce(map, (acc, value) => acc + value, 0);
* // result will be: 6
*
* @example
* const map = new Map([
*   ['a', 10],
*   ['b', 20]
* ]);
* const result = reduce(map, (acc, value) => acc + value);
* // result will be: 30 (starts with first value 10)
*/
function reduce(map, callback, initialValue) {
	if (initialValue == null && map.size === 0) throw new TypeError("Reduce of empty map with no initial value");
	let accumulator = initialValue;
	for (const [key, value] of map) if (accumulator == null) accumulator = value;
	else accumulator = callback(accumulator, value, key, map);
	return accumulator;
}
//#endregion
exports.reduce = reduce;
