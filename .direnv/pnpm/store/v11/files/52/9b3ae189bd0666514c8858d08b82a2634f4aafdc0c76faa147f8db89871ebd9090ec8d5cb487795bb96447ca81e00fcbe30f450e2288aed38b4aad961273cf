import { trim as trim$1 } from "../../string/trim.mjs";
//#region src/compat/string/trim.ts
function trim(str, chars, guard) {
	if (str == null) return "";
	if (guard != null || chars == null) return str.toString().trim();
	switch (typeof chars) {
		case "object": if (Array.isArray(chars)) return trim$1(str, chars.flatMap((x) => x.toString().split("")));
		else return trim$1(str, chars.toString().split(""));
		default: return trim$1(str, chars.toString().split(""));
	}
}
//#endregion
export { trim };
