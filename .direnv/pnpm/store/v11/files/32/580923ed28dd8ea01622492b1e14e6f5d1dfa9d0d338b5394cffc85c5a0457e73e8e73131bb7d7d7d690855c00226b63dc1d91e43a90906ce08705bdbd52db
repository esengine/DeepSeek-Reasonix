import { calculateSize } from "../svg/size.js";
import { isUnsetKeyword } from "../svg/build.js";
const svgWidthRegex = /\swidth\s*=\s*["']([^"']+)["']/;
const svgHeightRegex = /\sheight\s*=\s*["']([^"']+)["']/;
const svgViewBoxRegex = /\sviewBox\s*=\s*["']([^"']+)["']/;
const svgTagRegex = /<svg\s+/;
const loaderDefaultWidthProp = "__iconify_loader_width";
const loaderDefaultHeightProp = "__iconify_loader_height";
function stringifySize(value) {
	return value === void 0 ? value : value.toString();
}
function getConfiguredSize(source, scale) {
	if (typeof scale === "number") return scale > 0 ? stringifySize(calculateSize(source ?? "1em", scale)) : void 0;
	return source;
}
function getSvgAspectRatio(svgNode, props) {
	const viewBox = props.viewBox ?? svgViewBoxRegex.exec(svgNode)?.[1];
	if (!viewBox) return;
	const values = viewBox.trim().split(/[\s,]+/).map((value) => parseFloat(value));
	if (values.length !== 4 || values.some((value) => !Number.isFinite(value))) return;
	const width = values[2];
	const height = values[3];
	if (width <= 0 || height <= 0) return;
	return width / height;
}
function configureSvgSize(svg, props, scale) {
	const svgNode = svg.slice(0, svg.indexOf(">"));
	const widthOnSvg = svgWidthRegex.test(svgNode);
	const heightOnSvg = svgHeightRegex.test(svgNode);
	const defaultWidth = props["__iconify_loader_width"] ?? svgWidthRegex.exec(svgNode)?.[1];
	const defaultHeight = props["__iconify_loader_height"] ?? svgHeightRegex.exec(svgNode)?.[1];
	const aspectRatio = getSvgAspectRatio(svgNode, props);
	delete props[loaderDefaultWidthProp];
	delete props[loaderDefaultHeightProp];
	const customWidth = props.width;
	const customHeight = props.height;
	const hasCustomWidth = !!customWidth || isUnsetKeyword(customWidth);
	const hasCustomHeight = !!customHeight || isUnsetKeyword(customHeight);
	if (hasCustomWidth || hasCustomHeight) {
		if (!hasCustomWidth) if (isUnsetKeyword(customHeight)) {
			delete props.width;
			delete props.height;
		} else if (aspectRatio) {
			props.width = stringifySize(calculateSize(customHeight, aspectRatio));
			props.height = customHeight;
		} else delete props.width;
		else if (isUnsetKeyword(customWidth)) {
			delete props.width;
			if (!hasCustomHeight || isUnsetKeyword(customHeight)) delete props.height;
		}
		if (!hasCustomHeight) if (isUnsetKeyword(customWidth)) delete props.height;
		else if (aspectRatio) {
			props.height = stringifySize(calculateSize(customWidth, 1 / aspectRatio));
			props.width = customWidth;
		} else delete props.height;
		else if (isUnsetKeyword(customHeight)) delete props.height;
	} else {
		const width = getConfiguredSize(defaultWidth, scale);
		const height = getConfiguredSize(defaultHeight, scale);
		if (width) props.width = width;
		if (height) props.height = height;
	}
	return [widthOnSvg, heightOnSvg];
}
async function mergeIconProps(svg, collection, icon, options, propsProvider, afterCustomizations) {
	const { scale, addXmlNs = false } = options ?? {};
	const { additionalProps = {}, iconCustomizer } = options?.customizations ?? {};
	const props = await propsProvider?.() ?? {};
	await iconCustomizer?.(collection, icon, props);
	Object.keys(additionalProps).forEach((p) => {
		const v = additionalProps[p];
		if (v !== void 0 && v !== null) props[p] = v;
	});
	afterCustomizations?.(props);
	const [widthOnSvg, heightOnSvg] = configureSvgSize(svg, props, scale);
	if (addXmlNs) {
		if (!svg.includes("xmlns=") && !props["xmlns"]) props["xmlns"] = "http://www.w3.org/2000/svg";
		if (!svg.includes("xmlns:xlink=") && svg.includes("xlink:") && !props["xmlns:xlink"]) props["xmlns:xlink"] = "http://www.w3.org/1999/xlink";
	}
	const propsToAdd = Object.keys(props).map((p) => p === "width" && widthOnSvg || p === "height" && heightOnSvg ? null : `${p}="${props[p]}"`).filter((p) => p != null);
	if (propsToAdd.length) svg = svg.replace(svgTagRegex, `<svg ${propsToAdd.join(" ")} `);
	if (options) {
		const { defaultStyle, defaultClass } = options;
		if (defaultClass && !svg.includes("class=")) svg = svg.replace(svgTagRegex, `<svg class="${defaultClass}" `);
		if (defaultStyle && !svg.includes("style=")) svg = svg.replace(svgTagRegex, `<svg style="${defaultStyle}" `);
	}
	const usedProps = options?.usedProps;
	if (usedProps) {
		Object.keys(additionalProps).forEach((p) => {
			const v = props[p];
			if (v !== void 0 && v !== null) usedProps[p] = v;
		});
		if (typeof props.width !== "undefined" && props.width !== null) usedProps.width = props.width;
		if (typeof props.height !== "undefined" && props.height !== null) usedProps.height = props.height;
	}
	return svg;
}
function getPossibleIconNames(icon) {
	return [
		icon,
		icon.replace(/([a-z])([A-Z])/g, "$1-$2").toLowerCase(),
		icon.replace(/([a-z])(\d+)/g, "$1-$2")
	];
}
export { getPossibleIconNames, loaderDefaultHeightProp, loaderDefaultWidthProp, mergeIconProps };
