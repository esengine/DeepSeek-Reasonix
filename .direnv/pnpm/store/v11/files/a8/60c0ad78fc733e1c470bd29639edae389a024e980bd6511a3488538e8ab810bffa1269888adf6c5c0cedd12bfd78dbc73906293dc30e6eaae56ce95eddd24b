import { defaultIconCustomisations } from "../customisations/defaults.js";
import { getIconData } from "../icon-set/get-icon.js";
import { iconToSVG } from "../svg/build.js";
import { loaderDefaultHeightProp, loaderDefaultWidthProp, mergeIconProps } from "./utils.js";
async function searchForIcon(iconSet, collection, ids, options) {
	let iconData;
	const { customize } = options?.customizations ?? {};
	for (const id of ids) {
		iconData = getIconData(iconSet, id);
		if (iconData) {
			let defaultCustomizations = { ...defaultIconCustomisations };
			if (typeof customize === "function") {
				iconData = Object.assign({}, iconData);
				defaultCustomizations = customize(defaultCustomizations, iconData, `${collection}:${id}`) ?? defaultCustomizations;
			}
			const { attributes: { width, height, ...restAttributes }, body } = iconToSVG(iconData, defaultCustomizations);
			return await mergeIconProps(`<svg >${body}</svg>`, collection, id, options, () => {
				return {
					...restAttributes,
					...width ? { [loaderDefaultWidthProp]: width } : {},
					...height ? { [loaderDefaultHeightProp]: height } : {}
				};
			});
		}
	}
}
export { searchForIcon };
