/*!
 * SplitText 3.15.0
 * https://gsap.com
 *
 * @license Copyright 2008-2026, GreenSock. All rights reserved.
 * Subject to the terms at https://gsap.com/standard-license
 * @author: Jack Doyle
*/
/* eslint-disable */

// Core Types
export type SplitTextTarget = string | NodeList | Node | Node[];
type BoundingRect = {
	left: number;
	top: number;
	width: number;
	height: number;
};

// Configuration Types
export interface WordDelimiterConfig {
	delimiter: RegExp | string;
	replaceWith?: string;
}

export interface SplitTextConfig {
	type: string;
	mask?: "lines" | "words" | "chars";
	wordDelimiter?: string | RegExp | WordDelimiterConfig;
	linesClass?: string;
	wordsClass?: string;
	charsClass?: string;
	aria?: "auto" | "hidden" | "none";
	tag?: string;
	propIndex?: boolean;
	deepSlice?: boolean;
	smartWrap?: boolean;
	specialChars?: string[] | RegExp;
	reduceWhiteSpace?: boolean;
	autoSplit?: boolean;
	ignore?: SplitTextTarget;
	prepareText?: PrepareTextFunction;
	onSplit?: (splitText: SplitText) => void;
	onRevert?: (splitText: SplitText) => void;
	overwrite?: boolean; // whether to overwrite the element if it has been split before
}

// Function Types
export type PrepareTextFunction = (text: string, element: Element) => string;
type LineWrapperFunction = (startIndex: number, endIndex: number) => void;
type ContextFunction = (obj?: SplitText) => object | void;

// Wrapper Types
type WrapFunction = {
	(text: string): HTMLElement;
	collection: HTMLElement[];
};

// Internal Types
interface SplitTextOriginal {
	element: Element;
	html: string;
	ariaL: string | null; // aria-label
	ariaH: string | null; // aria-hidden
	width?: number;
}

