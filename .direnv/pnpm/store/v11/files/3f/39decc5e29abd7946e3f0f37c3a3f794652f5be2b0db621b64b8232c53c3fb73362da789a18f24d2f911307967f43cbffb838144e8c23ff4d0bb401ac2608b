//#region src/_internal/compareValues.ts
function nullishRank(value) {
	if (value === null) return 1;
	if (value === void 0) return 2;
	return 0;
}
function compareAscending(a, b) {
	const aRank = nullishRank(a);
	const bRank = nullishRank(b);
	if (aRank < bRank) return -1;
	if (aRank > bRank) return 1;
	if (aRank !== 0) return 0;
	if (a < b) return -1;
	if (a > b) return 1;
	return 0;
}
function compareValues(a, b, order) {
	return order === "asc" ? compareAscending(a, b) : compareAscending(b, a);
}
//#endregion
exports.compareValues = compareValues;
