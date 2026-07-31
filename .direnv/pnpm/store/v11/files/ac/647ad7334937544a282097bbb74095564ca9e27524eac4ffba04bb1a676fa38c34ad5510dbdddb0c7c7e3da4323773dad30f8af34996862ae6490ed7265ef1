//#region src/map/mapValues.ts
/**
* Creates a new Map with the same keys but with values transformed by the provided function.
*
* This function takes a Map and a function that generates a new value from each value-key pair.
* It returns a new Map where the values are the result of applying the function to each entry,
* while the keys remain the same.
*
* @template K - The type of keys in the Map.
* @template V - The type of values in the Map.
* @param map - The Map to transform.
* @param getNewValue - A function that generates a new value from a value-key pair.
* @returns A new Map with the same keys and transformed values.
*
* @example
* const map = new Map([
*   ['a', 1],
*   ['b', 2],
*   ['c', 3]
* ]);
* const result = mapValues(map, (value) => value * 2);
* // result will be:
* // Map(3) {
* //   'a' => 2,
* //   'b' => 4,
* //   'c' => 6
* // }
*/
function mapValues(map, getNewValue) {
	const result = /* @__PURE__ */ new Map();
	for (const [key, value] of map) {
		const newValue = getNewValue(value, key, map);
		result.set(key, newValue);
	}
	return result;
}
//#endregion
exports.mapValues = mapValues;