let gsap: any, 
	_fonts: FontFaceSet | undefined, 
	_splitProp: symbol | string = typeof Symbol === "function" ? Symbol() : "_split",
	_coreInitted: boolean, // set to true when the GSAP core is registered
	_initIfNecessary = () => _coreInitted || SplitText.register((window as any).gsap),
	_charSegmenter: any = typeof Intl !== "undefined" && "Segmenter" in Intl ? new (Intl as any).Segmenter() : 0, // not all older browsers support Intl.Segmenter
	_toArray = (r: string | NodeList | Node | Node[]): Node[] => !r ? [] : typeof r === "string" ? _toArray(document.querySelectorAll(r)) : "length" in r ? Array.from(r).reduce((acc: Node[], cur: Node | string) => { typeof cur === "string" ? acc.push(..._toArray(cur)) : acc.push(cur); return acc; }, []) : [r],
	_elements = (targets: SplitTextTarget): HTMLElement[] => _toArray(targets).filter((e) => e && e.nodeType === 1) as HTMLElement[], // don't use instanceof HTMLElement because it's not always reliable (e.g. in an iframe). See https://github.com/greensock/GSAP/issues/640
	_emptyArray: string[] = [],
	_context: ContextFunction = function() {},
	_defaultContext: {add:(f: Function) => any} = {add:f => f()},
	_spacesRegEx: RegExp = /\s+/g,
	_emojiSafeRegEx: RegExp = /\p{RI}\p{RI}|\p{Emoji}(\p{EMod}|\u{FE0F}\u{20E3}?|[\u{E0020}-\u{E007E}]+\u{E007F})?(\u{200D}\p{Emoji}(\p{EMod}|\u{FE0F}\u{20E3}?|[\u{E0020}-\u{E007E}]+\u{E007F})?)*|./gu, // accommodates emojis like 👨‍👨‍👦‍👦 which the more simple /./gu RegExp does not.
	
	// alternate regex for emojis:
	//_emojiSafeRegEx: RegExp = /\p{RI}\p{RI}|\p{Emoji}(\p{Emoji_Modifier}+|\u{FE0F}\u{20E3}?|[\u{E0020}-\u{E007E}]+\u{E007F})?(\u{200D}\p{Emoji}(\p{Emoji_Modifier}+|\u{FE0F}\u{20E3}?|[\u{E0020}-\u{E007E}]+\u{E007F})?)+|\p{EPres}(\p{Emoji_Modifier}+|\u{FE0F}\u{20E3}?|[\u{E0020}-\u{E007E}]+\u{E007F})?|\p{Emoji}(\p{Emoji_Modifier}+|\u{FE0F}\u{20E3}?|[\u{E0020}-\u{E007E}]+\u{E007F})*|./gu,
		
	_emptyBounds: BoundingRect = {left: 0, top: 0, width: 0, height: 0},
	_findNextValidBounds = (allBounds: BoundingRect[], startIndex: number): BoundingRect => { // searches through the allBounds Array to find the next non-empty bounds after the startIndex (non-inclusive). Returns the empty bounds if no valid bounds are found.
		while (++startIndex < allBounds.length && allBounds[startIndex] === _emptyBounds) {}
		return allBounds[startIndex] || _emptyBounds;
	},
	_revertOriginal = ({element, html, ariaL, ariaH}: SplitTextOriginal): void => {
		element.innerHTML = html;
		ariaL ? element.setAttribute("aria-label", ariaL) : element.removeAttribute("aria-label");
		ariaH ? element.setAttribute("aria-hidden", ariaH) : element.removeAttribute("aria-hidden");
	},

	// custom word split function that allows for a callback to specify the delimiter replacement
	// _customSplit = (str: string, pattern: RegExp, callback?: (subStr: string, match: string, matchObj: RegExpExecArray) => string) => {
	// 	let result: string[] = [],
	// 		pos: number = 0,
	// 		match: RegExpExecArray | null, subStr: string;
	// 	while ((match = pattern.exec(str)) !== null) {
	// 		subStr = str.slice(pos, match.index);
	// 		result.push(subStr + (callback && callback(subStr, match[0], match) || ""));
	// 		pos = match.index + match[0].length;
	// 	}
	// 	pos < str.length && result.push(str.slice(pos));
	// 	return result;
	// },
	
	// merges consecutive cells that form a special character into a single cell. Like ["a", "b", "c"] and /ab/g would become ["ab", "c"]. Does not create a new Array - it modifies the existing one in place.
	_stretchToFitSpecialChars = (collection: string[], specialCharsRegEx: RegExp | undefined): string[] => {
		if (specialCharsRegEx) {
			let charsFound: Set<string> = new Set((collection.join("").match(specialCharsRegEx) || _emptyArray)),
				i: number = collection.length, slots: number, word: string, char: string, combined: string;
			if (charsFound.size) {
				while (--i > -1) {
					word = collection[i];
					for (char of charsFound) {
						if (char.startsWith(word) && char.length > word.length) {
							slots = 0;
							combined = word;
							while (char.startsWith((combined += collection[i+(++slots)])) && combined.length < char.length) {}
							if (slots && combined.length === char.length) {
								collection[i] = char;
								collection.splice(i+1, slots);
								break;
							}
						}
					}
				}
			}
		}
		return collection;
	},
	_disallowInline = (element: HTMLElement): unknown => window.getComputedStyle(element).display === "inline" && (element.style.display = "inline-block"),
	_insertNodeBefore = (newChild: Node | string, parent: Element, existingChild: Node): Node => parent.insertBefore(typeof newChild === "string" ? document.createTextNode(newChild) : newChild as Node, existingChild),
	
	// create a wrapper function that will create a new element with the given type (char, word, or line) and add it to the collection
	_getWrapper = (type: string, config: SplitTextConfig, collection: HTMLElement[]): WrapFunction => {
		let className: string = (config as any)[type + "sClass"] || "",
			{tag = "div", aria = "auto", propIndex = false} = config,
			display: string = type === "line" ? "block" : "inline-block",
			incrementClass: boolean = className.indexOf("++") > -1,
			wrapper = ((text: string): HTMLElement => {
				let el: HTMLElement = document.createElement(tag),
					i: number = collection.length + 1;
				className && (el.className = className + (incrementClass ? " " + className + i : ""));
				propIndex && el.style.setProperty("--" + type, i + "");
				aria !== "none" && el.setAttribute("aria-hidden", "true");
				if (tag !== "span") {
					el.style.position = "relative";
					el.style.display = display;
				}
				el.textContent = text;
				collection.push(el);
				return el;
			}) as WrapFunction;
		incrementClass && (className = className.replace("++", ""));
		wrapper.collection = collection;
		return wrapper;
	},

	// there's some special logic for lines that we need to handle on top of the normal wrapper function
	_getLineWrapper = (element: Element, nodes: Node[], config: SplitTextConfig, collection: HTMLElement[]): LineWrapperFunction => {
		let lineWrapper: WrapFunction = _getWrapper("line", config, collection),
			textAlign: string = window.getComputedStyle(element).textAlign || "left";
		return (startIndex: number, endIndex: number): void => {
			let newLine: HTMLElement = lineWrapper("");
			newLine.style.textAlign = textAlign;
			element.insertBefore(newLine, nodes[startIndex]);
			for (; startIndex < endIndex; startIndex++) {
				newLine.appendChild(nodes[startIndex]);
			}
			newLine.normalize();
		}
	},

	// this is the main recursive function that splits the text into words and characters. We handle line splitting separately. 
	_splitWordsAndCharsRecursively = (element: Element, config: SplitTextConfig, wordWrapper: WrapFunction, charWrapper: WrapFunction | null, prepForCharsOnly: boolean, deepSlice: boolean, ignore: Element[] | false, charSplitRegEx: RegExp, specialCharsRegEx: RegExp | undefined, isNested: boolean): void => {
		let nodes: Node[] = Array.from(element.childNodes),
			i: number = 0,
			{wordDelimiter, reduceWhiteSpace = true, prepareText } = config,
			elementBounds: BoundingRect = element.getBoundingClientRect(),
			lastBounds: BoundingRect = elementBounds,
			isPreformatted: boolean = !reduceWhiteSpace && window.getComputedStyle(element).whiteSpace.substring(0, 3) === "pre",
			ignoredPreviousSibling: Element | number = 0,
			wordsCollection: HTMLElement[] = wordWrapper.collection,
			wordDelimIsNotSpace: boolean,
			wordDelimString: string, 
			wordDelimSplitter: RegExp | string | undefined,
			curNode: Node, words: string[], curWordEl: Element, startsWithSpace: boolean, endsWithSpace: boolean, j: number, bounds: BoundingRect, curWordChars: string[], 
			clonedNode: HTMLElement, curSubNode: Node | null, tempSubNode: Node, curTextContent: string, wordText: string, lastWordText: string, k: number;
		
		if (typeof wordDelimiter === "object") {
			wordDelimSplitter = (wordDelimiter as WordDelimiterConfig).delimiter || wordDelimiter as (string | RegExp);
			wordDelimString = (wordDelimiter as WordDelimiterConfig).replaceWith || "";
		} else {
			wordDelimString = wordDelimiter === "" ? "" : wordDelimiter || " ";
		}
		wordDelimIsNotSpace = wordDelimString !== " ";
		
		for (; i < nodes.length; i++) {
			curNode = nodes[i];
			if (curNode.nodeType === 3) {

				curTextContent = curNode.textContent || "";
				if (reduceWhiteSpace) {
					curTextContent = curTextContent.replace(_spacesRegEx, " ")
				} else if (isPreformatted) {
					curTextContent = curTextContent.replace(/\n/g, wordDelimString + "\n");
				}
				prepareText && (curTextContent = prepareText(curTextContent, element));
				curNode.textContent = curTextContent;

				words = wordDelimString || wordDelimSplitter ? curTextContent.split(wordDelimSplitter || wordDelimString) : curTextContent.match(charSplitRegEx) || _emptyArray;
				lastWordText = words[words.length - 1];
				endsWithSpace = wordDelimIsNotSpace ? lastWordText.slice(-1) === " " : !lastWordText;
				lastWordText || words.pop(); // if the last word is empty, remove it
				lastBounds = elementBounds;
				startsWithSpace = wordDelimIsNotSpace ? words[0].charAt(0) === " " : !words[0];
				startsWithSpace && _insertNodeBefore(" ", element, curNode); // if the word starts with a space, add a space to the beginning of the node
				words[0] || words.shift(); // if the first word is empty, remove it

				_stretchToFitSpecialChars(words, specialCharsRegEx); 
				(deepSlice && isNested) || (curNode.textContent = ""); // only clear out the text if we don't need to measure bounds. We must measure bounds if we're either splitting lines -OR- if we're splitting ONLY characters. In that case, it's important to gradually swap out the text content as we slice it up, otherwise we'll get funky wrapping that'd throw off the bounds calculations.
				for (j = 1; j <= words.length; j++) {

					wordText = words[j-1];
					if (!reduceWhiteSpace && isPreformatted && wordText.charAt(0) === "\n") { // when we're NOT reducing white space, and we're in a preformatted element, and the word starts with a newline, then we need to remove the previous sibling (which is a wordDelimiter) and insert a <br> tag.
						curNode.previousSibling?.remove(); // we added an extra wordDelimiter in front of all newline characters, so we need to remove it.
						_insertNodeBefore(document.createElement("br"), element, curNode);
						wordText = wordText.slice(1);
					}

					if (!reduceWhiteSpace && wordText === "") {
						_insertNodeBefore(wordDelimString, element, curNode);
					} else if (wordText === " ") {
						element.insertBefore(document.createTextNode(" "), curNode);
					} else {

						wordDelimIsNotSpace && wordText.charAt(0) === " " && _insertNodeBefore(" ", element, curNode);

						// if the previous sibling is an ignored element, and we're on the first word, and there's no starting space, and the previous sibling is part of the word collection, then we must combine them (as if this is continuing a word from the previous node before the ignored element(s)).
						if (ignoredPreviousSibling && j === 1 && !startsWithSpace && wordsCollection.indexOf((ignoredPreviousSibling as Element).parentNode as HTMLElement) > -1) {
							curWordEl = wordsCollection[wordsCollection.length - 1];
							curWordEl.appendChild(document.createTextNode(charWrapper ? "" : wordText)); // if we're splitting characters, we'll add them one-by-one below
						} else {
							curWordEl = wordWrapper(charWrapper ? "" : wordText); // if we're splitting characters, we'll add them one-by-one below
							_insertNodeBefore(curWordEl, element, curNode);
							ignoredPreviousSibling && j === 1 && !startsWithSpace && curWordEl.insertBefore(ignoredPreviousSibling as Element, curWordEl.firstChild);
						}

						// split characters if necessary
						if (charWrapper) {
							curWordChars = _charSegmenter ? _stretchToFitSpecialChars([..._charSegmenter.segment(wordText)].map(s => s.segment), specialCharsRegEx) : wordText.match(charSplitRegEx) || _emptyArray;
							for (k = 0; k < curWordChars.length; k++) {
								curWordEl.appendChild(curWordChars[k] === " " ? document.createTextNode(" ") : charWrapper(curWordChars[k]));
							}
						}
						
						// subdivide if necessary so that if a single inline element spills onto multiple lines, it gets sliced up accordingly
						if (deepSlice && isNested) { 
							curTextContent = curNode.textContent = curTextContent.substring(wordText.length+1, curTextContent.length); // remember that we've got to accommodate the word delimiter in the substring.
							bounds = curWordEl.getBoundingClientRect();
							if (bounds.top > lastBounds.top && bounds.left <= lastBounds.left) {
								clonedNode = element.cloneNode() as HTMLElement;
								curSubNode = element.childNodes[0];
								while (curSubNode && curSubNode !== curWordEl) {
									tempSubNode = curSubNode;
									curSubNode = curSubNode.nextSibling; // once we renest it in clonedNode, the nextSibling will be different, so grab it here. 
									clonedNode.appendChild(tempSubNode);
								}
								(element.parentNode as Element).insertBefore(clonedNode, element);
								prepForCharsOnly && _disallowInline(clonedNode);
							}
							lastBounds = bounds;
						}
						if (j < words.length || endsWithSpace) {
							// always add the delimiter between each word unless we're at the very last word in which case we may need to add a space. Special case: if a word in the middle ends in a space and we're NOT using space as the delimiter, then we need to insert a space before the delimiter too. Example: if wordDelimiter is "t" in "This text is <strong>bold</strong> and there is a <a href="https://gsap.com">link here</a>."
							_insertNodeBefore(j >= words.length ? " " : wordDelimIsNotSpace && wordText.slice(-1) === " " ? " " + wordDelimString : wordDelimString, element, curNode);
						}
					}

				}
				element.removeChild(curNode);
				ignoredPreviousSibling = 0;
			} else if (curNode.nodeType === 1) {
				if (ignore && ignore.indexOf(curNode as Element) > -1) { // if the current node is in the ignore array, then we need to ignore it and move it inside the end of the last word (but only if the previous sibling is a word).
					wordsCollection.indexOf(curNode.previousSibling as HTMLElement) > -1 && wordsCollection[wordsCollection.length - 1].appendChild(curNode);
					ignoredPreviousSibling = curNode as Element;
				} else {
					_splitWordsAndCharsRecursively(curNode as Element, config, wordWrapper, charWrapper, prepForCharsOnly, deepSlice, ignore, charSplitRegEx, specialCharsRegEx, true);
					ignoredPreviousSibling = 0;
				}
				prepForCharsOnly && _disallowInline(curNode as HTMLElement);
			}
		}
	};

