//#region src/compat/_internal/getSymbols.ts
function getSymbols(object) {
	return Object.getOwnPropertySymbols(object).filter((symbol) => Object.prototype.propertyIsEnumerable.call(object, symbol));
}
//#endregion
exports.getSymbols = getSymbols;
