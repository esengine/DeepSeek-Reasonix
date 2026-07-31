import { emojiComponents } from "../data.js";
const nameSplit = ": ";
const variationSplit = ", ";
const ignoredVariations = new Set(["person"]);
/**
* Split emoji name to base name and variations
*
* Also finds indexes of each variation
*/
function splitEmojiNameVariations(name, sequence, componentsData) {
	const parts = name.split(nameSplit);
	const base = parts.shift();
	if (!parts.length) return {
		base,
		key: base
	};
	const baseVariations = parts.join(nameSplit).split(variationSplit).filter((text) => {
		if (!componentsData.types[text]) return !ignoredVariations.has(text);
		return false;
	});
	const result = {
		base,
		key: base + (baseVariations.length ? nameSplit + baseVariations.join(variationSplit) : "")
	};
	let components = 0;
	const variations = [...baseVariations];
	for (let index = 0; index < sequence.length; index++) {
		const num = sequence[index];
		for (const key in emojiComponents) {
			const type = key;
			const range = emojiComponents[type];
			if (num >= range[0] && num < range[1]) {
				variations.push({
					index,
					type
				});
				components++;
			}
		}
	}
	if (variations.length) result.variations = variations;
	if (components) result.components = components;
	return result;
}
export { splitEmojiNameVariations };