export class SplitText {
	elements: HTMLElement[];
	chars: HTMLElement[];
	words: HTMLElement[];
	lines: HTMLElement[];
	masks: HTMLElement[];
	_data: {
		orig: SplitTextOriginal[];
		anim?: {totalTime: (t?: number) => number, revert: () => void};
		animTime?: number;
		obs: ResizeObserver | false;
	};
	vars: SplitTextConfig;
	isSplit: boolean = false;
	_ctx?: {add: (f: Function) => void};
	_split: () => void;

	constructor(elements: SplitTextTarget, config: SplitTextConfig) {
		_initIfNecessary();
		this.elements = _elements(elements);
		this.chars = [];
		this.words = [];
		this.lines = [];
		this.masks = [];
		this.vars = config;
		this.elements.forEach((el: HTMLElement) => { // avoid re-splitting the same element multiple times
			config.overwrite !== false && (el as any)[_splitProp]?._data.orig.filter(({element}: SplitTextOriginal) => element === el).forEach(_revertOriginal);
			(el as any)[_splitProp] = this; // set the split prop to the current instance
		});
		this._split = () => this.isSplit && this.split(this.vars);
		let orig: SplitTextOriginal[] = [],
			timerId: number,
			checkWidths = () => {
				let i: number = orig.length,
					o: SplitTextOriginal;
				while (i--) {
					o = orig[i];
					let w: number = (o.element as HTMLElement).offsetWidth;
					if (w !== o.width) {
						o.width = w;
						this._split();
						return;
					}
				}
			};
		this._data = {orig: orig, obs: typeof(ResizeObserver) !== "undefined" && new ResizeObserver(() => { clearTimeout(timerId); timerId = setTimeout(checkWidths, 200) as unknown as number})};
		
		_context(this);
		this.split(config);
	}

