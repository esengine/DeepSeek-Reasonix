/*!
 * TextPlugin 3.15.0
 * https://gsap.com
 *
 * @license Copyright 2008-2026, GreenSock. All rights reserved.
 * Subject to the terms at https://gsap.com/standard-license
 * @author: Jack Doyle, jack@greensock.com
*/
/* eslint-disable */

import { emojiSafeSplit, getText, splitInnerHTML } from "./utils/strings.js";

let gsap, _tempDiv,
	_getGSAP = () => gsap || (typeof(window) !== "undefined" && (gsap = window.gsap) && gsap.registerPlugin && gsap);


export const TextPlugin = {
	version:"3.15.0",
	name:"text",
	init(target, value, tween) {
		typeof(value) !== "object" && (value = {value:value});
		let i = target.nodeName.toUpperCase(),
			data = this,
			{ newClass, oldClass, preserveSpaces, rtl } = value,
			delimiter = data.delimiter = value.delimiter || "",
			fillChar = data.fillChar = value.fillChar || (value.padSpace ? "&nbsp;" : ""),
			short, text, original, j, condensedText, condensedOriginal, aggregate, s;
		data.svg = (target.getBBox && (i === "TEXT" || i === "TSPAN"));
		if (!("innerHTML" in target) && !data.svg) {
			return false;
		}
		data.target = target;
		if (!("value" in value)) {
			data.text = data.original = [""];
			return;
		}
		original = splitInnerHTML(target, delimiter, false, preserveSpaces, data.svg);
		_tempDiv || (_tempDiv = document.createElement("div"));
		_tempDiv.innerHTML = value.value;
		text = splitInnerHTML(_tempDiv, delimiter, false, preserveSpaces, data.svg);
		data.from = tween._from;
		if ((data.from || rtl) && !(rtl && data.from)) { // right-to-left or "from()" tweens should invert things (but if it's BOTH .from() and rtl, inverting twice equals not inverting at all :)
			i = original;
			original = text;
			text = i;
		}
		data.hasClass = !!(newClass || oldClass);
		data.newClass = rtl ? oldClass : newClass;
		data.oldClass = rtl ? newClass : oldClass;
		i = original.length - text.length;
		short = i < 0 ? original : text;
		if (i < 0) {
			i = -i;
		}
		while (--i > -1) {
			short.push(fillChar);
		}
		if (value.type === "diff") {
			j = 0;
			condensedText = [];
			condensedOriginal = [];
			aggregate = "";
			for (i = 0; i < text.length; i++) {
				s = text[i];
				if (s === original[i]) {
					aggregate += s;
				} else {
					condensedText[j] = aggregate + s;
					condensedOriginal[j++] = aggregate + original[i];
					aggregate = "";
				}
			}
			text = condensedText;
			original = condensedOriginal;
			if (aggregate) {
				text.push(aggregate);
				original.push(aggregate);
			}
		}
		value.speed && tween.duration(Math.min(0.05 / value.speed * short.length, value.maxDuration || 9999));
		data.rtl = rtl;
		data.original = original;
		data.text = text;
		data._props.push("text");
	},
	render(ratio, data) {
		if (ratio > 1) {
			ratio = 1;
		} else if (ratio < 0) {
			ratio = 0;
		}
		if (data.from) {
			ratio = 1 - ratio;
		}
		let { text, hasClass, newClass, oldClass, delimiter, target, fillChar, original, rtl } = data,
			l = text.length,
			i = ((rtl ? 1 - ratio : ratio) * l + 0.5) | 0,
			applyNew, applyOld, str;
		if (hasClass && ratio) {
			applyNew = (newClass && i);
			applyOld = (oldClass && i !== l);
			str = (applyNew ? "<span class='" + newClass + "'>" : "") + text.slice(0, i).join(delimiter) + (applyNew ? "</span>" : "") + (applyOld ? "<span class='" + oldClass + "'>" : "") + delimiter + original.slice(i).join(delimiter) + (applyOld ? "</span>" : "");
		} else {
			str = text.slice(0, i).join(delimiter) + delimiter + original.slice(i).join(delimiter);
		}
		if (data.svg) { //SVG text elements don't have an "innerHTML" in Microsoft browsers.
			target.textContent = str;
		} else {
			target.innerHTML = (fillChar === "&nbsp;" && ~str.indexOf("  ")) ? str.split("  ").join("&nbsp;&nbsp;") : str;
		}
	}
};

TextPlugin.splitInnerHTML = splitInnerHTML;
TextPlugin.emojiSafeSplit = emojiSafeSplit;
TextPlugin.getText = getText;

_getGSAP() && gsap.registerPlugin(TextPlugin);

export { TextPlugin as default };