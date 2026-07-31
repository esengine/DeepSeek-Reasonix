import { defaultIconProps } from "../icon/defaults.js";
import { generateItemCSSRules, generateItemContent, getCommonCSSRules } from "./common.js";
import { formatCSS } from "./format.js";
/**
* Get CSS for icon, rendered as background or mask
*/
function getIconCSS(icon, options = {}) {
	const body = options.customise ? options.customise(icon.body) : icon.body;
	const mode = options.mode || (options.color || !body.includes("currentColor") ? "background" : "mask");
	let varName = options.varName;
	if (varName === void 0 && mode === "mask") varName = "svg";
	const newOptions = {
		...options,
		mode,
		varName
	};
	if (mode === "background") delete newOptions.varName;
	const rules = {
		...options.rules,
		...getCommonCSSRules(newOptions),
		...generateItemCSSRules({
			...defaultIconProps,
			...icon,
			body
		}, newOptions)
	};
	return formatCSS([{
		selector: options.iconSelector || ".icon",
		rules
	}], newOptions.format);
}
/**
* Get CSS for icon, rendered as content
*/
function getIconContentCSS(icon, options) {
	const body = options.customise ? options.customise(icon.body) : icon.body;
	const content = generateItemContent({
		...defaultIconProps,
		...icon,
		body
	}, options);
	return formatCSS([{
		selector: options.iconSelector || ".icon::after",
		rules: {
			...options.rules,
			content
		}
	}], options.format);
}
export { getIconCSS, getIconContentCSS };