	split(config: SplitTextConfig) {
		(this._ctx || _defaultContext).add(() => {
			this.isSplit && this.revert();
			this.vars = config = config || this.vars || {};
			let {type = "chars,words,lines", aria = "auto", deepSlice = true, smartWrap, onSplit, autoSplit = false, specialChars, mask} = this.vars,
				splitLines: boolean = type.indexOf("lines") > -1,
				splitCharacters: boolean = type.indexOf("chars") > -1,
				splitWords: boolean = type.indexOf("words") > -1,
				onlySplitCharacters: boolean = splitCharacters && !splitWords && !splitLines,
				specialCharsRegEx: RegExp | undefined = specialChars && (("push" in specialChars) ? new RegExp("(?:" + specialChars.join("|") + ")", "gu") : specialChars),
				finalCharSplitRegEx: RegExp = specialCharsRegEx ? new RegExp(specialCharsRegEx.source + "|" + _emojiSafeRegEx.source, "gu") : _emojiSafeRegEx,
				ignore: HTMLElement[] | false = !!config.ignore && _elements(config.ignore),
				{orig, animTime, obs} = this._data,
				onSplitResult: any;
			if (splitCharacters || splitWords || splitLines) {
				this.elements.forEach((element, index) => {
					orig[index] = { 
						element, 
						html: element.innerHTML, 
						ariaL: element.getAttribute("aria-label"),
						ariaH: element.getAttribute("aria-hidden")
					};
					aria === "auto" ? element.setAttribute("aria-label", (element.textContent || "").trim()) : aria === "hidden" && element.setAttribute("aria-hidden", "true");
					let chars: HTMLElement[] = [],
						words: HTMLElement[] = [],
						lines: HTMLElement[] = [], 
						charWrapper = splitCharacters ? _getWrapper("char", config, chars) : null,
						wordWrapper = _getWrapper("word", config, words),
						i: number, curWord: Element, smartWrapSpan: HTMLElement, nextSibling: Node;

					// split words (always) and characters (if necessary)
					_splitWordsAndCharsRecursively(element, config, wordWrapper, charWrapper, onlySplitCharacters, deepSlice && (splitLines || onlySplitCharacters), ignore, finalCharSplitRegEx, specialCharsRegEx, false);

					// subdivide lines
					if (splitLines) {
						let nodes: Node[] = _toArray(element.childNodes),
							wrapLine = _getLineWrapper(element, nodes, config, lines),
							curNode: Node, 
							toRemove: Node[] = [],
							lineStartIndex: number = 0,
							allBounds: BoundingRect[] = nodes.map((n) => n.nodeType === 1 ? (n as Element).getBoundingClientRect() : _emptyBounds), // do all these measurements at once so that we don't trigger layout thrashing
							lastBounds: BoundingRect = _emptyBounds,
							curBounds: BoundingRect;
						for (i = 0; i < nodes.length; i++) {
							curNode = nodes[i];
							if (curNode.nodeType === 1) {
								if (curNode.nodeName === "BR") { // remove any <br> tags because breaking up by lines already handles that.
									if (!i || nodes[i-1].nodeName !== "BR") {
										toRemove.push(curNode);
										wrapLine(lineStartIndex, i+1);
									}
									lineStartIndex = i+1;
									lastBounds = _findNextValidBounds(allBounds, i);
								} else {
									curBounds = allBounds[i];
									if (i && curBounds.top > lastBounds.top && curBounds.left < lastBounds.left + lastBounds.width - 1) {
										wrapLine(lineStartIndex, i);
										lineStartIndex = i;
									}
									lastBounds = curBounds;
								}
							}
						}
						lineStartIndex < i && wrapLine(lineStartIndex, i);
						toRemove.forEach(el => el.parentNode?.removeChild(el));
					}
					
					// remove words if "type" doesn't include "words"
					if (!splitWords) {
						for (i = 0; i < words.length; i++) {
							curWord = words[i];
							if (splitCharacters || !curWord.nextSibling || curWord.nextSibling.nodeType !== 3) {
								if (smartWrap && !splitLines) { // tried inserting String.fromCharCode(8288) between each character to prevent words from wrapping strangely, but it doesn't work. Also tried adding <wbr> tags between each character, but it doesn't work either.
									smartWrapSpan = document.createElement("span"); // replace the word element with a span that has white-space: nowrap
									smartWrapSpan.style.whiteSpace = "nowrap";
									while (curWord.firstChild) {
										smartWrapSpan.appendChild(curWord.firstChild);
									}
									curWord.replaceWith(smartWrapSpan);
								} else {
									curWord.replaceWith(...curWord.childNodes);
								}
							} else {
								nextSibling = curWord.nextSibling;
								if (nextSibling && nextSibling.nodeType === 3) {
									nextSibling.textContent = (curWord.textContent || "") + (nextSibling.textContent || "");
									curWord.remove();
								}
							}
						}
						words.length = 0;
						element.normalize();
					}
					
					this.lines.push(...lines);
					this.words.push(...words);
					this.chars.push(...chars);
				});

				mask && this[mask] && this.masks.push(...(this as any)[mask as string].map((el: HTMLElement) => {
					let maskEl: HTMLElement = el.cloneNode() as HTMLElement;
					el.replaceWith(maskEl);
					maskEl.appendChild(el);
					el.className && (maskEl.className = el.className.trim().split(" ").map((s: string) => s + "-mask").join(" "));
					maskEl.style.overflow = "clip";
					return maskEl;
				}));

			}

			this.isSplit = true;

			_fonts && splitLines && autoSplit && _fonts.addEventListener("loadingdone", this._split);

			if ((onSplitResult = onSplit && onSplit(this)) && onSplitResult.totalTime) {
				this._data.anim = animTime ? onSplitResult.totalTime(animTime) : onSplitResult;
			}

			splitLines && autoSplit && this.elements.forEach((element, index) => {
				orig[index].width = element.offsetWidth;
				obs && obs.observe(element);
			});
		});
		return this;
	}

	kill() {
		let {obs} = this._data;
		obs && obs.disconnect();
		_fonts?.removeEventListener("loadingdone", this._split);
	}

	revert() {
		if (this.isSplit) {
			let {orig, anim} = this._data;
			this.kill();
			orig.forEach(_revertOriginal);
			this.chars.length = this.words.length = this.lines.length = orig.length = this.masks.length = 0;
			this.isSplit = false;
			if (anim) {  // if the user returned an animation in the onSplit, we record the totalTime() here and revert() it and then we'll the totalTime() of the newly returned onSplit animation. This allows things to be seamless
				this._data.animTime = anim.totalTime();
				anim.revert(); 
			}
			this.vars.onRevert?.(this);
		}
		return this;
	}

	static create(elements: SplitTextTarget, config: SplitTextConfig) {
		return new SplitText(elements, config);
	}

	static register(core: any) {
		gsap = gsap || core || (window as any).gsap;
		if (gsap) {
			_toArray = gsap.utils.toArray;
			_context = gsap.core.context || _context;
		}
		if (!_coreInitted && window.innerWidth > 0) {
			_fonts = document.fonts;
			_coreInitted = true;
		}
	}

	static readonly version: string = "3.15.0";
}

export { SplitText as default };