import { getSymbols } from "./getSymbols.mjs";
//#region src/compat/_internal/getSymbolsIn.ts
function getSymbolsIn(object) {
	const result = [];
	while (object) {
		result.push(...getSymbols(object));
		object = Object.getPrototypeOf(object);
	}
	return result;
}
//#endregion
export { getSymbolsIn };
