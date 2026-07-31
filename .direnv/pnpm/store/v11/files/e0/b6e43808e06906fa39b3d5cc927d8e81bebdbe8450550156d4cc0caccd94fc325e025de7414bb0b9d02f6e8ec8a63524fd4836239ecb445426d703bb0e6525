//#region src/compat/_internal/mapToEntries.ts
function mapToEntries(map) {
	const arr = new Array(map.size);
	const keys = map.keys();
	const values = map.values();
	for (let i = 0; i < arr.length; i++) arr[i] = [keys.next().value, values.next().value];
	return arr;
}
//#endregion
export { mapToEntries };
