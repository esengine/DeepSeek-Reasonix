import {
  getConfig,
  sanitizeText
} from "./chunk-WYO6CB5R.mjs";
import {
  log
} from "./chunk-X3CZISLH.mjs";
import {
  __name
} from "./chunk-Y2CYZVJY.mjs";

// src/rendering-util/icons.ts
import { getIconData, iconToHTML, iconToSVG, replaceIDs, stringToIcon } from "@iconify/utils";
var unknownIcon = {
  body: '<g><rect width="80" height="80" style="fill: #087ebf; stroke-width: 0px;"/><text transform="translate(21.16 64.67)" style="fill: #fff; font-family: ArialMT, Arial; font-size: 67.75px;"><tspan x="0" y="0">?</tspan></text></g>',
  height: 80,
  width: 80
};
var iconsStore = /* @__PURE__ */ new Map();
var loaderStore = /* @__PURE__ */ new Map();
var registerIconPacks = /* @__PURE__ */ __name((iconLoaders) => {
  for (const iconLoader of iconLoaders) {
    if (!iconLoader.name) {
      throw new Error(
        'Invalid icon loader. Must have a "name" property with non-empty string value.'
      );
    }
    log.debug("Registering icon pack:", iconLoader.name);
    if ("loader" in iconLoader) {
      loaderStore.set(iconLoader.name, iconLoader.loader);
    } else if ("icons" in iconLoader) {
      iconsStore.set(iconLoader.name, iconLoader.icons);
    } else {
      log.error("Invalid icon loader:", iconLoader);
      throw new Error('Invalid icon loader. Must have either "icons" or "loader" property.');
    }
  }
}, "registerIconPacks");
var getRegisteredIconData = /* @__PURE__ */ __name(async (iconName, fallbackPrefix) => {
  const data = stringToIcon(iconName, true, fallbackPrefix !== void 0);
  if (!data) {
    throw new Error(`Invalid icon name: ${iconName}`);
  }
  const prefix = data.prefix || fallbackPrefix;
  if (!prefix) {
    throw new Error(`Icon name must contain a prefix: ${iconName}`);
  }
  let icons = iconsStore.get(prefix);
  if (!icons) {
    const loader = loaderStore.get(prefix);
    if (!loader) {
      throw new Error(`Icon set not found: ${data.prefix}`);
    }
    try {
      const loaded = await loader();
      icons = { ...loaded, prefix };
      iconsStore.set(prefix, icons);
    } catch (e) {
      log.error(e);
      throw new Error(`Failed to load icon set: ${data.prefix}`);
    }
  }
  const iconData = getIconData(icons, data.name);
  if (!iconData) {
    throw new Error(`Icon not found: ${iconName}`);
  }
  return iconData;
}, "getRegisteredIconData");
var isIconAvailable = /* @__PURE__ */ __name(async (iconName) => {
  try {
    await getRegisteredIconData(iconName);
    return true;
  } catch {
    return false;
  }
}, "isIconAvailable");
var getIconSVG = /* @__PURE__ */ __name(async (iconName, customisations, extraAttributes) => {
  let iconData;
  try {
    iconData = await getRegisteredIconData(iconName, customisations?.fallbackPrefix);
  } catch (e) {
    log.error(e);
    iconData = unknownIcon;
  }
  const renderData = iconToSVG(iconData, customisations);
  const svg = iconToHTML(replaceIDs(renderData.body), {
    ...renderData.attributes,
    ...extraAttributes
  });
  return sanitizeText(svg, getConfig());
}, "getIconSVG");

export {
  unknownIcon,
  registerIconPacks,
  isIconAvailable,
  getIconSVG
};
