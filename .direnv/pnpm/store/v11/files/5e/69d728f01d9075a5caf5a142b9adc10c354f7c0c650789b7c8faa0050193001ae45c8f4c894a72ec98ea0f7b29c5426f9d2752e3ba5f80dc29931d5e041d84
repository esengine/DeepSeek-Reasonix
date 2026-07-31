import { styleText } from "node:util";
const warned = /* @__PURE__ */ new Set();
function warnOnce(msg) {
	if (!warned.has(msg)) {
		warned.add(msg);
		console.warn(styleText("yellow", `[@iconify-loader] ${msg}`));
	}
}
export { warnOnce };
