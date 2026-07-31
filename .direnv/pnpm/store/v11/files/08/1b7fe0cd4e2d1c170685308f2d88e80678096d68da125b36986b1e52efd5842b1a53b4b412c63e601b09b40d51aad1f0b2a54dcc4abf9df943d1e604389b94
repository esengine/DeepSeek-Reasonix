//#region src/map/mapKeys.ts
/**
* Creates a new Map with the same values but with keys transformed by the provided function.
*
* This function takes a Map and a function that generates a new key from each value-key pair.
* It returns a new Map where the keys are the result of applying the function to each entry,
* while the values remain the same.
*
* @template K - The type of keys in the Map.
* @template V - The type of values in the Map.
* @param map - The Map to transform.
* @param getNewKey - A function that generates a new key from a value-key pair.
* @returns A new Map with transformed keys and the same values.
*
* @example
* const map = new Map([
*   ['a', 1],
*   ['b', 2],
*   ['c', 3]
* ]);
* const result = mapKeys(map, (value, key) => key.toUpperCase());
* // result will be:
* // Map(3) {
* //   'A' => 1,
* //   'B' => 2,
* //   'C' => 3
* // }
*/
function mapKeys(map, getNewKey) {
	const result = /* @__PURE__ */ new Map();
	for (const [key, value] of map) {
		const newKey = getNewKey(value, key, map);
		result.set(newKey, value);
	}
	return result;
}
//#endregion
exports.mapKeys = mapKeys;
