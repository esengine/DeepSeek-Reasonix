import { H as __require, z as __commonJSMin } from "./logger.js";
import { t as require_lib } from "./lib.js";
//#region ../../node_modules/.pnpm/postcss-modules@6.0.1_postcss@8.5.15/node_modules/postcss-modules/build/fs.js
var require_fs = /* @__PURE__ */ __commonJSMin(((exports) => {
	Object.defineProperty(exports, "__esModule", { value: true });
	exports.getFileSystem = getFileSystem;
	exports.setFileSystem = setFileSystem;
	let fileSystem = {
		readFile: () => {
			throw Error("readFile not implemented");
		},
		writeFile: () => {
			throw Error("writeFile not implemented");
		}
	};
	function setFileSystem(fs) {
		fileSystem.readFile = fs.readFile;
		fileSystem.writeFile = fs.writeFile;
	}
	function getFileSystem() {
		return fileSystem;
	}
}));
//#endregion
//#region ../../node_modules/.pnpm/postcss-modules@6.0.1_postcss@8.5.15/node_modules/postcss-modules/build/unquote.js
var require_unquote = /* @__PURE__ */ __commonJSMin(((exports) => {
	Object.defineProperty(exports, "__esModule", { value: true });
	exports.default = unquote;
	const reg = /['"]/;
	function unquote(str) {
		if (!str) return "";
		if (reg.test(str.charAt(0))) str = str.substr(1);
		if (reg.test(str.charAt(str.length - 1))) str = str.substr(0, str.length - 1);
		return str;
	}
}));
//#endregion
//#region ../../node_modules/.pnpm/icss-utils@5.1.0_postcss@8.5.15/node_modules/icss-utils/src/replaceValueSymbols.js
var require_replaceValueSymbols = /* @__PURE__ */ __commonJSMin(((exports, module) => {
	const matchValueName = /[$]?[\w-]+/g;
	const replaceValueSymbols = (value, replacements) => {
		let matches;
		while (matches = matchValueName.exec(value)) {
			const replacement = replacements[matches[0]];
			if (replacement) {
				value = value.slice(0, matches.index) + replacement + value.slice(matchValueName.lastIndex);
				matchValueName.lastIndex -= matches[0].length - replacement.length;
			}
		}
		return value;
	};
	module.exports = replaceValueSymbols;
}));
//#endregion
//#region ../../node_modules/.pnpm/icss-utils@5.1.0_postcss@8.5.15/node_modules/icss-utils/src/replaceSymbols.js
var require_replaceSymbols = /* @__PURE__ */ __commonJSMin(((exports, module) => {
	const replaceValueSymbols = require_replaceValueSymbols();
	const replaceSymbols = (css, replacements) => {
		css.walk((node) => {
			if (node.type === "decl" && node.value) node.value = replaceValueSymbols(node.value.toString(), replacements);
			else if (node.type === "rule" && node.selector) node.selector = replaceValueSymbols(node.selector.toString(), replacements);
			else if (node.type === "atrule" && node.params) node.params = replaceValueSymbols(node.params.toString(), replacements);
		});
	};
	module.exports = replaceSymbols;
}));
//#endregion
//#region ../../node_modules/.pnpm/icss-utils@5.1.0_postcss@8.5.15/node_modules/icss-utils/src/extractICSS.js
var require_extractICSS = /* @__PURE__ */ __commonJSMin(((exports, module) => {
	const importPattern = /^:import\(("[^"]*"|'[^']*'|[^"']+)\)$/;
	const balancedQuotes = /^("[^"]*"|'[^']*'|[^"']+)$/;
	const getDeclsObject = (rule) => {
		const object = {};
		rule.walkDecls((decl) => {
			const before = decl.raws.before ? decl.raws.before.trim() : "";
			object[before + decl.prop] = decl.value;
		});
		return object;
	};
	/**
	*
	* @param {string} css
	* @param {boolean} removeRules
	* @param {'auto' | 'rule' | 'at-rule'} mode
	*/
	const extractICSS = (css, removeRules = true, mode = "auto") => {
		const icssImports = {};
		const icssExports = {};
		function addImports(node, path) {
			const unquoted = path.replace(/'|"/g, "");
			icssImports[unquoted] = Object.assign(icssImports[unquoted] || {}, getDeclsObject(node));
			if (removeRules) node.remove();
		}
		function addExports(node) {
			Object.assign(icssExports, getDeclsObject(node));
			if (removeRules) node.remove();
		}
		css.each((node) => {
			if (node.type === "rule" && mode !== "at-rule") {
				if (node.selector.slice(0, 7) === ":import") {
					const matches = importPattern.exec(node.selector);
					if (matches) addImports(node, matches[1]);
				}
				if (node.selector === ":export") addExports(node);
			}
			if (node.type === "atrule" && mode !== "rule") {
				if (node.name === "icss-import") {
					const matches = balancedQuotes.exec(node.params);
					if (matches) addImports(node, matches[1]);
				}
				if (node.name === "icss-export") addExports(node);
			}
		});
		return {
			icssImports,
			icssExports
		};
	};
	module.exports = extractICSS;
}));
//#endregion
//#region ../../node_modules/.pnpm/icss-utils@5.1.0_postcss@8.5.15/node_modules/icss-utils/src/createICSSRules.js
var require_createICSSRules = /* @__PURE__ */ __commonJSMin(((exports, module) => {
	const createImports = (imports, postcss, mode = "rule") => {
		return Object.keys(imports).map((path) => {
			const aliases = imports[path];
			const declarations = Object.keys(aliases).map((key) => postcss.decl({
				prop: key,
				value: aliases[key],
				raws: { before: "\n  " }
			}));
			const hasDeclarations = declarations.length > 0;
			const rule = mode === "rule" ? postcss.rule({
				selector: `:import('${path}')`,
				raws: { after: hasDeclarations ? "\n" : "" }
			}) : postcss.atRule({
				name: "icss-import",
				params: `'${path}'`,
				raws: { after: hasDeclarations ? "\n" : "" }
			});
			if (hasDeclarations) rule.append(declarations);
			return rule;
		});
	};
	const createExports = (exports$7, postcss, mode = "rule") => {
		const declarations = Object.keys(exports$7).map((key) => postcss.decl({
			prop: key,
			value: exports$7[key],
			raws: { before: "\n  " }
		}));
		if (declarations.length === 0) return [];
		const rule = mode === "rule" ? postcss.rule({
			selector: `:export`,
			raws: { after: "\n" }
		}) : postcss.atRule({
			name: "icss-export",
			raws: { after: "\n" }
		});
		rule.append(declarations);
		return [rule];
	};
	const createICSSRules = (imports, exports$8, postcss, mode) => [...createImports(imports, postcss, mode), ...createExports(exports$8, postcss, mode)];
	module.exports = createICSSRules;
}));
//#endregion
//#region ../../node_modules/.pnpm/icss-utils@5.1.0_postcss@8.5.15/node_modules/icss-utils/src/index.js
var require_src$4 = /* @__PURE__ */ __commonJSMin(((exports, module) => {
	module.exports = {
		replaceValueSymbols: require_replaceValueSymbols(),
		replaceSymbols: require_replaceSymbols(),
		extractICSS: require_extractICSS(),
		createICSSRules: require_createICSSRules()
	};
}));
//#endregion
//#region ../../node_modules/.pnpm/postcss-modules@6.0.1_postcss@8.5.15/node_modules/postcss-modules/build/Parser.js
var require_Parser = /* @__PURE__ */ __commonJSMin(((exports) => {
	Object.defineProperty(exports, "__esModule", { value: true });
	exports.default = void 0;
	var _icssUtils = require_src$4();
	const importRegexp = /^:import\((.+)\)$/;
	var Parser = class {
		constructor(pathFetcher, trace) {
			this.pathFetcher = pathFetcher;
			this.plugin = this.plugin.bind(this);
			this.exportTokens = {};
			this.translations = {};
			this.trace = trace;
		}
		plugin() {
			const parser = this;
			return {
				postcssPlugin: "css-modules-parser",
				async OnceExit(css) {
					await Promise.all(parser.fetchAllImports(css));
					parser.linkImportedSymbols(css);
					return parser.extractExports(css);
				}
			};
		}
		fetchAllImports(css) {
			let imports = [];
			css.each((node) => {
				if (node.type == "rule" && node.selector.match(importRegexp)) imports.push(this.fetchImport(node, css.source.input.from, imports.length));
			});
			return imports;
		}
		linkImportedSymbols(css) {
			(0, _icssUtils.replaceSymbols)(css, this.translations);
		}
		extractExports(css) {
			css.each((node) => {
				if (node.type == "rule" && node.selector == ":export") this.handleExport(node);
			});
		}
		handleExport(exportNode) {
			exportNode.each((decl) => {
				if (decl.type == "decl") {
					Object.keys(this.translations).forEach((translation) => {
						decl.value = decl.value.replace(translation, this.translations[translation]);
					});
					this.exportTokens[decl.prop] = decl.value;
				}
			});
			exportNode.remove();
		}
		async fetchImport(importNode, relativeTo, depNr) {
			const file = importNode.selector.match(importRegexp)[1];
			const depTrace = this.trace + String.fromCharCode(depNr);
			const exports$6 = await this.pathFetcher(file, relativeTo, depTrace);
			try {
				importNode.each((decl) => {
					if (decl.type == "decl") this.translations[decl.prop] = exports$6[decl.value];
				});
				importNode.remove();
			} catch (err) {
				console.log(err);
			}
		}
	};
	exports.default = Parser;
}));
//#endregion
//#region ../../node_modules/.pnpm/postcss-modules@6.0.1_postcss@8.5.15/node_modules/postcss-modules/build/saveJSON.js
var require_saveJSON = /* @__PURE__ */ __commonJSMin(((exports) => {
	Object.defineProperty(exports, "__esModule", { value: true });
	exports.default = saveJSON;
	var _fs = require_fs();
	function saveJSON(cssFile, json) {
		return new Promise((resolve, reject) => {
			const { writeFile } = (0, _fs.getFileSystem)();
			writeFile(`${cssFile}.json`, JSON.stringify(json), (e) => e ? reject(e) : resolve(json));
		});
	}
}));
//#endregion
//#region ../../node_modules/.pnpm/lodash.camelcase@4.3.0/node_modules/lodash.camelcase/index.js
var require_lodash_camelcase = /* @__PURE__ */ __commonJSMin(((exports, module) => {
	/**
	* lodash (Custom Build) <https://lodash.com/>
	* Build: `lodash modularize exports="npm" -o ./`
	* Copyright jQuery Foundation and other contributors <https://jquery.org/>
	* Released under MIT license <https://lodash.com/license>
	* Based on Underscore.js 1.8.3 <http://underscorejs.org/LICENSE>
	* Copyright Jeremy Ashkenas, DocumentCloud and Investigative Reporters & Editors
	*/
	/** Used as references for various `Number` constants. */
	var INFINITY = Infinity;
	/** `Object#toString` result references. */
	var symbolTag = "[object Symbol]";
	/** Used to match words composed of alphanumeric characters. */
	var reAsciiWord = /[^\x00-\x2f\x3a-\x40\x5b-\x60\x7b-\x7f]+/g;
	/** Used to match Latin Unicode letters (excluding mathematical operators). */
	var reLatin = /[\xc0-\xd6\xd8-\xf6\xf8-\xff\u0100-\u017f]/g;
	/** Used to compose unicode character classes. */
	var rsAstralRange = "\\ud800-\\udfff", rsComboMarksRange = "\\u0300-\\u036f\\ufe20-\\ufe23", rsComboSymbolsRange = "\\u20d0-\\u20f0", rsDingbatRange = "\\u2700-\\u27bf", rsLowerRange = "a-z\\xdf-\\xf6\\xf8-\\xff", rsMathOpRange = "\\xac\\xb1\\xd7\\xf7", rsNonCharRange = "\\x00-\\x2f\\x3a-\\x40\\x5b-\\x60\\x7b-\\xbf", rsPunctuationRange = "\\u2000-\\u206f", rsSpaceRange = " \\t\\x0b\\f\\xa0\\ufeff\\n\\r\\u2028\\u2029\\u1680\\u180e\\u2000\\u2001\\u2002\\u2003\\u2004\\u2005\\u2006\\u2007\\u2008\\u2009\\u200a\\u202f\\u205f\\u3000", rsUpperRange = "A-Z\\xc0-\\xd6\\xd8-\\xde", rsVarRange = "\\ufe0e\\ufe0f", rsBreakRange = rsMathOpRange + rsNonCharRange + rsPunctuationRange + rsSpaceRange;
	/** Used to compose unicode capture groups. */
	var rsApos = "['’]", rsAstral = "[" + rsAstralRange + "]", rsBreak = "[" + rsBreakRange + "]", rsCombo = "[" + rsComboMarksRange + rsComboSymbolsRange + "]", rsDigits = "\\d+", rsDingbat = "[" + rsDingbatRange + "]", rsLower = "[" + rsLowerRange + "]", rsMisc = "[^" + rsAstralRange + rsBreakRange + rsDigits + rsDingbatRange + rsLowerRange + rsUpperRange + "]", rsFitz = "\\ud83c[\\udffb-\\udfff]", rsModifier = "(?:" + rsCombo + "|" + rsFitz + ")", rsNonAstral = "[^" + rsAstralRange + "]", rsRegional = "(?:\\ud83c[\\udde6-\\uddff]){2}", rsSurrPair = "[\\ud800-\\udbff][\\udc00-\\udfff]", rsUpper = "[" + rsUpperRange + "]", rsZWJ = "\\u200d";
	/** Used to compose unicode regexes. */
	var rsLowerMisc = "(?:" + rsLower + "|" + rsMisc + ")", rsUpperMisc = "(?:" + rsUpper + "|" + rsMisc + ")", rsOptLowerContr = "(?:" + rsApos + "(?:d|ll|m|re|s|t|ve))?", rsOptUpperContr = "(?:" + rsApos + "(?:D|LL|M|RE|S|T|VE))?", reOptMod = rsModifier + "?", rsOptVar = "[" + rsVarRange + "]?", rsOptJoin = "(?:" + rsZWJ + "(?:" + [
		rsNonAstral,
		rsRegional,
		rsSurrPair
	].join("|") + ")" + rsOptVar + reOptMod + ")*", rsSeq = rsOptVar + reOptMod + rsOptJoin, rsEmoji = "(?:" + [
		rsDingbat,
		rsRegional,
		rsSurrPair
	].join("|") + ")" + rsSeq, rsSymbol = "(?:" + [
		rsNonAstral + rsCombo + "?",
		rsCombo,
		rsRegional,
		rsSurrPair,
		rsAstral
	].join("|") + ")";
	/** Used to match apostrophes. */
	var reApos = RegExp(rsApos, "g");
	/**
	* Used to match [combining diacritical marks](https://en.wikipedia.org/wiki/Combining_Diacritical_Marks) and
	* [combining diacritical marks for symbols](https://en.wikipedia.org/wiki/Combining_Diacritical_Marks_for_Symbols).
	*/
	var reComboMark = RegExp(rsCombo, "g");
	/** Used to match [string symbols](https://mathiasbynens.be/notes/javascript-unicode). */
	var reUnicode = RegExp(rsFitz + "(?=" + rsFitz + ")|" + rsSymbol + rsSeq, "g");
	/** Used to match complex or compound words. */
	var reUnicodeWord = RegExp([
		rsUpper + "?" + rsLower + "+" + rsOptLowerContr + "(?=" + [
			rsBreak,
			rsUpper,
			"$"
		].join("|") + ")",
		rsUpperMisc + "+" + rsOptUpperContr + "(?=" + [
			rsBreak,
			rsUpper + rsLowerMisc,
			"$"
		].join("|") + ")",
		rsUpper + "?" + rsLowerMisc + "+" + rsOptLowerContr,
		rsUpper + "+" + rsOptUpperContr,
		rsDigits,
		rsEmoji
	].join("|"), "g");
	/** Used to detect strings with [zero-width joiners or code points from the astral planes](http://eev.ee/blog/2015/09/12/dark-corners-of-unicode/). */
	var reHasUnicode = RegExp("[" + rsZWJ + rsAstralRange + rsComboMarksRange + rsComboSymbolsRange + rsVarRange + "]");
	/** Used to detect strings that need a more robust regexp to match words. */
	var reHasUnicodeWord = /[a-z][A-Z]|[A-Z]{2,}[a-z]|[0-9][a-zA-Z]|[a-zA-Z][0-9]|[^a-zA-Z0-9 ]/;
	/** Used to map Latin Unicode letters to basic Latin letters. */
	var deburredLetters = {
		"À": "A",
		"Á": "A",
		"Â": "A",
		"Ã": "A",
		"Ä": "A",
		"Å": "A",
		"à": "a",
		"á": "a",
		"â": "a",
		"ã": "a",
		"ä": "a",
		"å": "a",
		"Ç": "C",
		"ç": "c",
		"Ð": "D",
		"ð": "d",
		"È": "E",
		"É": "E",
		"Ê": "E",
		"Ë": "E",
		"è": "e",
		"é": "e",
		"ê": "e",
		"ë": "e",
		"Ì": "I",
		"Í": "I",
		"Î": "I",
		"Ï": "I",
		"ì": "i",
		"í": "i",
		"î": "i",
		"ï": "i",
		"Ñ": "N",
		"ñ": "n",
		"Ò": "O",
		"Ó": "O",
		"Ô": "O",
		"Õ": "O",
		"Ö": "O",
		"Ø": "O",
		"ò": "o",
		"ó": "o",
		"ô": "o",
		"õ": "o",
		"ö": "o",
		"ø": "o",
		"Ù": "U",
		"Ú": "U",
		"Û": "U",
		"Ü": "U",
		"ù": "u",
		"ú": "u",
		"û": "u",
		"ü": "u",
		"Ý": "Y",
		"ý": "y",
		"ÿ": "y",
		"Æ": "Ae",
		"æ": "ae",
		"Þ": "Th",
		"þ": "th",
		"ß": "ss",
		"Ā": "A",
		"Ă": "A",
		"Ą": "A",
		"ā": "a",
		"ă": "a",
		"ą": "a",
		"Ć": "C",
		"Ĉ": "C",
		"Ċ": "C",
		"Č": "C",
		"ć": "c",
		"ĉ": "c",
		"ċ": "c",
		"č": "c",
		"Ď": "D",
		"Đ": "D",
		"ď": "d",
		"đ": "d",
		"Ē": "E",
		"Ĕ": "E",
		"Ė": "E",
		"Ę": "E",
		"Ě": "E",
		"ē": "e",
		"ĕ": "e",
		"ė": "e",
		"ę": "e",
		"ě": "e",
		"Ĝ": "G",
		"Ğ": "G",
		"Ġ": "G",
		"Ģ": "G",
		"ĝ": "g",
		"ğ": "g",
		"ġ": "g",
		"ģ": "g",
		"Ĥ": "H",
		"Ħ": "H",
		"ĥ": "h",
		"ħ": "h",
		"Ĩ": "I",
		"Ī": "I",
		"Ĭ": "I",
		"Į": "I",
		"İ": "I",
		"ĩ": "i",
		"ī": "i",
		"ĭ": "i",
		"į": "i",
		"ı": "i",
		"Ĵ": "J",
		"ĵ": "j",
		"Ķ": "K",
		"ķ": "k",
		"ĸ": "k",
		"Ĺ": "L",
		"Ļ": "L",
		"Ľ": "L",
		"Ŀ": "L",
		"Ł": "L",
		"ĺ": "l",
		"ļ": "l",
		"ľ": "l",
		"ŀ": "l",
		"ł": "l",
		"Ń": "N",
		"Ņ": "N",
		"Ň": "N",
		"Ŋ": "N",
		"ń": "n",
		"ņ": "n",
		"ň": "n",
		"ŋ": "n",
		"Ō": "O",
		"Ŏ": "O",
		"Ő": "O",
		"ō": "o",
		"ŏ": "o",
		"ő": "o",
		"Ŕ": "R",
		"Ŗ": "R",
		"Ř": "R",
		"ŕ": "r",
		"ŗ": "r",
		"ř": "r",
		"Ś": "S",
		"Ŝ": "S",
		"Ş": "S",
		"Š": "S",
		"ś": "s",
		"ŝ": "s",
		"ş": "s",
		"š": "s",
		"Ţ": "T",
		"Ť": "T",
		"Ŧ": "T",
		"ţ": "t",
		"ť": "t",
		"ŧ": "t",
		"Ũ": "U",
		"Ū": "U",
		"Ŭ": "U",
		"Ů": "U",
		"Ű": "U",
		"Ų": "U",
		"ũ": "u",
		"ū": "u",
		"ŭ": "u",
		"ů": "u",
		"ű": "u",
		"ų": "u",
		"Ŵ": "W",
		"ŵ": "w",
		"Ŷ": "Y",
		"ŷ": "y",
		"Ÿ": "Y",
		"Ź": "Z",
		"Ż": "Z",
		"Ž": "Z",
		"ź": "z",
		"ż": "z",
		"ž": "z",
		"Ĳ": "IJ",
		"ĳ": "ij",
		"Œ": "Oe",
		"œ": "oe",
		"ŉ": "'n",
		"ſ": "ss"
	};
	/** Used as a reference to the global object. */
	var root = typeof global == "object" && global && global.Object === Object && global || typeof self == "object" && self && self.Object === Object && self || Function("return this")();
	/**
	* A specialized version of `_.reduce` for arrays without support for
	* iteratee shorthands.
	*
	* @private
	* @param {Array} [array] The array to iterate over.
	* @param {Function} iteratee The function invoked per iteration.
	* @param {*} [accumulator] The initial value.
	* @param {boolean} [initAccum] Specify using the first element of `array` as
	*  the initial value.
	* @returns {*} Returns the accumulated value.
	*/
	function arrayReduce(array, iteratee, accumulator, initAccum) {
		var index = -1, length = array ? array.length : 0;
		if (initAccum && length) accumulator = array[++index];
		while (++index < length) accumulator = iteratee(accumulator, array[index], index, array);
		return accumulator;
	}
	/**
	* Converts an ASCII `string` to an array.
	*
	* @private
	* @param {string} string The string to convert.
	* @returns {Array} Returns the converted array.
	*/
	function asciiToArray(string) {
		return string.split("");
	}
	/**
	* Splits an ASCII `string` into an array of its words.
	*
	* @private
	* @param {string} The string to inspect.
	* @returns {Array} Returns the words of `string`.
	*/
	function asciiWords(string) {
		return string.match(reAsciiWord) || [];
	}
	/**
	* The base implementation of `_.propertyOf` without support for deep paths.
	*
	* @private
	* @param {Object} object The object to query.
	* @returns {Function} Returns the new accessor function.
	*/
	function basePropertyOf(object) {
		return function(key) {
			return object == null ? void 0 : object[key];
		};
	}
	/**
	* Used by `_.deburr` to convert Latin-1 Supplement and Latin Extended-A
	* letters to basic Latin letters.
	*
	* @private
	* @param {string} letter The matched letter to deburr.
	* @returns {string} Returns the deburred letter.
	*/
	var deburrLetter = basePropertyOf(deburredLetters);
	/**
	* Checks if `string` contains Unicode symbols.
	*
	* @private
	* @param {string} string The string to inspect.
	* @returns {boolean} Returns `true` if a symbol is found, else `false`.
	*/
	function hasUnicode(string) {
		return reHasUnicode.test(string);
	}
	/**
	* Checks if `string` contains a word composed of Unicode symbols.
	*
	* @private
	* @param {string} string The string to inspect.
	* @returns {boolean} Returns `true` if a word is found, else `false`.
	*/
	function hasUnicodeWord(string) {
		return reHasUnicodeWord.test(string);
	}
	/**
	* Converts `string` to an array.
	*
	* @private
	* @param {string} string The string to convert.
	* @returns {Array} Returns the converted array.
	*/
	function stringToArray(string) {
		return hasUnicode(string) ? unicodeToArray(string) : asciiToArray(string);
	}
	/**
	* Converts a Unicode `string` to an array.
	*
	* @private
	* @param {string} string The string to convert.
	* @returns {Array} Returns the converted array.
	*/
	function unicodeToArray(string) {
		return string.match(reUnicode) || [];
	}
	/**
	* Splits a Unicode `string` into an array of its words.
	*
	* @private
	* @param {string} The string to inspect.
	* @returns {Array} Returns the words of `string`.
	*/
	function unicodeWords(string) {
		return string.match(reUnicodeWord) || [];
	}
	/**
	* Used to resolve the
	* [`toStringTag`](http://ecma-international.org/ecma-262/7.0/#sec-object.prototype.tostring)
	* of values.
	*/
	var objectToString = Object.prototype.toString;
	/** Built-in value references. */
	var Symbol = root.Symbol;
	/** Used to convert symbols to primitives and strings. */
	var symbolProto = Symbol ? Symbol.prototype : void 0, symbolToString = symbolProto ? symbolProto.toString : void 0;
	/**
	* The base implementation of `_.slice` without an iteratee call guard.
	*
	* @private
	* @param {Array} array The array to slice.
	* @param {number} [start=0] The start position.
	* @param {number} [end=array.length] The end position.
	* @returns {Array} Returns the slice of `array`.
	*/
	function baseSlice(array, start, end) {
		var index = -1, length = array.length;
		if (start < 0) start = -start > length ? 0 : length + start;
		end = end > length ? length : end;
		if (end < 0) end += length;
		length = start > end ? 0 : end - start >>> 0;
		start >>>= 0;
		var result = Array(length);
		while (++index < length) result[index] = array[index + start];
		return result;
	}
	/**
	* The base implementation of `_.toString` which doesn't convert nullish
	* values to empty strings.
	*
	* @private
	* @param {*} value The value to process.
	* @returns {string} Returns the string.
	*/
	function baseToString(value) {
		if (typeof value == "string") return value;
		if (isSymbol(value)) return symbolToString ? symbolToString.call(value) : "";
		var result = value + "";
		return result == "0" && 1 / value == -INFINITY ? "-0" : result;
	}
	/**
	* Casts `array` to a slice if it's needed.
	*
	* @private
	* @param {Array} array The array to inspect.
	* @param {number} start The start position.
	* @param {number} [end=array.length] The end position.
	* @returns {Array} Returns the cast slice.
	*/
	function castSlice(array, start, end) {
		var length = array.length;
		end = end === void 0 ? length : end;
		return !start && end >= length ? array : baseSlice(array, start, end);
	}
	/**
	* Creates a function like `_.lowerFirst`.
	*
	* @private
	* @param {string} methodName The name of the `String` case method to use.
	* @returns {Function} Returns the new case function.
	*/
	function createCaseFirst(methodName) {
		return function(string) {
			string = toString(string);
			var strSymbols = hasUnicode(string) ? stringToArray(string) : void 0;
			var chr = strSymbols ? strSymbols[0] : string.charAt(0);
			var trailing = strSymbols ? castSlice(strSymbols, 1).join("") : string.slice(1);
			return chr[methodName]() + trailing;
		};
	}
	/**
	* Creates a function like `_.camelCase`.
	*
	* @private
	* @param {Function} callback The function to combine each word.
	* @returns {Function} Returns the new compounder function.
	*/
	function createCompounder(callback) {
		return function(string) {
			return arrayReduce(words(deburr(string).replace(reApos, "")), callback, "");
		};
	}
	/**
	* Checks if `value` is object-like. A value is object-like if it's not `null`
	* and has a `typeof` result of "object".
	*
	* @static
	* @memberOf _
	* @since 4.0.0
	* @category Lang
	* @param {*} value The value to check.
	* @returns {boolean} Returns `true` if `value` is object-like, else `false`.
	* @example
	*
	* _.isObjectLike({});
	* // => true
	*
	* _.isObjectLike([1, 2, 3]);
	* // => true
	*
	* _.isObjectLike(_.noop);
	* // => false
	*
	* _.isObjectLike(null);
	* // => false
	*/
	function isObjectLike(value) {
		return !!value && typeof value == "object";
	}
	/**
	* Checks if `value` is classified as a `Symbol` primitive or object.
	*
	* @static
	* @memberOf _
	* @since 4.0.0
	* @category Lang
	* @param {*} value The value to check.
	* @returns {boolean} Returns `true` if `value` is a symbol, else `false`.
	* @example
	*
	* _.isSymbol(Symbol.iterator);
	* // => true
	*
	* _.isSymbol('abc');
	* // => false
	*/
	function isSymbol(value) {
		return typeof value == "symbol" || isObjectLike(value) && objectToString.call(value) == symbolTag;
	}
	/**
	* Converts `value` to a string. An empty string is returned for `null`
	* and `undefined` values. The sign of `-0` is preserved.
	*
	* @static
	* @memberOf _
	* @since 4.0.0
	* @category Lang
	* @param {*} value The value to process.
	* @returns {string} Returns the string.
	* @example
	*
	* _.toString(null);
	* // => ''
	*
	* _.toString(-0);
	* // => '-0'
	*
	* _.toString([1, 2, 3]);
	* // => '1,2,3'
	*/
	function toString(value) {
		return value == null ? "" : baseToString(value);
	}
	/**
	* Converts `string` to [camel case](https://en.wikipedia.org/wiki/CamelCase).
	*
	* @static
	* @memberOf _
	* @since 3.0.0
	* @category String
	* @param {string} [string=''] The string to convert.
	* @returns {string} Returns the camel cased string.
	* @example
	*
	* _.camelCase('Foo Bar');
	* // => 'fooBar'
	*
	* _.camelCase('--foo-bar--');
	* // => 'fooBar'
	*
	* _.camelCase('__FOO_BAR__');
	* // => 'fooBar'
	*/
	var camelCase = createCompounder(function(result, word, index) {
		word = word.toLowerCase();
		return result + (index ? capitalize(word) : word);
	});
	/**
	* Converts the first character of `string` to upper case and the remaining
	* to lower case.
	*
	* @static
	* @memberOf _
	* @since 3.0.0
	* @category String
	* @param {string} [string=''] The string to capitalize.
	* @returns {string} Returns the capitalized string.
	* @example
	*
	* _.capitalize('FRED');
	* // => 'Fred'
	*/
	function capitalize(string) {
		return upperFirst(toString(string).toLowerCase());
	}
	/**
	* Deburrs `string` by converting
	* [Latin-1 Supplement](https://en.wikipedia.org/wiki/Latin-1_Supplement_(Unicode_block)#Character_table)
	* and [Latin Extended-A](https://en.wikipedia.org/wiki/Latin_Extended-A)
	* letters to basic Latin letters and removing
	* [combining diacritical marks](https://en.wikipedia.org/wiki/Combining_Diacritical_Marks).
	*
	* @static
	* @memberOf _
	* @since 3.0.0
	* @category String
	* @param {string} [string=''] The string to deburr.
	* @returns {string} Returns the deburred string.
	* @example
	*
	* _.deburr('déjà vu');
	* // => 'deja vu'
	*/
	function deburr(string) {
		string = toString(string);
		return string && string.replace(reLatin, deburrLetter).replace(reComboMark, "");
	}
	/**
	* Converts the first character of `string` to upper case.
	*
	* @static
	* @memberOf _
	* @since 4.0.0
	* @category String
	* @param {string} [string=''] The string to convert.
	* @returns {string} Returns the converted string.
	* @example
	*
	* _.upperFirst('fred');
	* // => 'Fred'
	*
	* _.upperFirst('FRED');
	* // => 'FRED'
	*/
	var upperFirst = createCaseFirst("toUpperCase");
	/**
	* Splits `string` into an array of its words.
	*
	* @static
	* @memberOf _
	* @since 3.0.0
	* @category String
	* @param {string} [string=''] The string to inspect.
	* @param {RegExp|string} [pattern] The pattern to match words.
	* @param- {Object} [guard] Enables use as an iteratee for methods like `_.map`.
	* @returns {Array} Returns the words of `string`.
	* @example
	*
	* _.words('fred, barney, & pebbles');
	* // => ['fred', 'barney', 'pebbles']
	*
	* _.words('fred, barney, & pebbles', /[^, ]+/g);
	* // => ['fred', 'barney', '&', 'pebbles']
	*/
	function words(string, pattern, guard) {
		string = toString(string);
		pattern = guard ? void 0 : pattern;
		if (pattern === void 0) return hasUnicodeWord(string) ? unicodeWords(string) : asciiWords(string);
		return string.match(pattern) || [];
	}
	module.exports = camelCase;
}));
//#endregion
//#region ../../node_modules/.pnpm/postcss-modules@6.0.1_postcss@8.5.15/node_modules/postcss-modules/build/localsConvention.js
var require_localsConvention = /* @__PURE__ */ __commonJSMin(((exports) => {
	Object.defineProperty(exports, "__esModule", { value: true });
	exports.makeLocalsConventionReducer = makeLocalsConventionReducer;
	var _lodash = _interopRequireDefault(require_lodash_camelcase());
	function _interopRequireDefault(obj) {
		return obj && obj.__esModule ? obj : { default: obj };
	}
	function dashesCamelCase(string) {
		return string.replace(/-+(\w)/g, (_, firstLetter) => firstLetter.toUpperCase());
	}
	function makeLocalsConventionReducer(localsConvention, inputFile) {
		const isFunc = typeof localsConvention === "function";
		return (tokens, [className, value]) => {
			if (isFunc) {
				const convention = localsConvention(className, value, inputFile);
				tokens[convention] = value;
				return tokens;
			}
			switch (localsConvention) {
				case "camelCase":
					tokens[className] = value;
					tokens[(0, _lodash.default)(className)] = value;
					break;
				case "camelCaseOnly":
					tokens[(0, _lodash.default)(className)] = value;
					break;
				case "dashes":
					tokens[className] = value;
					tokens[dashesCamelCase(className)] = value;
					break;
				case "dashesOnly":
					tokens[dashesCamelCase(className)] = value;
					break;
			}
			return tokens;
		};
	}
}));
//#endregion
//#region ../../node_modules/.pnpm/postcss-modules@6.0.1_postcss@8.5.15/node_modules/postcss-modules/build/FileSystemLoader.js
var require_FileSystemLoader = /* @__PURE__ */ __commonJSMin(((exports) => {
	Object.defineProperty(exports, "__esModule", { value: true });
	exports.default = void 0;
	var _postcss$1 = _interopRequireDefault(__require("postcss"));
	var _path = _interopRequireDefault(__require("path"));
	var _Parser = _interopRequireDefault(require_Parser());
	var _fs = require_fs();
	function _interopRequireDefault(obj) {
		return obj && obj.__esModule ? obj : { default: obj };
	}
	var Core = class Core {
		constructor(plugins) {
			this.plugins = plugins || Core.defaultPlugins;
		}
		async load(sourceString, sourcePath, trace, pathFetcher) {
			const parser = new _Parser.default(pathFetcher, trace);
			const plugins = this.plugins.concat([parser.plugin()]);
			return {
				injectableSource: (await (0, _postcss$1.default)(plugins).process(sourceString, { from: sourcePath })).css,
				exportTokens: parser.exportTokens
			};
		}
	};
	const traceKeySorter = (a, b) => {
		if (a.length < b.length) return a < b.substring(0, a.length) ? -1 : 1;
		if (a.length > b.length) return a.substring(0, b.length) <= b ? -1 : 1;
		return a < b ? -1 : 1;
	};
	var FileSystemLoader = class {
		constructor(root, plugins, fileResolve) {
			if (root === "/" && process.platform === "win32") {
				const cwdDrive = process.cwd().slice(0, 3);
				if (!/^[A-Za-z]:\\$/.test(cwdDrive)) throw new Error(`Failed to obtain root from "${process.cwd()}".`);
				root = cwdDrive;
			}
			this.root = root;
			this.fileResolve = fileResolve;
			this.sources = {};
			this.traces = {};
			this.importNr = 0;
			this.core = new Core(plugins);
			this.tokensByFile = {};
			this.fs = (0, _fs.getFileSystem)();
		}
		async fetch(_newPath, relativeTo, _trace) {
			const newPath = _newPath.replace(/^["']|["']$/g, "");
			const trace = _trace || String.fromCharCode(this.importNr++);
			const useFileResolve = typeof this.fileResolve === "function";
			const fileResolvedPath = useFileResolve ? await this.fileResolve(newPath, relativeTo) : await Promise.resolve();
			if (fileResolvedPath && !_path.default.isAbsolute(fileResolvedPath)) throw new Error("The returned path from the \"fileResolve\" option must be absolute.");
			const relativeDir = _path.default.dirname(relativeTo);
			const rootRelativePath = fileResolvedPath || _path.default.resolve(relativeDir, newPath);
			let fileRelativePath = fileResolvedPath || _path.default.resolve(_path.default.resolve(this.root, relativeDir), newPath);
			if (!useFileResolve && newPath[0] !== "." && !_path.default.isAbsolute(newPath)) try {
				fileRelativePath = __require.resolve(newPath);
			} catch (e) {}
			const tokens = this.tokensByFile[fileRelativePath];
			if (tokens) return tokens;
			return new Promise((resolve, reject) => {
				this.fs.readFile(fileRelativePath, "utf-8", async (err, source) => {
					if (err) reject(err);
					const { injectableSource, exportTokens } = await this.core.load(source, rootRelativePath, trace, this.fetch.bind(this));
					this.sources[fileRelativePath] = injectableSource;
					this.traces[trace] = fileRelativePath;
					this.tokensByFile[fileRelativePath] = exportTokens;
					resolve(exportTokens);
				});
			});
		}
		get finalSource() {
			const traces = this.traces;
			const sources = this.sources;
			let written = /* @__PURE__ */ new Set();
			return Object.keys(traces).sort(traceKeySorter).map((key) => {
				const filename = traces[key];
				if (written.has(filename)) return null;
				written.add(filename);
				return sources[filename];
			}).join("");
		}
	};
	exports.default = FileSystemLoader;
}));
//#endregion
//#region ../../node_modules/.pnpm/postcss-modules-extract-imports@3.1.0_postcss@8.5.15/node_modules/postcss-modules-extract-imports/src/topologicalSort.js
var require_topologicalSort = /* @__PURE__ */ __commonJSMin(((exports, module) => {
	const PERMANENT_MARKER = 2;
	const TEMPORARY_MARKER = 1;
	function createError(node, graph) {
		const er = /* @__PURE__ */ new Error("Nondeterministic import's order");
		er.nodes = [node, graph[node].find((relatedNode) => graph[relatedNode].indexOf(node) > -1)];
		return er;
	}
	function walkGraph(node, graph, state, result, strict) {
		if (state[node] === PERMANENT_MARKER) return;
		if (state[node] === TEMPORARY_MARKER) {
			if (strict) return createError(node, graph);
			return;
		}
		state[node] = TEMPORARY_MARKER;
		const children = graph[node];
		const length = children.length;
		for (let i = 0; i < length; ++i) {
			const error = walkGraph(children[i], graph, state, result, strict);
			if (error instanceof Error) return error;
		}
		state[node] = PERMANENT_MARKER;
		result.push(node);
	}
	function topologicalSort(graph, strict) {
		const result = [];
		const state = {};
		const nodes = Object.keys(graph);
		const length = nodes.length;
		for (let i = 0; i < length; ++i) {
			const er = walkGraph(nodes[i], graph, state, result, strict);
			if (er instanceof Error) return er;
		}
		return result;
	}
	module.exports = topologicalSort;
}));
//#endregion
//#region ../../node_modules/.pnpm/postcss-modules-extract-imports@3.1.0_postcss@8.5.15/node_modules/postcss-modules-extract-imports/src/index.js
var require_src$3 = /* @__PURE__ */ __commonJSMin(((exports, module) => {
	const topologicalSort = require_topologicalSort();
	const matchImports = /^(.+?)\s+from\s+(?:"([^"]+)"|'([^']+)'|(global))$/;
	const icssImport = /^:import\((?:"([^"]+)"|'([^']+)')\)/;
	const VISITED_MARKER = 1;
	/**
	* :import('G') {}
	*
	* Rule
	*   composes: ... from 'A'
	*   composes: ... from 'B'
	
	* Rule
	*   composes: ... from 'A'
	*   composes: ... from 'A'
	*   composes: ... from 'C'
	*
	* Results in:
	*
	* graph: {
	*   G: [],
	*   A: [],
	*   B: ['A'],
	*   C: ['A'],
	* }
	*/
	function addImportToGraph(importId, parentId, graph, visited) {
		const siblingsId = parentId + "_siblings";
		const visitedId = parentId + "_" + importId;
		if (visited[visitedId] !== VISITED_MARKER) {
			if (!Array.isArray(visited[siblingsId])) visited[siblingsId] = [];
			const siblings = visited[siblingsId];
			if (Array.isArray(graph[importId])) graph[importId] = graph[importId].concat(siblings);
			else graph[importId] = siblings.slice();
			visited[visitedId] = VISITED_MARKER;
			siblings.push(importId);
		}
	}
	module.exports = (options = {}) => {
		let importIndex = 0;
		const createImportedName = typeof options.createImportedName !== "function" ? (importName) => `i__imported_${importName.replace(/\W/g, "_")}_${importIndex++}` : options.createImportedName;
		const failOnWrongOrder = options.failOnWrongOrder;
		return {
			postcssPlugin: "postcss-modules-extract-imports",
			prepare() {
				const graph = {};
				const visited = {};
				const existingImports = {};
				const importDecls = {};
				const imports = {};
				return { Once(root, postcss) {
					root.walkRules((rule) => {
						const matches = icssImport.exec(rule.selector);
						if (matches) {
							const [, doubleQuotePath, singleQuotePath] = matches;
							const importPath = doubleQuotePath || singleQuotePath;
							addImportToGraph(importPath, "root", graph, visited);
							existingImports[importPath] = rule;
						}
					});
					root.walkDecls(/^composes$/, (declaration) => {
						const multiple = declaration.value.split(",");
						const values = [];
						multiple.forEach((value) => {
							const matches = value.trim().match(matchImports);
							if (!matches) {
								values.push(value);
								return;
							}
							let tmpSymbols;
							let [, symbols, doubleQuotePath, singleQuotePath, global] = matches;
							if (global) tmpSymbols = symbols.split(/\s+/).map((s) => `global(${s})`);
							else {
								const importPath = doubleQuotePath || singleQuotePath;
								let parent = declaration.parent;
								let parentIndexes = "";
								while (parent.type !== "root") {
									parentIndexes = parent.parent.index(parent) + "_" + parentIndexes;
									parent = parent.parent;
								}
								const { selector } = declaration.parent;
								addImportToGraph(importPath, `_${parentIndexes}${selector}`, graph, visited);
								importDecls[importPath] = declaration;
								imports[importPath] = imports[importPath] || {};
								tmpSymbols = symbols.split(/\s+/).map((s) => {
									if (!imports[importPath][s]) imports[importPath][s] = createImportedName(s, importPath);
									return imports[importPath][s];
								});
							}
							values.push(tmpSymbols.join(" "));
						});
						declaration.value = values.join(", ");
					});
					const importsOrder = topologicalSort(graph, failOnWrongOrder);
					if (importsOrder instanceof Error) throw importDecls[importsOrder.nodes.find((importPath) => importDecls.hasOwnProperty(importPath))].error("Failed to resolve order of composed modules " + importsOrder.nodes.map((importPath) => "`" + importPath + "`").join(", ") + ".", {
						plugin: "postcss-modules-extract-imports",
						word: "composes"
					});
					let lastImportRule;
					importsOrder.forEach((path) => {
						const importedSymbols = imports[path];
						let rule = existingImports[path];
						if (!rule && importedSymbols) {
							rule = postcss.rule({
								selector: `:import("${path}")`,
								raws: { after: "\n" }
							});
							if (lastImportRule) root.insertAfter(lastImportRule, rule);
							else root.prepend(rule);
						}
						lastImportRule = rule;
						if (!importedSymbols) return;
						Object.keys(importedSymbols).forEach((importedSymbol) => {
							rule.append(postcss.decl({
								value: importedSymbol,
								prop: importedSymbols[importedSymbol],
								raws: { before: "\n  " }
							}));
						});
					});
				} };
			}
		};
	};
	module.exports.postcss = true;
}));
//#endregion
//#region ../../node_modules/.pnpm/loader-utils@3.3.1/node_modules/loader-utils/lib/hash/wasm-hash.js
var require_wasm_hash = /* @__PURE__ */ __commonJSMin(((exports, module) => {
	const MAX_SHORT_STRING = Math.floor(65472 / 4) & -4;
	var WasmHash = class {
		/**
		* @param {WebAssembly.Instance} instance wasm instance
		* @param {WebAssembly.Instance[]} instancesPool pool of instances
		* @param {number} chunkSize size of data chunks passed to wasm
		* @param {number} digestSize size of digest returned by wasm
		*/
		constructor(instance, instancesPool, chunkSize, digestSize) {
			const exports$2 = instance.exports;
			exports$2.init();
			this.exports = exports$2;
			this.mem = Buffer.from(exports$2.memory.buffer, 0, 65536);
			this.buffered = 0;
			this.instancesPool = instancesPool;
			this.chunkSize = chunkSize;
			this.digestSize = digestSize;
		}
		reset() {
			this.buffered = 0;
			this.exports.init();
		}
		/**
		* @param {Buffer | string} data data
		* @param {BufferEncoding=} encoding encoding
		* @returns {this} itself
		*/
		update(data, encoding) {
			if (typeof data === "string") {
				while (data.length > MAX_SHORT_STRING) {
					this._updateWithShortString(data.slice(0, MAX_SHORT_STRING), encoding);
					data = data.slice(MAX_SHORT_STRING);
				}
				this._updateWithShortString(data, encoding);
				return this;
			}
			this._updateWithBuffer(data);
			return this;
		}
		/**
		* @param {string} data data
		* @param {BufferEncoding=} encoding encoding
		* @returns {void}
		*/
		_updateWithShortString(data, encoding) {
			const { exports: exports$3, buffered, mem, chunkSize } = this;
			let endPos;
			if (data.length < 70) if (!encoding || encoding === "utf-8" || encoding === "utf8") {
				endPos = buffered;
				for (let i = 0; i < data.length; i++) {
					const cc = data.charCodeAt(i);
					if (cc < 128) mem[endPos++] = cc;
					else if (cc < 2048) {
						mem[endPos] = cc >> 6 | 192;
						mem[endPos + 1] = cc & 63 | 128;
						endPos += 2;
					} else {
						endPos += mem.write(data.slice(i), endPos, encoding);
						break;
					}
				}
			} else if (encoding === "latin1") {
				endPos = buffered;
				for (let i = 0; i < data.length; i++) {
					const cc = data.charCodeAt(i);
					mem[endPos++] = cc;
				}
			} else endPos = buffered + mem.write(data, buffered, encoding);
			else endPos = buffered + mem.write(data, buffered, encoding);
			if (endPos < chunkSize) this.buffered = endPos;
			else {
				const l = endPos & ~(this.chunkSize - 1);
				exports$3.update(l);
				const newBuffered = endPos - l;
				this.buffered = newBuffered;
				if (newBuffered > 0) mem.copyWithin(0, l, endPos);
			}
		}
		/**
		* @param {Buffer} data data
		* @returns {void}
		*/
		_updateWithBuffer(data) {
			const { exports: exports$4, buffered, mem } = this;
			const length = data.length;
			if (buffered + length < this.chunkSize) {
				data.copy(mem, buffered, 0, length);
				this.buffered += length;
			} else {
				const l = buffered + length & ~(this.chunkSize - 1);
				if (l > 65536) {
					let i = 65536 - buffered;
					data.copy(mem, buffered, 0, i);
					exports$4.update(65536);
					const stop = l - buffered - 65536;
					while (i < stop) {
						data.copy(mem, 0, i, i + 65536);
						exports$4.update(65536);
						i += 65536;
					}
					data.copy(mem, 0, i, l - buffered);
					exports$4.update(l - buffered - i);
				} else {
					data.copy(mem, buffered, 0, l - buffered);
					exports$4.update(l);
				}
				const newBuffered = length + buffered - l;
				this.buffered = newBuffered;
				if (newBuffered > 0) data.copy(mem, 0, length - newBuffered, length);
			}
		}
		digest(type) {
			const { exports: exports$5, buffered, mem, digestSize } = this;
			exports$5.final(buffered);
			this.instancesPool.push(this);
			const hex = mem.toString("latin1", 0, digestSize);
			if (type === "hex") return hex;
			if (type === "binary" || !type) return Buffer.from(hex, "hex");
			return Buffer.from(hex, "hex").toString(type);
		}
	};
	const create = (wasmModule, instancesPool, chunkSize, digestSize) => {
		if (instancesPool.length > 0) {
			const old = instancesPool.pop();
			old.reset();
			return old;
		} else return new WasmHash(new WebAssembly.Instance(wasmModule), instancesPool, chunkSize, digestSize);
	};
	module.exports = create;
	module.exports.MAX_SHORT_STRING = MAX_SHORT_STRING;
}));
//#endregion
//#region ../../node_modules/.pnpm/loader-utils@3.3.1/node_modules/loader-utils/lib/hash/xxhash64.js
var require_xxhash64 = /* @__PURE__ */ __commonJSMin(((exports, module) => {
	const create = require_wasm_hash();
	const xxhash64 = new WebAssembly.Module(Buffer.from("AGFzbQEAAAABCAJgAX8AYAAAAwQDAQAABQMBAAEGGgV+AUIAC34BQgALfgFCAAt+AUIAC34BQgALByIEBGluaXQAAAZ1cGRhdGUAAQVmaW5hbAACBm1lbW9yeQIACrUIAzAAQtbrgu7q/Yn14AAkAELP1tO+0ser2UIkAUIAJAJC+erQ0OfJoeThACQDQgAkBAvUAQIBfwR+IABFBEAPCyMEIACtfCQEIwAhAiMBIQMjAiEEIwMhBQNAIAIgASkDAELP1tO+0ser2UJ+fEIfiUKHla+vmLbem55/fiECIAMgASkDCELP1tO+0ser2UJ+fEIfiUKHla+vmLbem55/fiEDIAQgASkDEELP1tO+0ser2UJ+fEIfiUKHla+vmLbem55/fiEEIAUgASkDGELP1tO+0ser2UJ+fEIfiUKHla+vmLbem55/fiEFIAAgAUEgaiIBSw0ACyACJAAgAyQBIAQkAiAFJAMLqwYCAX8EfiMEQgBSBH4jACICQgGJIwEiA0IHiXwjAiIEQgyJfCMDIgVCEol8IAJCz9bTvtLHq9lCfkIfiUKHla+vmLbem55/foVCh5Wvr5i23puef35CnaO16oOxjYr6AH0gA0LP1tO+0ser2UJ+Qh+JQoeVr6+Ytt6bnn9+hUKHla+vmLbem55/fkKdo7Xqg7GNivoAfSAEQs/W077Sx6vZQn5CH4lCh5Wvr5i23puef36FQoeVr6+Ytt6bnn9+Qp2jteqDsY2K+gB9IAVCz9bTvtLHq9lCfkIfiUKHla+vmLbem55/foVCh5Wvr5i23puef35CnaO16oOxjYr6AH0FQsXP2bLx5brqJwsjBCAArXx8IQIDQCABQQhqIABNBEAgAiABKQMAQs/W077Sx6vZQn5CH4lCh5Wvr5i23puef36FQhuJQoeVr6+Ytt6bnn9+Qp2jteqDsY2K+gB9IQIgAUEIaiEBDAELCyABQQRqIABNBEACfyACIAE1AgBCh5Wvr5i23puef36FQheJQs/W077Sx6vZQn5C+fPd8Zn2masWfCECIAFBBGoLIQELA0AgACABRwRAIAIgATEAAELFz9my8eW66id+hUILiUKHla+vmLbem55/fiECIAFBAWohAQwBCwtBACACIAJCIYiFQs/W077Sx6vZQn4iAiACQh2IhUL5893xmfaZqxZ+IgIgAkIgiIUiAkIgiCIDQv//A4NCIIYgA0KAgPz/D4NCEIiEIgNC/4GAgPAfg0IQhiADQoD+g4CA4D+DQgiIhCIDQo+AvIDwgcAHg0IIhiADQvCBwIeAnoD4AINCBIiEIgNChoyYsODAgYMGfEIEiEKBgoSIkKDAgAGDQid+IANCsODAgYOGjJgwhHw3AwBBCCACQv////8PgyICQv//A4NCIIYgAkKAgPz/D4NCEIiEIgJC/4GAgPAfg0IQhiACQoD+g4CA4D+DQgiIhCICQo+AvIDwgcAHg0IIhiACQvCBwIeAnoD4AINCBIiEIgJChoyYsODAgYMGfEIEiEKBgoSIkKDAgAGDQid+IAJCsODAgYOGjJgwhHw3AwAL", "base64"));
	module.exports = create.bind(null, xxhash64, [], 32, 16);
}));
//#endregion
//#region ../../node_modules/.pnpm/loader-utils@3.3.1/node_modules/loader-utils/lib/hash/BatchedHash.js
var require_BatchedHash = /* @__PURE__ */ __commonJSMin(((exports, module) => {
	const MAX_SHORT_STRING = require_wasm_hash().MAX_SHORT_STRING;
	var BatchedHash = class {
		constructor(hash) {
			this.string = void 0;
			this.encoding = void 0;
			this.hash = hash;
		}
		/**
		* Update hash {@link https://nodejs.org/api/crypto.html#crypto_hash_update_data_inputencoding}
		* @param {string|Buffer} data data
		* @param {string=} inputEncoding data encoding
		* @returns {this} updated hash
		*/
		update(data, inputEncoding) {
			if (this.string !== void 0) {
				if (typeof data === "string" && inputEncoding === this.encoding && this.string.length + data.length < MAX_SHORT_STRING) {
					this.string += data;
					return this;
				}
				this.hash.update(this.string, this.encoding);
				this.string = void 0;
			}
			if (typeof data === "string") if (data.length < MAX_SHORT_STRING && (!inputEncoding || !inputEncoding.startsWith("ba"))) {
				this.string = data;
				this.encoding = inputEncoding;
			} else this.hash.update(data, inputEncoding);
			else this.hash.update(data);
			return this;
		}
		/**
		* Calculates the digest {@link https://nodejs.org/api/crypto.html#crypto_hash_digest_encoding}
		* @param {string=} encoding encoding of the return value
		* @returns {string|Buffer} digest
		*/
		digest(encoding) {
			if (this.string !== void 0) this.hash.update(this.string, this.encoding);
			return this.hash.digest(encoding);
		}
	};
	module.exports = BatchedHash;
}));
//#endregion
//#region ../../node_modules/.pnpm/loader-utils@3.3.1/node_modules/loader-utils/lib/hash/md4.js
var require_md4 = /* @__PURE__ */ __commonJSMin(((exports, module) => {
	const create = require_wasm_hash();
	const md4 = new WebAssembly.Module(Buffer.from("AGFzbQEAAAABCAJgAX8AYAAAAwUEAQAAAAUDAQABBhoFfwFBAAt/AUEAC38BQQALfwFBAAt/AUEACwciBARpbml0AAAGdXBkYXRlAAIFZmluYWwAAwZtZW1vcnkCAAqFEAQmAEGBxpS6BiQBQYnXtv5+JAJB/rnrxXkkA0H2qMmBASQEQQAkAAvMCgEYfyMBIQojAiEGIwMhByMEIQgDQCAAIAVLBEAgBSgCCCINIAcgBiAFKAIEIgsgCCAHIAUoAgAiDCAKIAggBiAHIAhzcXNqakEDdyIDIAYgB3Nxc2pqQQd3IgEgAyAGc3FzampBC3chAiAFKAIUIg8gASACIAUoAhAiCSADIAEgBSgCDCIOIAYgAyACIAEgA3Nxc2pqQRN3IgQgASACc3FzampBA3ciAyACIARzcXNqakEHdyEBIAUoAiAiEiADIAEgBSgCHCIRIAQgAyAFKAIYIhAgAiAEIAEgAyAEc3FzampBC3ciAiABIANzcXNqakETdyIEIAEgAnNxc2pqQQN3IQMgBSgCLCIVIAQgAyAFKAIoIhQgAiAEIAUoAiQiEyABIAIgAyACIARzcXNqakEHdyIBIAMgBHNxc2pqQQt3IgIgASADc3FzampBE3chBCAPIBAgCSAVIBQgEyAFKAI4IhYgAiAEIAUoAjQiFyABIAIgBSgCMCIYIAMgASAEIAEgAnNxc2pqQQN3IgEgAiAEc3FzampBB3ciAiABIARzcXNqakELdyIDIAkgAiAMIAEgBSgCPCIJIAQgASADIAEgAnNxc2pqQRN3IgEgAiADcnEgAiADcXJqakGZ84nUBWpBA3ciAiABIANycSABIANxcmpqQZnzidQFakEFdyIEIAEgAnJxIAEgAnFyaiASakGZ84nUBWpBCXciAyAPIAQgCyACIBggASADIAIgBHJxIAIgBHFyampBmfOJ1AVqQQ13IgEgAyAEcnEgAyAEcXJqakGZ84nUBWpBA3ciAiABIANycSABIANxcmpqQZnzidQFakEFdyIEIAEgAnJxIAEgAnFyampBmfOJ1AVqQQl3IgMgECAEIAIgFyABIAMgAiAEcnEgAiAEcXJqakGZ84nUBWpBDXciASADIARycSADIARxcmogDWpBmfOJ1AVqQQN3IgIgASADcnEgASADcXJqakGZ84nUBWpBBXciBCABIAJycSABIAJxcmpqQZnzidQFakEJdyIDIBEgBCAOIAIgFiABIAMgAiAEcnEgAiAEcXJqakGZ84nUBWpBDXciASADIARycSADIARxcmpqQZnzidQFakEDdyICIAEgA3JxIAEgA3FyampBmfOJ1AVqQQV3IgQgASACcnEgASACcXJqakGZ84nUBWpBCXciAyAMIAIgAyAJIAEgAyACIARycSACIARxcmpqQZnzidQFakENdyIBcyAEc2pqQaHX5/YGakEDdyICIAQgASACcyADc2ogEmpBodfn9gZqQQl3IgRzIAFzampBodfn9gZqQQt3IgMgAiADIBggASADIARzIAJzampBodfn9gZqQQ93IgFzIARzaiANakGh1+f2BmpBA3ciAiAUIAQgASACcyADc2pqQaHX5/YGakEJdyIEcyABc2pqQaHX5/YGakELdyIDIAsgAiADIBYgASADIARzIAJzampBodfn9gZqQQ93IgFzIARzampBodfn9gZqQQN3IgIgEyAEIAEgAnMgA3NqakGh1+f2BmpBCXciBHMgAXNqakGh1+f2BmpBC3chAyAKIA4gAiADIBcgASADIARzIAJzampBodfn9gZqQQ93IgFzIARzampBodfn9gZqQQN3IgJqIQogBiAJIAEgESADIAIgFSAEIAEgAnMgA3NqakGh1+f2BmpBCXciBHMgAXNqakGh1+f2BmpBC3ciAyAEcyACc2pqQaHX5/YGakEPd2ohBiADIAdqIQcgBCAIaiEIIAVBQGshBQwBCwsgCiQBIAYkAiAHJAMgCCQECw0AIAAQASMAIABqJAAL/wQCA38BfiMAIABqrUIDhiEEIABByABqQUBxIgJBCGshAyAAIgFBAWohACABQYABOgAAA0AgACACSUEAIABBB3EbBEAgAEEAOgAAIABBAWohAAwBCwsDQCAAIAJJBEAgAEIANwMAIABBCGohAAwBCwsgAyAENwMAIAIQAUEAIwGtIgRC//8DgyAEQoCA/P8Pg0IQhoQiBEL/gYCA8B+DIARCgP6DgIDgP4NCCIaEIgRCj4C8gPCBwAeDQgiGIARC8IHAh4CegPgAg0IEiIQiBEKGjJiw4MCBgwZ8QgSIQoGChIiQoMCAAYNCJ34gBEKw4MCBg4aMmDCEfDcDAEEIIwKtIgRC//8DgyAEQoCA/P8Pg0IQhoQiBEL/gYCA8B+DIARCgP6DgIDgP4NCCIaEIgRCj4C8gPCBwAeDQgiGIARC8IHAh4CegPgAg0IEiIQiBEKGjJiw4MCBgwZ8QgSIQoGChIiQoMCAAYNCJ34gBEKw4MCBg4aMmDCEfDcDAEEQIwOtIgRC//8DgyAEQoCA/P8Pg0IQhoQiBEL/gYCA8B+DIARCgP6DgIDgP4NCCIaEIgRCj4C8gPCBwAeDQgiGIARC8IHAh4CegPgAg0IEiIQiBEKGjJiw4MCBgwZ8QgSIQoGChIiQoMCAAYNCJ34gBEKw4MCBg4aMmDCEfDcDAEEYIwStIgRC//8DgyAEQoCA/P8Pg0IQhoQiBEL/gYCA8B+DIARCgP6DgIDgP4NCCIaEIgRCj4C8gPCBwAeDQgiGIARC8IHAh4CegPgAg0IEiIQiBEKGjJiw4MCBgwZ8QgSIQoGChIiQoMCAAYNCJ34gBEKw4MCBg4aMmDCEfDcDAAs=", "base64"));
	module.exports = create.bind(null, md4, [], 64, 32);
}));
//#endregion
//#region ../../node_modules/.pnpm/loader-utils@3.3.1/node_modules/loader-utils/lib/hash/BulkUpdateDecorator.js
var require_BulkUpdateDecorator = /* @__PURE__ */ __commonJSMin(((exports, module) => {
	const BULK_SIZE = 2e3;
	const digestCaches = {};
	var BulkUpdateDecorator = class {
		/**
		* @param {Hash | function(): Hash} hashOrFactory function to create a hash
		* @param {string=} hashKey key for caching
		*/
		constructor(hashOrFactory, hashKey) {
			this.hashKey = hashKey;
			if (typeof hashOrFactory === "function") {
				this.hashFactory = hashOrFactory;
				this.hash = void 0;
			} else {
				this.hashFactory = void 0;
				this.hash = hashOrFactory;
			}
			this.buffer = "";
		}
		/**
		* Update hash {@link https://nodejs.org/api/crypto.html#crypto_hash_update_data_inputencoding}
		* @param {string|Buffer} data data
		* @param {string=} inputEncoding data encoding
		* @returns {this} updated hash
		*/
		update(data, inputEncoding) {
			if (inputEncoding !== void 0 || typeof data !== "string" || data.length > BULK_SIZE) {
				if (this.hash === void 0) this.hash = this.hashFactory();
				if (this.buffer.length > 0) {
					this.hash.update(this.buffer);
					this.buffer = "";
				}
				this.hash.update(data, inputEncoding);
			} else {
				this.buffer += data;
				if (this.buffer.length > BULK_SIZE) {
					if (this.hash === void 0) this.hash = this.hashFactory();
					this.hash.update(this.buffer);
					this.buffer = "";
				}
			}
			return this;
		}
		/**
		* Calculates the digest {@link https://nodejs.org/api/crypto.html#crypto_hash_digest_encoding}
		* @param {string=} encoding encoding of the return value
		* @returns {string|Buffer} digest
		*/
		digest(encoding) {
			let digestCache;
			const buffer = this.buffer;
			if (this.hash === void 0) {
				const cacheKey = `${this.hashKey}-${encoding}`;
				digestCache = digestCaches[cacheKey];
				if (digestCache === void 0) digestCache = digestCaches[cacheKey] = /* @__PURE__ */ new Map();
				const cacheEntry = digestCache.get(buffer);
				if (cacheEntry !== void 0) return cacheEntry;
				this.hash = this.hashFactory();
			}
			if (buffer.length > 0) this.hash.update(buffer);
			const digestResult = this.hash.digest(encoding);
			if (digestCache !== void 0) digestCache.set(buffer, digestResult);
			return digestResult;
		}
	};
	module.exports = BulkUpdateDecorator;
}));
//#endregion
//#region ../../node_modules/.pnpm/loader-utils@3.3.1/node_modules/loader-utils/lib/getHashDigest.js
var require_getHashDigest = /* @__PURE__ */ __commonJSMin(((exports, module) => {
	const baseEncodeTables = {
		26: "abcdefghijklmnopqrstuvwxyz",
		32: "123456789abcdefghjkmnpqrstuvwxyz",
		36: "0123456789abcdefghijklmnopqrstuvwxyz",
		49: "abcdefghijkmnopqrstuvwxyzABCDEFGHJKLMNPQRSTUVWXYZ",
		52: "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ",
		58: "123456789abcdefghijkmnopqrstuvwxyzABCDEFGHJKLMNPQRSTUVWXYZ",
		62: "0123456789abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ",
		64: "0123456789abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ-_"
	};
	/**
	* @param {Uint32Array} uint32Array Treated as a long base-0x100000000 number, little endian
	* @param {number} divisor The divisor
	* @return {number} Modulo (remainder) of the division
	*/
	function divmod32(uint32Array, divisor) {
		let carry = 0;
		for (let i = uint32Array.length - 1; i >= 0; i--) {
			const value = carry * 4294967296 + uint32Array[i];
			carry = value % divisor;
			uint32Array[i] = Math.floor(value / divisor);
		}
		return carry;
	}
	function encodeBufferToBase(buffer, base, length) {
		const encodeTable = baseEncodeTables[base];
		if (!encodeTable) throw new Error("Unknown encoding base" + base);
		const limit = Math.ceil(buffer.length * 8 / Math.log2(base));
		length = Math.min(length, limit);
		const uint32Array = new Uint32Array(Math.ceil(buffer.length / 4));
		buffer.copy(Buffer.from(uint32Array.buffer));
		let output = "";
		for (let i = 0; i < length; i++) output = encodeTable[divmod32(uint32Array, base)] + output;
		return output;
	}
	let crypto = void 0;
	let createXXHash64 = void 0;
	let createMd4 = void 0;
	let BatchedHash = void 0;
	let BulkUpdateDecorator = void 0;
	function getHashDigest(buffer, algorithm, digestType, maxLength) {
		algorithm = algorithm || "xxhash64";
		maxLength = maxLength || 9999;
		let hash;
		if (algorithm === "xxhash64") {
			if (createXXHash64 === void 0) {
				createXXHash64 = require_xxhash64();
				if (BatchedHash === void 0) BatchedHash = require_BatchedHash();
			}
			hash = new BatchedHash(createXXHash64());
		} else if (algorithm === "md4") {
			if (createMd4 === void 0) {
				createMd4 = require_md4();
				if (BatchedHash === void 0) BatchedHash = require_BatchedHash();
			}
			hash = new BatchedHash(createMd4());
		} else if (algorithm === "native-md4") {
			if (typeof crypto === "undefined") {
				crypto = __require("crypto");
				if (BulkUpdateDecorator === void 0) BulkUpdateDecorator = require_BulkUpdateDecorator();
			}
			hash = new BulkUpdateDecorator(() => crypto.createHash("md4"), "md4");
		} else {
			if (typeof crypto === "undefined") {
				crypto = __require("crypto");
				if (BulkUpdateDecorator === void 0) BulkUpdateDecorator = require_BulkUpdateDecorator();
			}
			hash = new BulkUpdateDecorator(() => crypto.createHash(algorithm), algorithm);
		}
		hash.update(buffer);
		if (digestType === "base26" || digestType === "base32" || digestType === "base36" || digestType === "base49" || digestType === "base52" || digestType === "base58" || digestType === "base62" || digestType === "base64safe") return encodeBufferToBase(hash.digest(), digestType === "base64safe" ? 64 : digestType.substr(4), maxLength);
		return hash.digest(digestType || "hex").substr(0, maxLength);
	}
	module.exports = getHashDigest;
}));
//#endregion
//#region ../../node_modules/.pnpm/loader-utils@3.3.1/node_modules/loader-utils/lib/interpolateName.js
var require_interpolateName = /* @__PURE__ */ __commonJSMin(((exports, module) => {
	const path$1 = __require("path");
	const getHashDigest = require_getHashDigest();
	function interpolateName(loaderContext, name, options = {}) {
		let filename;
		const hasQuery = loaderContext.resourceQuery && loaderContext.resourceQuery.length > 1;
		if (typeof name === "function") filename = name(loaderContext.resourcePath, hasQuery ? loaderContext.resourceQuery : void 0);
		else filename = name || "[hash].[ext]";
		const context = options.context;
		const content = options.content;
		const regExp = options.regExp;
		let ext = "bin";
		let basename = "file";
		let directory = "";
		let folder = "";
		let query = "";
		if (loaderContext.resourcePath) {
			const parsed = path$1.parse(loaderContext.resourcePath);
			let resourcePath = loaderContext.resourcePath;
			if (parsed.ext) ext = parsed.ext.substr(1);
			if (parsed.dir) {
				basename = parsed.name;
				resourcePath = parsed.dir + path$1.sep;
			}
			if (typeof context !== "undefined") {
				directory = path$1.relative(context, resourcePath + "_").replace(/\\/g, "/").replace(/\.\.(\/)?/g, "_$1");
				directory = directory.substr(0, directory.length - 1);
			} else directory = resourcePath.replace(/\\/g, "/").replace(/\.\.(\/)?/g, "_$1");
			if (directory.length <= 1) directory = "";
			else folder = path$1.basename(directory);
		}
		if (loaderContext.resourceQuery && loaderContext.resourceQuery.length > 1) {
			query = loaderContext.resourceQuery;
			const hashIdx = query.indexOf("#");
			if (hashIdx >= 0) query = query.substr(0, hashIdx);
		}
		let url = filename;
		if (content) url = url.replace(/\[(?:([^[:\]]+):)?(?:hash|contenthash)(?::([a-z]+\d*(?:safe)?))?(?::(\d+))?\]/gi, (all, hashType, digestType, maxLength) => getHashDigest(content, hashType, digestType, parseInt(maxLength, 10)));
		url = url.replace(/\[ext\]/gi, () => ext).replace(/\[name\]/gi, () => basename).replace(/\[path\]/gi, () => directory).replace(/\[folder\]/gi, () => folder).replace(/\[query\]/gi, () => query);
		if (regExp && loaderContext.resourcePath) {
			const match = loaderContext.resourcePath.match(new RegExp(regExp));
			match && match.forEach((matched, i) => {
				url = url.replace(new RegExp("\\[" + i + "\\]", "ig"), matched);
			});
		}
		if (typeof loaderContext.options === "object" && typeof loaderContext.options.customInterpolateName === "function") url = loaderContext.options.customInterpolateName.call(loaderContext, url, name, options);
		return url;
	}
	module.exports = interpolateName;
}));
//#endregion
//#region ../../node_modules/.pnpm/generic-names@4.0.0/node_modules/generic-names/index.js
var require_generic_names = /* @__PURE__ */ __commonJSMin(((exports, module) => {
	var interpolateName = require_interpolateName();
	var path = __require("path");
	/**
	* @param  {string} pattern
	* @param  {object} options
	* @param  {string} options.context
	* @param  {string} options.hashPrefix
	* @return {function}
	*/
	module.exports = function createGenerator(pattern, options) {
		options = options || {};
		var context = options && typeof options.context === "string" ? options.context : process.cwd();
		var hashPrefix = options && typeof options.hashPrefix === "string" ? options.hashPrefix : "";
		/**
		* @param  {string} localName Usually a class name
		* @param  {string} filepath  Absolute path
		* @return {string}
		*/
		return function generate(localName, filepath) {
			var name = pattern.replace(/\[local\]/gi, localName);
			return interpolateName({ resourcePath: filepath }, name, {
				content: hashPrefix + path.relative(context, filepath).replace(/\\/g, "/") + "\0" + localName,
				context
			}).replace(/* @__PURE__ */ new RegExp("[^a-zA-Z0-9\\-_\xA0-￿]", "g"), "-").replace(/^((-?[0-9])|--)/, "_$1");
		};
	};
}));
//#endregion
//#region ../../node_modules/.pnpm/postcss-selector-parser@7.1.1/node_modules/postcss-selector-parser/dist/util/unesc.js
var require_unesc = /* @__PURE__ */ __commonJSMin(((exports, module) => {
	exports.__esModule = true;
	exports["default"] = unesc;
	/**
	* 
	* @param {string} str 
	* @returns {[string, number]|undefined}
	*/
	function gobbleHex(str) {
		var lower = str.toLowerCase();
		var hex = "";
		var spaceTerminated = false;
		for (var i = 0; i < 6 && lower[i] !== void 0; i++) {
			var code = lower.charCodeAt(i);
			var valid = code >= 97 && code <= 102 || code >= 48 && code <= 57;
			spaceTerminated = code === 32;
			if (!valid) break;
			hex += lower[i];
		}
		if (hex.length === 0) return;
		var codePoint = parseInt(hex, 16);
		if (codePoint >= 55296 && codePoint <= 57343 || codePoint === 0 || codePoint > 1114111) return ["�", hex.length + (spaceTerminated ? 1 : 0)];
		return [String.fromCodePoint(codePoint), hex.length + (spaceTerminated ? 1 : 0)];
	}
	var CONTAINS_ESCAPE = /\\/;
	function unesc(str) {
		if (!CONTAINS_ESCAPE.test(str)) return str;
		var ret = "";
		for (var i = 0; i < str.length; i++) {
			if (str[i] === "\\") {
				var gobbled = gobbleHex(str.slice(i + 1, i + 7));
				if (gobbled !== void 0) {
					ret += gobbled[0];
					i += gobbled[1];
					continue;
				}
				if (str[i + 1] === "\\") {
					ret += "\\";
					i++;
					continue;
				}
				if (str.length === i + 1) ret += str[i];
				continue;
			}
			ret += str[i];
		}
		return ret;
	}
	module.exports = exports.default;
}));
//#endregion
//#region ../../node_modules/.pnpm/postcss-selector-parser@7.1.1/node_modules/postcss-selector-parser/dist/util/getProp.js
var require_getProp = /* @__PURE__ */ __commonJSMin(((exports, module) => {
	exports.__esModule = true;
	exports["default"] = getProp;
	function getProp(obj) {
		for (var _len = arguments.length, props = new Array(_len > 1 ? _len - 1 : 0), _key = 1; _key < _len; _key++) props[_key - 1] = arguments[_key];
		while (props.length > 0) {
			var prop = props.shift();
			if (!obj[prop]) return;
			obj = obj[prop];
		}
		return obj;
	}
	module.exports = exports.default;
}));
//#endregion
//#region ../../node_modules/.pnpm/postcss-selector-parser@7.1.1/node_modules/postcss-selector-parser/dist/util/ensureObject.js
var require_ensureObject = /* @__PURE__ */ __commonJSMin(((exports, module) => {
	exports.__esModule = true;
	exports["default"] = ensureObject;
	function ensureObject(obj) {
		for (var _len = arguments.length, props = new Array(_len > 1 ? _len - 1 : 0), _key = 1; _key < _len; _key++) props[_key - 1] = arguments[_key];
		while (props.length > 0) {
			var prop = props.shift();
			if (!obj[prop]) obj[prop] = {};
			obj = obj[prop];
		}
	}
	module.exports = exports.default;
}));
//#endregion
//#region ../../node_modules/.pnpm/postcss-selector-parser@7.1.1/node_modules/postcss-selector-parser/dist/util/stripComments.js
var require_stripComments = /* @__PURE__ */ __commonJSMin(((exports, module) => {
	exports.__esModule = true;
	exports["default"] = stripComments;
	function stripComments(str) {
		var s = "";
		var commentStart = str.indexOf("/*");
		var lastEnd = 0;
		while (commentStart >= 0) {
			s = s + str.slice(lastEnd, commentStart);
			var commentEnd = str.indexOf("*/", commentStart + 2);
			if (commentEnd < 0) return s;
			lastEnd = commentEnd + 2;
			commentStart = str.indexOf("/*", lastEnd);
		}
		s = s + str.slice(lastEnd);
		return s;
	}
	module.exports = exports.default;
}));
//#endregion
//#region ../../node_modules/.pnpm/postcss-selector-parser@7.1.1/node_modules/postcss-selector-parser/dist/util/index.js
var require_util = /* @__PURE__ */ __commonJSMin(((exports) => {
	exports.__esModule = true;
	exports.unesc = exports.stripComments = exports.getProp = exports.ensureObject = void 0;
	exports.unesc = _interopRequireDefault(require_unesc())["default"];
	exports.getProp = _interopRequireDefault(require_getProp())["default"];
	exports.ensureObject = _interopRequireDefault(require_ensureObject())["default"];
	exports.stripComments = _interopRequireDefault(require_stripComments())["default"];
	function _interopRequireDefault(obj) {
		return obj && obj.__esModule ? obj : { "default": obj };
	}
}));
//#endregion
//#region ../../node_modules/.pnpm/postcss-selector-parser@7.1.1/node_modules/postcss-selector-parser/dist/selectors/node.js
var require_node$1 = /* @__PURE__ */ __commonJSMin(((exports, module) => {
	exports.__esModule = true;
	exports["default"] = void 0;
	var _util = require_util();
	function _defineProperties(target, props) {
		for (var i = 0; i < props.length; i++) {
			var descriptor = props[i];
			descriptor.enumerable = descriptor.enumerable || false;
			descriptor.configurable = true;
			if ("value" in descriptor) descriptor.writable = true;
			Object.defineProperty(target, descriptor.key, descriptor);
		}
	}
	function _createClass(Constructor, protoProps, staticProps) {
		if (protoProps) _defineProperties(Constructor.prototype, protoProps);
		if (staticProps) _defineProperties(Constructor, staticProps);
		Object.defineProperty(Constructor, "prototype", { writable: false });
		return Constructor;
	}
	var cloneNode = function cloneNode(obj, parent) {
		if (typeof obj !== "object" || obj === null) return obj;
		var cloned = new obj.constructor();
		for (var i in obj) {
			if (!obj.hasOwnProperty(i)) continue;
			var value = obj[i];
			if (i === "parent" && typeof value === "object") {
				if (parent) cloned[i] = parent;
			} else if (value instanceof Array) cloned[i] = value.map(function(j) {
				return cloneNode(j, cloned);
			});
			else cloned[i] = cloneNode(value, cloned);
		}
		return cloned;
	};
	exports["default"] = /* @__PURE__ */ function() {
		function Node(opts) {
			if (opts === void 0) opts = {};
			Object.assign(this, opts);
			this.spaces = this.spaces || {};
			this.spaces.before = this.spaces.before || "";
			this.spaces.after = this.spaces.after || "";
		}
		var _proto = Node.prototype;
		_proto.remove = function remove() {
			if (this.parent) this.parent.removeChild(this);
			this.parent = void 0;
			return this;
		};
		_proto.replaceWith = function replaceWith() {
			if (this.parent) {
				for (var index in arguments) this.parent.insertBefore(this, arguments[index]);
				this.remove();
			}
			return this;
		};
		_proto.next = function next() {
			return this.parent.at(this.parent.index(this) + 1);
		};
		_proto.prev = function prev() {
			return this.parent.at(this.parent.index(this) - 1);
		};
		_proto.clone = function clone(overrides) {
			if (overrides === void 0) overrides = {};
			var cloned = cloneNode(this);
			for (var name in overrides) cloned[name] = overrides[name];
			return cloned;
		};
		_proto.appendToPropertyAndEscape = function appendToPropertyAndEscape(name, value, valueEscaped) {
			if (!this.raws) this.raws = {};
			var originalValue = this[name];
			var originalEscaped = this.raws[name];
			this[name] = originalValue + value;
			if (originalEscaped || valueEscaped !== value) this.raws[name] = (originalEscaped || originalValue) + valueEscaped;
			else delete this.raws[name];
		};
		_proto.setPropertyAndEscape = function setPropertyAndEscape(name, value, valueEscaped) {
			if (!this.raws) this.raws = {};
			this[name] = value;
			this.raws[name] = valueEscaped;
		};
		_proto.setPropertyWithoutEscape = function setPropertyWithoutEscape(name, value) {
			this[name] = value;
			if (this.raws) delete this.raws[name];
		};
		_proto.isAtPosition = function isAtPosition(line, column) {
			if (this.source && this.source.start && this.source.end) {
				if (this.source.start.line > line) return false;
				if (this.source.end.line < line) return false;
				if (this.source.start.line === line && this.source.start.column > column) return false;
				if (this.source.end.line === line && this.source.end.column < column) return false;
				return true;
			}
		};
		_proto.stringifyProperty = function stringifyProperty(name) {
			return this.raws && this.raws[name] || this[name];
		};
		_proto.valueToString = function valueToString() {
			return String(this.stringifyProperty("value"));
		};
		_proto.toString = function toString() {
			return [
				this.rawSpaceBefore,
				this.valueToString(),
				this.rawSpaceAfter
			].join("");
		};
		_createClass(Node, [{
			key: "rawSpaceBefore",
			get: function get() {
				var rawSpace = this.raws && this.raws.spaces && this.raws.spaces.before;
				if (rawSpace === void 0) rawSpace = this.spaces && this.spaces.before;
				return rawSpace || "";
			},
			set: function set(raw) {
				(0, _util.ensureObject)(this, "raws", "spaces");
				this.raws.spaces.before = raw;
			}
		}, {
			key: "rawSpaceAfter",
			get: function get() {
				var rawSpace = this.raws && this.raws.spaces && this.raws.spaces.after;
				if (rawSpace === void 0) rawSpace = this.spaces.after;
				return rawSpace || "";
			},
			set: function set(raw) {
				(0, _util.ensureObject)(this, "raws", "spaces");
				this.raws.spaces.after = raw;
			}
		}]);
		return Node;
	}();
	module.exports = exports.default;
}));
//#endregion
//#region ../../node_modules/.pnpm/postcss-selector-parser@7.1.1/node_modules/postcss-selector-parser/dist/selectors/types.js
var require_types = /* @__PURE__ */ __commonJSMin(((exports) => {
	exports.__esModule = true;
	exports.UNIVERSAL = exports.TAG = exports.STRING = exports.SELECTOR = exports.ROOT = exports.PSEUDO = exports.NESTING = exports.ID = exports.COMMENT = exports.COMBINATOR = exports.CLASS = exports.ATTRIBUTE = void 0;
	exports.TAG = "tag";
	exports.STRING = "string";
	exports.SELECTOR = "selector";
	exports.ROOT = "root";
	exports.PSEUDO = "pseudo";
	exports.NESTING = "nesting";
	exports.ID = "id";
	exports.COMMENT = "comment";
	exports.COMBINATOR = "combinator";
	exports.CLASS = "class";
	exports.ATTRIBUTE = "attribute";
	exports.UNIVERSAL = "universal";
}));
//#endregion
//#region ../../node_modules/.pnpm/postcss-selector-parser@7.1.1/node_modules/postcss-selector-parser/dist/selectors/container.js
var require_container = /* @__PURE__ */ __commonJSMin(((exports, module) => {
	exports.__esModule = true;
	exports["default"] = void 0;
	var _node = _interopRequireDefault(require_node$1());
	var types = _interopRequireWildcard(require_types());
	function _getRequireWildcardCache(nodeInterop) {
		if (typeof WeakMap !== "function") return null;
		var cacheBabelInterop = /* @__PURE__ */ new WeakMap();
		var cacheNodeInterop = /* @__PURE__ */ new WeakMap();
		return (_getRequireWildcardCache = function _getRequireWildcardCache(nodeInterop) {
			return nodeInterop ? cacheNodeInterop : cacheBabelInterop;
		})(nodeInterop);
	}
	function _interopRequireWildcard(obj, nodeInterop) {
		if (!nodeInterop && obj && obj.__esModule) return obj;
		if (obj === null || typeof obj !== "object" && typeof obj !== "function") return { "default": obj };
		var cache = _getRequireWildcardCache(nodeInterop);
		if (cache && cache.has(obj)) return cache.get(obj);
		var newObj = {};
		var hasPropertyDescriptor = Object.defineProperty && Object.getOwnPropertyDescriptor;
		for (var key in obj) if (key !== "default" && Object.prototype.hasOwnProperty.call(obj, key)) {
			var desc = hasPropertyDescriptor ? Object.getOwnPropertyDescriptor(obj, key) : null;
			if (desc && (desc.get || desc.set)) Object.defineProperty(newObj, key, desc);
			else newObj[key] = obj[key];
		}
		newObj["default"] = obj;
		if (cache) cache.set(obj, newObj);
		return newObj;
	}
	function _interopRequireDefault(obj) {
		return obj && obj.__esModule ? obj : { "default": obj };
	}
	function _createForOfIteratorHelperLoose(o, allowArrayLike) {
		var it = typeof Symbol !== "undefined" && o[Symbol.iterator] || o["@@iterator"];
		if (it) return (it = it.call(o)).next.bind(it);
		if (Array.isArray(o) || (it = _unsupportedIterableToArray(o)) || allowArrayLike && o && typeof o.length === "number") {
			if (it) o = it;
			var i = 0;
			return function() {
				if (i >= o.length) return { done: true };
				return {
					done: false,
					value: o[i++]
				};
			};
		}
		throw new TypeError("Invalid attempt to iterate non-iterable instance.\nIn order to be iterable, non-array objects must have a [Symbol.iterator]() method.");
	}
	function _unsupportedIterableToArray(o, minLen) {
		if (!o) return;
		if (typeof o === "string") return _arrayLikeToArray(o, minLen);
		var n = Object.prototype.toString.call(o).slice(8, -1);
		if (n === "Object" && o.constructor) n = o.constructor.name;
		if (n === "Map" || n === "Set") return Array.from(o);
		if (n === "Arguments" || /^(?:Ui|I)nt(?:8|16|32)(?:Clamped)?Array$/.test(n)) return _arrayLikeToArray(o, minLen);
	}
	function _arrayLikeToArray(arr, len) {
		if (len == null || len > arr.length) len = arr.length;
		for (var i = 0, arr2 = new Array(len); i < len; i++) arr2[i] = arr[i];
		return arr2;
	}
	function _defineProperties(target, props) {
		for (var i = 0; i < props.length; i++) {
			var descriptor = props[i];
			descriptor.enumerable = descriptor.enumerable || false;
			descriptor.configurable = true;
			if ("value" in descriptor) descriptor.writable = true;
			Object.defineProperty(target, descriptor.key, descriptor);
		}
	}
	function _createClass(Constructor, protoProps, staticProps) {
		if (protoProps) _defineProperties(Constructor.prototype, protoProps);
		if (staticProps) _defineProperties(Constructor, staticProps);
		Object.defineProperty(Constructor, "prototype", { writable: false });
		return Constructor;
	}
	function _inheritsLoose(subClass, superClass) {
		subClass.prototype = Object.create(superClass.prototype);
		subClass.prototype.constructor = subClass;
		_setPrototypeOf(subClass, superClass);
	}
	function _setPrototypeOf(o, p) {
		_setPrototypeOf = Object.setPrototypeOf ? Object.setPrototypeOf.bind() : function _setPrototypeOf(o, p) {
			o.__proto__ = p;
			return o;
		};
		return _setPrototypeOf(o, p);
	}
	exports["default"] = /* @__PURE__ */ function(_Node) {
		_inheritsLoose(Container, _Node);
		function Container(opts) {
			var _this = _Node.call(this, opts) || this;
			if (!_this.nodes) _this.nodes = [];
			return _this;
		}
		var _proto = Container.prototype;
		_proto.append = function append(selector) {
			selector.parent = this;
			this.nodes.push(selector);
			return this;
		};
		_proto.prepend = function prepend(selector) {
			selector.parent = this;
			this.nodes.unshift(selector);
			for (var id in this.indexes) this.indexes[id]++;
			return this;
		};
		_proto.at = function at(index) {
			return this.nodes[index];
		};
		_proto.index = function index(child) {
			if (typeof child === "number") return child;
			return this.nodes.indexOf(child);
		};
		_proto.removeChild = function removeChild(child) {
			child = this.index(child);
			this.at(child).parent = void 0;
			this.nodes.splice(child, 1);
			var index;
			for (var id in this.indexes) {
				index = this.indexes[id];
				if (index >= child) this.indexes[id] = index - 1;
			}
			return this;
		};
		_proto.removeAll = function removeAll() {
			for (var _iterator = _createForOfIteratorHelperLoose(this.nodes), _step; !(_step = _iterator()).done;) {
				var node = _step.value;
				node.parent = void 0;
			}
			this.nodes = [];
			return this;
		};
		_proto.empty = function empty() {
			return this.removeAll();
		};
		_proto.insertAfter = function insertAfter(oldNode, newNode) {
			var _this$nodes;
			newNode.parent = this;
			var oldIndex = this.index(oldNode);
			var resetNode = [];
			for (var i = 2; i < arguments.length; i++) resetNode.push(arguments[i]);
			(_this$nodes = this.nodes).splice.apply(_this$nodes, [
				oldIndex + 1,
				0,
				newNode
			].concat(resetNode));
			newNode.parent = this;
			var index;
			for (var id in this.indexes) {
				index = this.indexes[id];
				if (oldIndex < index) this.indexes[id] = index + arguments.length - 1;
			}
			return this;
		};
		_proto.insertBefore = function insertBefore(oldNode, newNode) {
			var _this$nodes2;
			newNode.parent = this;
			var oldIndex = this.index(oldNode);
			var resetNode = [];
			for (var i = 2; i < arguments.length; i++) resetNode.push(arguments[i]);
			(_this$nodes2 = this.nodes).splice.apply(_this$nodes2, [
				oldIndex,
				0,
				newNode
			].concat(resetNode));
			newNode.parent = this;
			var index;
			for (var id in this.indexes) {
				index = this.indexes[id];
				if (index >= oldIndex) this.indexes[id] = index + arguments.length - 1;
			}
			return this;
		};
		_proto._findChildAtPosition = function _findChildAtPosition(line, col) {
			var found = void 0;
			this.each(function(node) {
				if (node.atPosition) {
					var foundChild = node.atPosition(line, col);
					if (foundChild) {
						found = foundChild;
						return false;
					}
				} else if (node.isAtPosition(line, col)) {
					found = node;
					return false;
				}
			});
			return found;
		};
		_proto.atPosition = function atPosition(line, col) {
			if (this.isAtPosition(line, col)) return this._findChildAtPosition(line, col) || this;
			else return;
		};
		_proto._inferEndPosition = function _inferEndPosition() {
			if (this.last && this.last.source && this.last.source.end) {
				this.source = this.source || {};
				this.source.end = this.source.end || {};
				Object.assign(this.source.end, this.last.source.end);
			}
		};
		_proto.each = function each(callback) {
			if (!this.lastEach) this.lastEach = 0;
			if (!this.indexes) this.indexes = {};
			this.lastEach++;
			var id = this.lastEach;
			this.indexes[id] = 0;
			if (!this.length) return;
			var index, result;
			while (this.indexes[id] < this.length) {
				index = this.indexes[id];
				result = callback(this.at(index), index);
				if (result === false) break;
				this.indexes[id] += 1;
			}
			delete this.indexes[id];
			if (result === false) return false;
		};
		_proto.walk = function walk(callback) {
			return this.each(function(node, i) {
				var result = callback(node, i);
				if (result !== false && node.length) result = node.walk(callback);
				if (result === false) return false;
			});
		};
		_proto.walkAttributes = function walkAttributes(callback) {
			var _this2 = this;
			return this.walk(function(selector) {
				if (selector.type === types.ATTRIBUTE) return callback.call(_this2, selector);
			});
		};
		_proto.walkClasses = function walkClasses(callback) {
			var _this3 = this;
			return this.walk(function(selector) {
				if (selector.type === types.CLASS) return callback.call(_this3, selector);
			});
		};
		_proto.walkCombinators = function walkCombinators(callback) {
			var _this4 = this;
			return this.walk(function(selector) {
				if (selector.type === types.COMBINATOR) return callback.call(_this4, selector);
			});
		};
		_proto.walkComments = function walkComments(callback) {
			var _this5 = this;
			return this.walk(function(selector) {
				if (selector.type === types.COMMENT) return callback.call(_this5, selector);
			});
		};
		_proto.walkIds = function walkIds(callback) {
			var _this6 = this;
			return this.walk(function(selector) {
				if (selector.type === types.ID) return callback.call(_this6, selector);
			});
		};
		_proto.walkNesting = function walkNesting(callback) {
			var _this7 = this;
			return this.walk(function(selector) {
				if (selector.type === types.NESTING) return callback.call(_this7, selector);
			});
		};
		_proto.walkPseudos = function walkPseudos(callback) {
			var _this8 = this;
			return this.walk(function(selector) {
				if (selector.type === types.PSEUDO) return callback.call(_this8, selector);
			});
		};
		_proto.walkTags = function walkTags(callback) {
			var _this9 = this;
			return this.walk(function(selector) {
				if (selector.type === types.TAG) return callback.call(_this9, selector);
			});
		};
		_proto.walkUniversals = function walkUniversals(callback) {
			var _this10 = this;
			return this.walk(function(selector) {
				if (selector.type === types.UNIVERSAL) return callback.call(_this10, selector);
			});
		};
		_proto.split = function split(callback) {
			var _this11 = this;
			var current = [];
			return this.reduce(function(memo, node, index) {
				var split = callback.call(_this11, node);
				current.push(node);
				if (split) {
					memo.push(current);
					current = [];
				} else if (index === _this11.length - 1) memo.push(current);
				return memo;
			}, []);
		};
		_proto.map = function map(callback) {
			return this.nodes.map(callback);
		};
		_proto.reduce = function reduce(callback, memo) {
			return this.nodes.reduce(callback, memo);
		};
		_proto.every = function every(callback) {
			return this.nodes.every(callback);
		};
		_proto.some = function some(callback) {
			return this.nodes.some(callback);
		};
		_proto.filter = function filter(callback) {
			return this.nodes.filter(callback);
		};
		_proto.sort = function sort(callback) {
			return this.nodes.sort(callback);
		};
		_proto.toString = function toString() {
			return this.map(String).join("");
		};
		_createClass(Container, [
			{
				key: "first",
				get: function get() {
					return this.at(0);
				}
			},
			{
				key: "last",
				get: function get() {
					return this.at(this.length - 1);
				}
			},
			{
				key: "length",
				get: function get() {
					return this.nodes.length;
				}
			}
		]);
		return Container;
	}(_node["default"]);
	module.exports = exports.default;
}));
//#endregion
//#region ../../node_modules/.pnpm/postcss-selector-parser@7.1.1/node_modules/postcss-selector-parser/dist/selectors/root.js
var require_root = /* @__PURE__ */ __commonJSMin(((exports, module) => {
	exports.__esModule = true;
	exports["default"] = void 0;
	var _container = _interopRequireDefault(require_container());
	var _types = require_types();
	function _interopRequireDefault(obj) {
		return obj && obj.__esModule ? obj : { "default": obj };
	}
	function _defineProperties(target, props) {
		for (var i = 0; i < props.length; i++) {
			var descriptor = props[i];
			descriptor.enumerable = descriptor.enumerable || false;
			descriptor.configurable = true;
			if ("value" in descriptor) descriptor.writable = true;
			Object.defineProperty(target, descriptor.key, descriptor);
		}
	}
	function _createClass(Constructor, protoProps, staticProps) {
		if (protoProps) _defineProperties(Constructor.prototype, protoProps);
		if (staticProps) _defineProperties(Constructor, staticProps);
		Object.defineProperty(Constructor, "prototype", { writable: false });
		return Constructor;
	}
	function _inheritsLoose(subClass, superClass) {
		subClass.prototype = Object.create(superClass.prototype);
		subClass.prototype.constructor = subClass;
		_setPrototypeOf(subClass, superClass);
	}
	function _setPrototypeOf(o, p) {
		_setPrototypeOf = Object.setPrototypeOf ? Object.setPrototypeOf.bind() : function _setPrototypeOf(o, p) {
			o.__proto__ = p;
			return o;
		};
		return _setPrototypeOf(o, p);
	}
	exports["default"] = /* @__PURE__ */ function(_Container) {
		_inheritsLoose(Root, _Container);
		function Root(opts) {
			var _this = _Container.call(this, opts) || this;
			_this.type = _types.ROOT;
			return _this;
		}
		var _proto = Root.prototype;
		_proto.toString = function toString() {
			var str = this.reduce(function(memo, selector) {
				memo.push(String(selector));
				return memo;
			}, []).join(",");
			return this.trailingComma ? str + "," : str;
		};
		_proto.error = function error(message, options) {
			if (this._error) return this._error(message, options);
			else return new Error(message);
		};
		_createClass(Root, [{
			key: "errorGenerator",
			set: function set(handler) {
				this._error = handler;
			}
		}]);
		return Root;
	}(_container["default"]);
	module.exports = exports.default;
}));
//#endregion
//#region ../../node_modules/.pnpm/postcss-selector-parser@7.1.1/node_modules/postcss-selector-parser/dist/selectors/selector.js
var require_selector = /* @__PURE__ */ __commonJSMin(((exports, module) => {
	exports.__esModule = true;
	exports["default"] = void 0;
	var _container = _interopRequireDefault(require_container());
	var _types = require_types();
	function _interopRequireDefault(obj) {
		return obj && obj.__esModule ? obj : { "default": obj };
	}
	function _inheritsLoose(subClass, superClass) {
		subClass.prototype = Object.create(superClass.prototype);
		subClass.prototype.constructor = subClass;
		_setPrototypeOf(subClass, superClass);
	}
	function _setPrototypeOf(o, p) {
		_setPrototypeOf = Object.setPrototypeOf ? Object.setPrototypeOf.bind() : function _setPrototypeOf(o, p) {
			o.__proto__ = p;
			return o;
		};
		return _setPrototypeOf(o, p);
	}
	exports["default"] = /* @__PURE__ */ function(_Container) {
		_inheritsLoose(Selector, _Container);
		function Selector(opts) {
			var _this = _Container.call(this, opts) || this;
			_this.type = _types.SELECTOR;
			return _this;
		}
		return Selector;
	}(_container["default"]);
	module.exports = exports.default;
}));
//#endregion
//#region ../../node_modules/.pnpm/cssesc@3.0.0/node_modules/cssesc/cssesc.js
/*! https://mths.be/cssesc v3.0.0 by @mathias */
var require_cssesc = /* @__PURE__ */ __commonJSMin(((exports, module) => {
	var hasOwnProperty = {}.hasOwnProperty;
	var merge = function merge(options, defaults) {
		if (!options) return defaults;
		var result = {};
		for (var key in defaults) result[key] = hasOwnProperty.call(options, key) ? options[key] : defaults[key];
		return result;
	};
	var regexAnySingleEscape = /[ -,\.\/:-@\[-\^`\{-~]/;
	var regexSingleEscape = /[ -,\.\/:-@\[\]\^`\{-~]/;
	var regexExcessiveSpaces = /(^|\\+)?(\\[A-F0-9]{1,6})\x20(?![a-fA-F0-9\x20])/g;
	var cssesc = function cssesc(string, options) {
		options = merge(options, cssesc.options);
		if (options.quotes != "single" && options.quotes != "double") options.quotes = "single";
		var quote = options.quotes == "double" ? "\"" : "'";
		var isIdentifier = options.isIdentifier;
		var firstChar = string.charAt(0);
		var output = "";
		var counter = 0;
		var length = string.length;
		while (counter < length) {
			var character = string.charAt(counter++);
			var codePoint = character.charCodeAt();
			var value = void 0;
			if (codePoint < 32 || codePoint > 126) {
				if (codePoint >= 55296 && codePoint <= 56319 && counter < length) {
					var extra = string.charCodeAt(counter++);
					if ((extra & 64512) == 56320) codePoint = ((codePoint & 1023) << 10) + (extra & 1023) + 65536;
					else counter--;
				}
				value = "\\" + codePoint.toString(16).toUpperCase() + " ";
			} else if (options.escapeEverything) if (regexAnySingleEscape.test(character)) value = "\\" + character;
			else value = "\\" + codePoint.toString(16).toUpperCase() + " ";
			else if (/[\t\n\f\r\x0B]/.test(character)) value = "\\" + codePoint.toString(16).toUpperCase() + " ";
			else if (character == "\\" || !isIdentifier && (character == "\"" && quote == character || character == "'" && quote == character) || isIdentifier && regexSingleEscape.test(character)) value = "\\" + character;
			else value = character;
			output += value;
		}
		if (isIdentifier) {
			if (/^-[-\d]/.test(output)) output = "\\-" + output.slice(1);
			else if (/\d/.test(firstChar)) output = "\\3" + firstChar + " " + output.slice(1);
		}
		output = output.replace(regexExcessiveSpaces, function($0, $1, $2) {
			if ($1 && $1.length % 2) return $0;
			return ($1 || "") + $2;
		});
		if (!isIdentifier && options.wrap) return quote + output + quote;
		return output;
	};
	cssesc.options = {
		"escapeEverything": false,
		"isIdentifier": false,
		"quotes": "single",
		"wrap": false
	};
	cssesc.version = "3.0.0";
	module.exports = cssesc;
}));
//#endregion
//#region ../../node_modules/.pnpm/postcss-selector-parser@7.1.1/node_modules/postcss-selector-parser/dist/selectors/className.js
var require_className = /* @__PURE__ */ __commonJSMin(((exports, module) => {
	exports.__esModule = true;
	exports["default"] = void 0;
	var _cssesc = _interopRequireDefault(require_cssesc());
	var _util = require_util();
	var _node = _interopRequireDefault(require_node$1());
	var _types = require_types();
	function _interopRequireDefault(obj) {
		return obj && obj.__esModule ? obj : { "default": obj };
	}
	function _defineProperties(target, props) {
		for (var i = 0; i < props.length; i++) {
			var descriptor = props[i];
			descriptor.enumerable = descriptor.enumerable || false;
			descriptor.configurable = true;
			if ("value" in descriptor) descriptor.writable = true;
			Object.defineProperty(target, descriptor.key, descriptor);
		}
	}
	function _createClass(Constructor, protoProps, staticProps) {
		if (protoProps) _defineProperties(Constructor.prototype, protoProps);
		if (staticProps) _defineProperties(Constructor, staticProps);
		Object.defineProperty(Constructor, "prototype", { writable: false });
		return Constructor;
	}
	function _inheritsLoose(subClass, superClass) {
		subClass.prototype = Object.create(superClass.prototype);
		subClass.prototype.constructor = subClass;
		_setPrototypeOf(subClass, superClass);
	}
	function _setPrototypeOf(o, p) {
		_setPrototypeOf = Object.setPrototypeOf ? Object.setPrototypeOf.bind() : function _setPrototypeOf(o, p) {
			o.__proto__ = p;
			return o;
		};
		return _setPrototypeOf(o, p);
	}
	exports["default"] = /* @__PURE__ */ function(_Node) {
		_inheritsLoose(ClassName, _Node);
		function ClassName(opts) {
			var _this = _Node.call(this, opts) || this;
			_this.type = _types.CLASS;
			_this._constructed = true;
			return _this;
		}
		var _proto = ClassName.prototype;
		_proto.valueToString = function valueToString() {
			return "." + _Node.prototype.valueToString.call(this);
		};
		_createClass(ClassName, [{
			key: "value",
			get: function get() {
				return this._value;
			},
			set: function set(v) {
				if (this._constructed) {
					var escaped = (0, _cssesc["default"])(v, { isIdentifier: true });
					if (escaped !== v) {
						(0, _util.ensureObject)(this, "raws");
						this.raws.value = escaped;
					} else if (this.raws) delete this.raws.value;
				}
				this._value = v;
			}
		}]);
		return ClassName;
	}(_node["default"]);
	module.exports = exports.default;
}));
//#endregion
//#region ../../node_modules/.pnpm/postcss-selector-parser@7.1.1/node_modules/postcss-selector-parser/dist/selectors/comment.js
var require_comment = /* @__PURE__ */ __commonJSMin(((exports, module) => {
	exports.__esModule = true;
	exports["default"] = void 0;
	var _node = _interopRequireDefault(require_node$1());
	var _types = require_types();
	function _interopRequireDefault(obj) {
		return obj && obj.__esModule ? obj : { "default": obj };
	}
	function _inheritsLoose(subClass, superClass) {
		subClass.prototype = Object.create(superClass.prototype);
		subClass.prototype.constructor = subClass;
		_setPrototypeOf(subClass, superClass);
	}
	function _setPrototypeOf(o, p) {
		_setPrototypeOf = Object.setPrototypeOf ? Object.setPrototypeOf.bind() : function _setPrototypeOf(o, p) {
			o.__proto__ = p;
			return o;
		};
		return _setPrototypeOf(o, p);
	}
	exports["default"] = /* @__PURE__ */ function(_Node) {
		_inheritsLoose(Comment, _Node);
		function Comment(opts) {
			var _this = _Node.call(this, opts) || this;
			_this.type = _types.COMMENT;
			return _this;
		}
		return Comment;
	}(_node["default"]);
	module.exports = exports.default;
}));
//#endregion
//#region ../../node_modules/.pnpm/postcss-selector-parser@7.1.1/node_modules/postcss-selector-parser/dist/selectors/id.js
var require_id = /* @__PURE__ */ __commonJSMin(((exports, module) => {
	exports.__esModule = true;
	exports["default"] = void 0;
	var _node = _interopRequireDefault(require_node$1());
	var _types = require_types();
	function _interopRequireDefault(obj) {
		return obj && obj.__esModule ? obj : { "default": obj };
	}
	function _inheritsLoose(subClass, superClass) {
		subClass.prototype = Object.create(superClass.prototype);
		subClass.prototype.constructor = subClass;
		_setPrototypeOf(subClass, superClass);
	}
	function _setPrototypeOf(o, p) {
		_setPrototypeOf = Object.setPrototypeOf ? Object.setPrototypeOf.bind() : function _setPrototypeOf(o, p) {
			o.__proto__ = p;
			return o;
		};
		return _setPrototypeOf(o, p);
	}
	exports["default"] = /* @__PURE__ */ function(_Node) {
		_inheritsLoose(ID, _Node);
		function ID(opts) {
			var _this = _Node.call(this, opts) || this;
			_this.type = _types.ID;
			return _this;
		}
		var _proto = ID.prototype;
		_proto.valueToString = function valueToString() {
			return "#" + _Node.prototype.valueToString.call(this);
		};
		return ID;
	}(_node["default"]);
	module.exports = exports.default;
}));
//#endregion
//#region ../../node_modules/.pnpm/postcss-selector-parser@7.1.1/node_modules/postcss-selector-parser/dist/selectors/namespace.js
var require_namespace = /* @__PURE__ */ __commonJSMin(((exports, module) => {
	exports.__esModule = true;
	exports["default"] = void 0;
	var _cssesc = _interopRequireDefault(require_cssesc());
	var _util = require_util();
	var _node = _interopRequireDefault(require_node$1());
	function _interopRequireDefault(obj) {
		return obj && obj.__esModule ? obj : { "default": obj };
	}
	function _defineProperties(target, props) {
		for (var i = 0; i < props.length; i++) {
			var descriptor = props[i];
			descriptor.enumerable = descriptor.enumerable || false;
			descriptor.configurable = true;
			if ("value" in descriptor) descriptor.writable = true;
			Object.defineProperty(target, descriptor.key, descriptor);
		}
	}
	function _createClass(Constructor, protoProps, staticProps) {
		if (protoProps) _defineProperties(Constructor.prototype, protoProps);
		if (staticProps) _defineProperties(Constructor, staticProps);
		Object.defineProperty(Constructor, "prototype", { writable: false });
		return Constructor;
	}
	function _inheritsLoose(subClass, superClass) {
		subClass.prototype = Object.create(superClass.prototype);
		subClass.prototype.constructor = subClass;
		_setPrototypeOf(subClass, superClass);
	}
	function _setPrototypeOf(o, p) {
		_setPrototypeOf = Object.setPrototypeOf ? Object.setPrototypeOf.bind() : function _setPrototypeOf(o, p) {
			o.__proto__ = p;
			return o;
		};
		return _setPrototypeOf(o, p);
	}
	exports["default"] = /* @__PURE__ */ function(_Node) {
		_inheritsLoose(Namespace, _Node);
		function Namespace() {
			return _Node.apply(this, arguments) || this;
		}
		var _proto = Namespace.prototype;
		_proto.qualifiedName = function qualifiedName(value) {
			if (this.namespace) return this.namespaceString + "|" + value;
			else return value;
		};
		_proto.valueToString = function valueToString() {
			return this.qualifiedName(_Node.prototype.valueToString.call(this));
		};
		_createClass(Namespace, [
			{
				key: "namespace",
				get: function get() {
					return this._namespace;
				},
				set: function set(namespace) {
					if (namespace === true || namespace === "*" || namespace === "&") {
						this._namespace = namespace;
						if (this.raws) delete this.raws.namespace;
						return;
					}
					var escaped = (0, _cssesc["default"])(namespace, { isIdentifier: true });
					this._namespace = namespace;
					if (escaped !== namespace) {
						(0, _util.ensureObject)(this, "raws");
						this.raws.namespace = escaped;
					} else if (this.raws) delete this.raws.namespace;
				}
			},
			{
				key: "ns",
				get: function get() {
					return this._namespace;
				},
				set: function set(namespace) {
					this.namespace = namespace;
				}
			},
			{
				key: "namespaceString",
				get: function get() {
					if (this.namespace) {
						var ns = this.stringifyProperty("namespace");
						if (ns === true) return "";
						else return ns;
					} else return "";
				}
			}
		]);
		return Namespace;
	}(_node["default"]);
	module.exports = exports.default;
}));
//#endregion
//#region ../../node_modules/.pnpm/postcss-selector-parser@7.1.1/node_modules/postcss-selector-parser/dist/selectors/tag.js
var require_tag = /* @__PURE__ */ __commonJSMin(((exports, module) => {
	exports.__esModule = true;
	exports["default"] = void 0;
	var _namespace = _interopRequireDefault(require_namespace());
	var _types = require_types();
	function _interopRequireDefault(obj) {
		return obj && obj.__esModule ? obj : { "default": obj };
	}
	function _inheritsLoose(subClass, superClass) {
		subClass.prototype = Object.create(superClass.prototype);
		subClass.prototype.constructor = subClass;
		_setPrototypeOf(subClass, superClass);
	}
	function _setPrototypeOf(o, p) {
		_setPrototypeOf = Object.setPrototypeOf ? Object.setPrototypeOf.bind() : function _setPrototypeOf(o, p) {
			o.__proto__ = p;
			return o;
		};
		return _setPrototypeOf(o, p);
	}
	exports["default"] = /* @__PURE__ */ function(_Namespace) {
		_inheritsLoose(Tag, _Namespace);
		function Tag(opts) {
			var _this = _Namespace.call(this, opts) || this;
			_this.type = _types.TAG;
			return _this;
		}
		return Tag;
	}(_namespace["default"]);
	module.exports = exports.default;
}));
//#endregion
//#region ../../node_modules/.pnpm/postcss-selector-parser@7.1.1/node_modules/postcss-selector-parser/dist/selectors/string.js
var require_string = /* @__PURE__ */ __commonJSMin(((exports, module) => {
	exports.__esModule = true;
	exports["default"] = void 0;
	var _node = _interopRequireDefault(require_node$1());
	var _types = require_types();
	function _interopRequireDefault(obj) {
		return obj && obj.__esModule ? obj : { "default": obj };
	}
	function _inheritsLoose(subClass, superClass) {
		subClass.prototype = Object.create(superClass.prototype);
		subClass.prototype.constructor = subClass;
		_setPrototypeOf(subClass, superClass);
	}
	function _setPrototypeOf(o, p) {
		_setPrototypeOf = Object.setPrototypeOf ? Object.setPrototypeOf.bind() : function _setPrototypeOf(o, p) {
			o.__proto__ = p;
			return o;
		};
		return _setPrototypeOf(o, p);
	}
	exports["default"] = /* @__PURE__ */ function(_Node) {
		_inheritsLoose(String, _Node);
		function String(opts) {
			var _this = _Node.call(this, opts) || this;
			_this.type = _types.STRING;
			return _this;
		}
		return String;
	}(_node["default"]);
	module.exports = exports.default;
}));
//#endregion
//#region ../../node_modules/.pnpm/postcss-selector-parser@7.1.1/node_modules/postcss-selector-parser/dist/selectors/pseudo.js
var require_pseudo = /* @__PURE__ */ __commonJSMin(((exports, module) => {
	exports.__esModule = true;
	exports["default"] = void 0;
	var _container = _interopRequireDefault(require_container());
	var _types = require_types();
	function _interopRequireDefault(obj) {
		return obj && obj.__esModule ? obj : { "default": obj };
	}
	function _inheritsLoose(subClass, superClass) {
		subClass.prototype = Object.create(superClass.prototype);
		subClass.prototype.constructor = subClass;
		_setPrototypeOf(subClass, superClass);
	}
	function _setPrototypeOf(o, p) {
		_setPrototypeOf = Object.setPrototypeOf ? Object.setPrototypeOf.bind() : function _setPrototypeOf(o, p) {
			o.__proto__ = p;
			return o;
		};
		return _setPrototypeOf(o, p);
	}
	exports["default"] = /* @__PURE__ */ function(_Container) {
		_inheritsLoose(Pseudo, _Container);
		function Pseudo(opts) {
			var _this = _Container.call(this, opts) || this;
			_this.type = _types.PSEUDO;
			return _this;
		}
		var _proto = Pseudo.prototype;
		_proto.toString = function toString() {
			var params = this.length ? "(" + this.map(String).join(",") + ")" : "";
			return [
				this.rawSpaceBefore,
				this.stringifyProperty("value"),
				params,
				this.rawSpaceAfter
			].join("");
		};
		return Pseudo;
	}(_container["default"]);
	module.exports = exports.default;
}));
//#endregion
//#region ../../node_modules/.pnpm/util-deprecate@1.0.2/node_modules/util-deprecate/node.js
var require_node = /* @__PURE__ */ __commonJSMin(((exports, module) => {
	/**
	* For Node.js, simply re-export the core `util.deprecate` function.
	*/
	module.exports = __require("util").deprecate;
}));
//#endregion
//#region ../../node_modules/.pnpm/postcss-selector-parser@7.1.1/node_modules/postcss-selector-parser/dist/selectors/attribute.js
var require_attribute = /* @__PURE__ */ __commonJSMin(((exports) => {
	exports.__esModule = true;
	exports["default"] = void 0;
	exports.unescapeValue = unescapeValue;
	var _cssesc = _interopRequireDefault(require_cssesc());
	var _unesc = _interopRequireDefault(require_unesc());
	var _namespace = _interopRequireDefault(require_namespace());
	var _types = require_types();
	var _CSSESC_QUOTE_OPTIONS;
	function _interopRequireDefault(obj) {
		return obj && obj.__esModule ? obj : { "default": obj };
	}
	function _defineProperties(target, props) {
		for (var i = 0; i < props.length; i++) {
			var descriptor = props[i];
			descriptor.enumerable = descriptor.enumerable || false;
			descriptor.configurable = true;
			if ("value" in descriptor) descriptor.writable = true;
			Object.defineProperty(target, descriptor.key, descriptor);
		}
	}
	function _createClass(Constructor, protoProps, staticProps) {
		if (protoProps) _defineProperties(Constructor.prototype, protoProps);
		if (staticProps) _defineProperties(Constructor, staticProps);
		Object.defineProperty(Constructor, "prototype", { writable: false });
		return Constructor;
	}
	function _inheritsLoose(subClass, superClass) {
		subClass.prototype = Object.create(superClass.prototype);
		subClass.prototype.constructor = subClass;
		_setPrototypeOf(subClass, superClass);
	}
	function _setPrototypeOf(o, p) {
		_setPrototypeOf = Object.setPrototypeOf ? Object.setPrototypeOf.bind() : function _setPrototypeOf(o, p) {
			o.__proto__ = p;
			return o;
		};
		return _setPrototypeOf(o, p);
	}
	var deprecate = require_node();
	var WRAPPED_IN_QUOTES = /^('|")([^]*)\1$/;
	var warnOfDeprecatedValueAssignment = deprecate(function() {}, "Assigning an attribute a value containing characters that might need to be escaped is deprecated. Call attribute.setValue() instead.");
	var warnOfDeprecatedQuotedAssignment = deprecate(function() {}, "Assigning attr.quoted is deprecated and has no effect. Assign to attr.quoteMark instead.");
	var warnOfDeprecatedConstructor = deprecate(function() {}, "Constructing an Attribute selector with a value without specifying quoteMark is deprecated. Note: The value should be unescaped now.");
	function unescapeValue(value) {
		var deprecatedUsage = false;
		var quoteMark = null;
		var unescaped = value;
		var m = unescaped.match(WRAPPED_IN_QUOTES);
		if (m) {
			quoteMark = m[1];
			unescaped = m[2];
		}
		unescaped = (0, _unesc["default"])(unescaped);
		if (unescaped !== value) deprecatedUsage = true;
		return {
			deprecatedUsage,
			unescaped,
			quoteMark
		};
	}
	function handleDeprecatedContructorOpts(opts) {
		if (opts.quoteMark !== void 0) return opts;
		if (opts.value === void 0) return opts;
		warnOfDeprecatedConstructor();
		var _unescapeValue = unescapeValue(opts.value), quoteMark = _unescapeValue.quoteMark, unescaped = _unescapeValue.unescaped;
		if (!opts.raws) opts.raws = {};
		if (opts.raws.value === void 0) opts.raws.value = opts.value;
		opts.value = unescaped;
		opts.quoteMark = quoteMark;
		return opts;
	}
	var Attribute = /*#__PURE__*/ function(_Namespace) {
		_inheritsLoose(Attribute, _Namespace);
		function Attribute(opts) {
			var _this;
			if (opts === void 0) opts = {};
			_this = _Namespace.call(this, handleDeprecatedContructorOpts(opts)) || this;
			_this.type = _types.ATTRIBUTE;
			_this.raws = _this.raws || {};
			Object.defineProperty(_this.raws, "unquoted", {
				get: deprecate(function() {
					return _this.value;
				}, "attr.raws.unquoted is deprecated. Call attr.value instead."),
				set: deprecate(function() {
					return _this.value;
				}, "Setting attr.raws.unquoted is deprecated and has no effect. attr.value is unescaped by default now.")
			});
			_this._constructed = true;
			return _this;
		}
		/**
		* Returns the Attribute's value quoted such that it would be legal to use
		* in the value of a css file. The original value's quotation setting
		* used for stringification is left unchanged. See `setValue(value, options)`
		* if you want to control the quote settings of a new value for the attribute.
		*
		* You can also change the quotation used for the current value by setting quoteMark.
		*
		* Options:
		*   * quoteMark {'"' | "'" | null} - Use this value to quote the value. If this
		*     option is not set, the original value for quoteMark will be used. If
		*     indeterminate, a double quote is used. The legal values are:
		*     * `null` - the value will be unquoted and characters will be escaped as necessary.
		*     * `'` - the value will be quoted with a single quote and single quotes are escaped.
		*     * `"` - the value will be quoted with a double quote and double quotes are escaped.
		*   * preferCurrentQuoteMark {boolean} - if true, prefer the source quote mark
		*     over the quoteMark option value.
		*   * smart {boolean} - if true, will select a quote mark based on the value
		*     and the other options specified here. See the `smartQuoteMark()`
		*     method.
		**/
		var _proto = Attribute.prototype;
		_proto.getQuotedValue = function getQuotedValue(options) {
			if (options === void 0) options = {};
			var cssescopts = CSSESC_QUOTE_OPTIONS[this._determineQuoteMark(options)];
			return (0, _cssesc["default"])(this._value, cssescopts);
		};
		_proto._determineQuoteMark = function _determineQuoteMark(options) {
			return options.smart ? this.smartQuoteMark(options) : this.preferredQuoteMark(options);
		};
		_proto.setValue = function setValue(value, options) {
			if (options === void 0) options = {};
			this._value = value;
			this._quoteMark = this._determineQuoteMark(options);
			this._syncRawValue();
		};
		_proto.smartQuoteMark = function smartQuoteMark(options) {
			var v = this.value;
			var numSingleQuotes = v.replace(/[^']/g, "").length;
			var numDoubleQuotes = v.replace(/[^"]/g, "").length;
			if (numSingleQuotes + numDoubleQuotes === 0) {
				var escaped = (0, _cssesc["default"])(v, { isIdentifier: true });
				if (escaped === v) return Attribute.NO_QUOTE;
				else {
					var pref = this.preferredQuoteMark(options);
					if (pref === Attribute.NO_QUOTE) {
						var quote = this.quoteMark || options.quoteMark || Attribute.DOUBLE_QUOTE;
						if ((0, _cssesc["default"])(v, CSSESC_QUOTE_OPTIONS[quote]).length < escaped.length) return quote;
					}
					return pref;
				}
			} else if (numDoubleQuotes === numSingleQuotes) return this.preferredQuoteMark(options);
			else if (numDoubleQuotes < numSingleQuotes) return Attribute.DOUBLE_QUOTE;
			else return Attribute.SINGLE_QUOTE;
		};
		_proto.preferredQuoteMark = function preferredQuoteMark(options) {
			var quoteMark = options.preferCurrentQuoteMark ? this.quoteMark : options.quoteMark;
			if (quoteMark === void 0) quoteMark = options.preferCurrentQuoteMark ? options.quoteMark : this.quoteMark;
			if (quoteMark === void 0) quoteMark = Attribute.DOUBLE_QUOTE;
			return quoteMark;
		};
		_proto._syncRawValue = function _syncRawValue() {
			var rawValue = (0, _cssesc["default"])(this._value, CSSESC_QUOTE_OPTIONS[this.quoteMark]);
			if (rawValue === this._value) {
				if (this.raws) delete this.raws.value;
			} else this.raws.value = rawValue;
		};
		_proto._handleEscapes = function _handleEscapes(prop, value) {
			if (this._constructed) {
				var escaped = (0, _cssesc["default"])(value, { isIdentifier: true });
				if (escaped !== value) this.raws[prop] = escaped;
				else delete this.raws[prop];
			}
		};
		_proto._spacesFor = function _spacesFor(name) {
			var attrSpaces = {
				before: "",
				after: ""
			};
			var spaces = this.spaces[name] || {};
			var rawSpaces = this.raws.spaces && this.raws.spaces[name] || {};
			return Object.assign(attrSpaces, spaces, rawSpaces);
		};
		_proto._stringFor = function _stringFor(name, spaceName, concat) {
			if (spaceName === void 0) spaceName = name;
			if (concat === void 0) concat = defaultAttrConcat;
			var attrSpaces = this._spacesFor(spaceName);
			return concat(this.stringifyProperty(name), attrSpaces);
		};
		_proto.offsetOf = function offsetOf(name) {
			var count = 1;
			var attributeSpaces = this._spacesFor("attribute");
			count += attributeSpaces.before.length;
			if (name === "namespace" || name === "ns") return this.namespace ? count : -1;
			if (name === "attributeNS") return count;
			count += this.namespaceString.length;
			if (this.namespace) count += 1;
			if (name === "attribute") return count;
			count += this.stringifyProperty("attribute").length;
			count += attributeSpaces.after.length;
			var operatorSpaces = this._spacesFor("operator");
			count += operatorSpaces.before.length;
			var operator = this.stringifyProperty("operator");
			if (name === "operator") return operator ? count : -1;
			count += operator.length;
			count += operatorSpaces.after.length;
			var valueSpaces = this._spacesFor("value");
			count += valueSpaces.before.length;
			var value = this.stringifyProperty("value");
			if (name === "value") return value ? count : -1;
			count += value.length;
			count += valueSpaces.after.length;
			var insensitiveSpaces = this._spacesFor("insensitive");
			count += insensitiveSpaces.before.length;
			if (name === "insensitive") return this.insensitive ? count : -1;
			return -1;
		};
		_proto.toString = function toString() {
			var _this2 = this;
			var selector = [this.rawSpaceBefore, "["];
			selector.push(this._stringFor("qualifiedAttribute", "attribute"));
			if (this.operator && (this.value || this.value === "")) {
				selector.push(this._stringFor("operator"));
				selector.push(this._stringFor("value"));
				selector.push(this._stringFor("insensitiveFlag", "insensitive", function(attrValue, attrSpaces) {
					if (attrValue.length > 0 && !_this2.quoted && attrSpaces.before.length === 0 && !(_this2.spaces.value && _this2.spaces.value.after)) attrSpaces.before = " ";
					return defaultAttrConcat(attrValue, attrSpaces);
				}));
			}
			selector.push("]");
			selector.push(this.rawSpaceAfter);
			return selector.join("");
		};
		_createClass(Attribute, [
			{
				key: "quoted",
				get: function get() {
					var qm = this.quoteMark;
					return qm === "'" || qm === "\"";
				},
				set: function set(value) {
					warnOfDeprecatedQuotedAssignment();
				}
			},
			{
				key: "quoteMark",
				get: function get() {
					return this._quoteMark;
				},
				set: function set(quoteMark) {
					if (!this._constructed) {
						this._quoteMark = quoteMark;
						return;
					}
					if (this._quoteMark !== quoteMark) {
						this._quoteMark = quoteMark;
						this._syncRawValue();
					}
				}
			},
			{
				key: "qualifiedAttribute",
				get: function get() {
					return this.qualifiedName(this.raws.attribute || this.attribute);
				}
			},
			{
				key: "insensitiveFlag",
				get: function get() {
					return this.insensitive ? "i" : "";
				}
			},
			{
				key: "value",
				get: function get() {
					return this._value;
				},
				set: function set(v) {
					if (this._constructed) {
						var _unescapeValue2 = unescapeValue(v), deprecatedUsage = _unescapeValue2.deprecatedUsage, unescaped = _unescapeValue2.unescaped, quoteMark = _unescapeValue2.quoteMark;
						if (deprecatedUsage) warnOfDeprecatedValueAssignment();
						if (unescaped === this._value && quoteMark === this._quoteMark) return;
						this._value = unescaped;
						this._quoteMark = quoteMark;
						this._syncRawValue();
					} else this._value = v;
				}
			},
			{
				key: "insensitive",
				get: function get() {
					return this._insensitive;
				},
				set: function set(insensitive) {
					if (!insensitive) {
						this._insensitive = false;
						if (this.raws && (this.raws.insensitiveFlag === "I" || this.raws.insensitiveFlag === "i")) this.raws.insensitiveFlag = void 0;
					}
					this._insensitive = insensitive;
				}
			},
			{
				key: "attribute",
				get: function get() {
					return this._attribute;
				},
				set: function set(name) {
					this._handleEscapes("attribute", name);
					this._attribute = name;
				}
			}
		]);
		return Attribute;
	}(_namespace["default"]);
	exports["default"] = Attribute;
	Attribute.NO_QUOTE = null;
	Attribute.SINGLE_QUOTE = "'";
	Attribute.DOUBLE_QUOTE = "\"";
	var CSSESC_QUOTE_OPTIONS = (_CSSESC_QUOTE_OPTIONS = {
		"'": {
			quotes: "single",
			wrap: true
		},
		"\"": {
			quotes: "double",
			wrap: true
		}
	}, _CSSESC_QUOTE_OPTIONS[null] = { isIdentifier: true }, _CSSESC_QUOTE_OPTIONS);
	function defaultAttrConcat(attrValue, attrSpaces) {
		return "" + attrSpaces.before + attrValue + attrSpaces.after;
	}
}));
//#endregion
//#region ../../node_modules/.pnpm/postcss-selector-parser@7.1.1/node_modules/postcss-selector-parser/dist/selectors/universal.js
var require_universal = /* @__PURE__ */ __commonJSMin(((exports, module) => {
	exports.__esModule = true;
	exports["default"] = void 0;
	var _namespace = _interopRequireDefault(require_namespace());
	var _types = require_types();
	function _interopRequireDefault(obj) {
		return obj && obj.__esModule ? obj : { "default": obj };
	}
	function _inheritsLoose(subClass, superClass) {
		subClass.prototype = Object.create(superClass.prototype);
		subClass.prototype.constructor = subClass;
		_setPrototypeOf(subClass, superClass);
	}
	function _setPrototypeOf(o, p) {
		_setPrototypeOf = Object.setPrototypeOf ? Object.setPrototypeOf.bind() : function _setPrototypeOf(o, p) {
			o.__proto__ = p;
			return o;
		};
		return _setPrototypeOf(o, p);
	}
	exports["default"] = /* @__PURE__ */ function(_Namespace) {
		_inheritsLoose(Universal, _Namespace);
		function Universal(opts) {
			var _this = _Namespace.call(this, opts) || this;
			_this.type = _types.UNIVERSAL;
			_this.value = "*";
			return _this;
		}
		return Universal;
	}(_namespace["default"]);
	module.exports = exports.default;
}));
//#endregion
//#region ../../node_modules/.pnpm/postcss-selector-parser@7.1.1/node_modules/postcss-selector-parser/dist/selectors/combinator.js
var require_combinator = /* @__PURE__ */ __commonJSMin(((exports, module) => {
	exports.__esModule = true;
	exports["default"] = void 0;
	var _node = _interopRequireDefault(require_node$1());
	var _types = require_types();
	function _interopRequireDefault(obj) {
		return obj && obj.__esModule ? obj : { "default": obj };
	}
	function _inheritsLoose(subClass, superClass) {
		subClass.prototype = Object.create(superClass.prototype);
		subClass.prototype.constructor = subClass;
		_setPrototypeOf(subClass, superClass);
	}
	function _setPrototypeOf(o, p) {
		_setPrototypeOf = Object.setPrototypeOf ? Object.setPrototypeOf.bind() : function _setPrototypeOf(o, p) {
			o.__proto__ = p;
			return o;
		};
		return _setPrototypeOf(o, p);
	}
	exports["default"] = /* @__PURE__ */ function(_Node) {
		_inheritsLoose(Combinator, _Node);
		function Combinator(opts) {
			var _this = _Node.call(this, opts) || this;
			_this.type = _types.COMBINATOR;
			return _this;
		}
		return Combinator;
	}(_node["default"]);
	module.exports = exports.default;
}));
//#endregion
//#region ../../node_modules/.pnpm/postcss-selector-parser@7.1.1/node_modules/postcss-selector-parser/dist/selectors/nesting.js
var require_nesting = /* @__PURE__ */ __commonJSMin(((exports, module) => {
	exports.__esModule = true;
	exports["default"] = void 0;
	var _node = _interopRequireDefault(require_node$1());
	var _types = require_types();
	function _interopRequireDefault(obj) {
		return obj && obj.__esModule ? obj : { "default": obj };
	}
	function _inheritsLoose(subClass, superClass) {
		subClass.prototype = Object.create(superClass.prototype);
		subClass.prototype.constructor = subClass;
		_setPrototypeOf(subClass, superClass);
	}
	function _setPrototypeOf(o, p) {
		_setPrototypeOf = Object.setPrototypeOf ? Object.setPrototypeOf.bind() : function _setPrototypeOf(o, p) {
			o.__proto__ = p;
			return o;
		};
		return _setPrototypeOf(o, p);
	}
	exports["default"] = /* @__PURE__ */ function(_Node) {
		_inheritsLoose(Nesting, _Node);
		function Nesting(opts) {
			var _this = _Node.call(this, opts) || this;
			_this.type = _types.NESTING;
			_this.value = "&";
			return _this;
		}
		return Nesting;
	}(_node["default"]);
	module.exports = exports.default;
}));
//#endregion
//#region ../../node_modules/.pnpm/postcss-selector-parser@7.1.1/node_modules/postcss-selector-parser/dist/sortAscending.js
var require_sortAscending = /* @__PURE__ */ __commonJSMin(((exports, module) => {
	exports.__esModule = true;
	exports["default"] = sortAscending;
	function sortAscending(list) {
		return list.sort(function(a, b) {
			return a - b;
		});
	}
	module.exports = exports.default;
}));
//#endregion
//#region ../../node_modules/.pnpm/postcss-selector-parser@7.1.1/node_modules/postcss-selector-parser/dist/tokenTypes.js
var require_tokenTypes = /* @__PURE__ */ __commonJSMin(((exports) => {
	exports.__esModule = true;
	exports.word = exports.tilde = exports.tab = exports.str = exports.space = exports.slash = exports.singleQuote = exports.semicolon = exports.plus = exports.pipe = exports.openSquare = exports.openParenthesis = exports.newline = exports.greaterThan = exports.feed = exports.equals = exports.doubleQuote = exports.dollar = exports.cr = exports.comment = exports.comma = exports.combinator = exports.colon = exports.closeSquare = exports.closeParenthesis = exports.caret = exports.bang = exports.backslash = exports.at = exports.asterisk = exports.ampersand = void 0;
	exports.ampersand = 38;
	exports.asterisk = 42;
	exports.at = 64;
	exports.comma = 44;
	exports.colon = 58;
	exports.semicolon = 59;
	exports.openParenthesis = 40;
	exports.closeParenthesis = 41;
	exports.openSquare = 91;
	exports.closeSquare = 93;
	exports.dollar = 36;
	exports.tilde = 126;
	exports.caret = 94;
	exports.plus = 43;
	exports.equals = 61;
	exports.pipe = 124;
	exports.greaterThan = 62;
	exports.space = 32;
	var singleQuote = 39;
	exports.singleQuote = singleQuote;
	exports.doubleQuote = 34;
	exports.slash = 47;
	exports.bang = 33;
	exports.backslash = 92;
	exports.cr = 13;
	exports.feed = 12;
	exports.newline = 10;
	exports.tab = 9;
	exports.str = singleQuote;
	exports.comment = -1;
	exports.word = -2;
	exports.combinator = -3;
}));
//#endregion
//#region ../../node_modules/.pnpm/postcss-selector-parser@7.1.1/node_modules/postcss-selector-parser/dist/tokenize.js
var require_tokenize = /* @__PURE__ */ __commonJSMin(((exports) => {
	exports.__esModule = true;
	exports.FIELDS = void 0;
	exports["default"] = tokenize;
	var t = _interopRequireWildcard(require_tokenTypes());
	var _unescapable, _wordDelimiters;
	function _getRequireWildcardCache(nodeInterop) {
		if (typeof WeakMap !== "function") return null;
		var cacheBabelInterop = /* @__PURE__ */ new WeakMap();
		var cacheNodeInterop = /* @__PURE__ */ new WeakMap();
		return (_getRequireWildcardCache = function _getRequireWildcardCache(nodeInterop) {
			return nodeInterop ? cacheNodeInterop : cacheBabelInterop;
		})(nodeInterop);
	}
	function _interopRequireWildcard(obj, nodeInterop) {
		if (!nodeInterop && obj && obj.__esModule) return obj;
		if (obj === null || typeof obj !== "object" && typeof obj !== "function") return { "default": obj };
		var cache = _getRequireWildcardCache(nodeInterop);
		if (cache && cache.has(obj)) return cache.get(obj);
		var newObj = {};
		var hasPropertyDescriptor = Object.defineProperty && Object.getOwnPropertyDescriptor;
		for (var key in obj) if (key !== "default" && Object.prototype.hasOwnProperty.call(obj, key)) {
			var desc = hasPropertyDescriptor ? Object.getOwnPropertyDescriptor(obj, key) : null;
			if (desc && (desc.get || desc.set)) Object.defineProperty(newObj, key, desc);
			else newObj[key] = obj[key];
		}
		newObj["default"] = obj;
		if (cache) cache.set(obj, newObj);
		return newObj;
	}
	var unescapable = (_unescapable = {}, _unescapable[t.tab] = true, _unescapable[t.newline] = true, _unescapable[t.cr] = true, _unescapable[t.feed] = true, _unescapable);
	var wordDelimiters = (_wordDelimiters = {}, _wordDelimiters[t.space] = true, _wordDelimiters[t.tab] = true, _wordDelimiters[t.newline] = true, _wordDelimiters[t.cr] = true, _wordDelimiters[t.feed] = true, _wordDelimiters[t.ampersand] = true, _wordDelimiters[t.asterisk] = true, _wordDelimiters[t.bang] = true, _wordDelimiters[t.comma] = true, _wordDelimiters[t.colon] = true, _wordDelimiters[t.semicolon] = true, _wordDelimiters[t.openParenthesis] = true, _wordDelimiters[t.closeParenthesis] = true, _wordDelimiters[t.openSquare] = true, _wordDelimiters[t.closeSquare] = true, _wordDelimiters[t.singleQuote] = true, _wordDelimiters[t.doubleQuote] = true, _wordDelimiters[t.plus] = true, _wordDelimiters[t.pipe] = true, _wordDelimiters[t.tilde] = true, _wordDelimiters[t.greaterThan] = true, _wordDelimiters[t.equals] = true, _wordDelimiters[t.dollar] = true, _wordDelimiters[t.caret] = true, _wordDelimiters[t.slash] = true, _wordDelimiters);
	var hex = {};
	var hexChars = "0123456789abcdefABCDEF";
	for (var i = 0; i < hexChars.length; i++) hex[hexChars.charCodeAt(i)] = true;
	/**
	*  Returns the last index of the bar css word
	* @param {string} css The string in which the word begins
	* @param {number} start The index into the string where word's first letter occurs
	*/
	function consumeWord(css, start) {
		var next = start;
		var code;
		do {
			code = css.charCodeAt(next);
			if (wordDelimiters[code]) return next - 1;
			else if (code === t.backslash) next = consumeEscape(css, next) + 1;
			else next++;
		} while (next < css.length);
		return next - 1;
	}
	/**
	*  Returns the last index of the escape sequence
	* @param {string} css The string in which the sequence begins
	* @param {number} start The index into the string where escape character (`\`) occurs.
	*/
	function consumeEscape(css, start) {
		var next = start;
		var code = css.charCodeAt(next + 1);
		if (unescapable[code]) {} else if (hex[code]) {
			var hexDigits = 0;
			do {
				next++;
				hexDigits++;
				code = css.charCodeAt(next + 1);
			} while (hex[code] && hexDigits < 6);
			if (hexDigits < 6 && code === t.space) next++;
		} else next++;
		return next;
	}
	exports.FIELDS = {
		TYPE: 0,
		START_LINE: 1,
		START_COL: 2,
		END_LINE: 3,
		END_COL: 4,
		START_POS: 5,
		END_POS: 6
	};
	function tokenize(input) {
		var tokens = [];
		var css = input.css.valueOf();
		var length = css.length;
		var offset = -1;
		var line = 1;
		var start = 0;
		var end = 0;
		var code, content, endColumn, endLine, escaped, escapePos, last, lines, next, nextLine, nextOffset, quote, tokenType;
		function unclosed(what, fix) {
			if (input.safe) {
				css += fix;
				next = css.length - 1;
			} else throw input.error("Unclosed " + what, line, start - offset, start);
		}
		while (start < length) {
			code = css.charCodeAt(start);
			if (code === t.newline) {
				offset = start;
				line += 1;
			}
			switch (code) {
				case t.space:
				case t.tab:
				case t.newline:
				case t.cr:
				case t.feed:
					next = start;
					do {
						next += 1;
						code = css.charCodeAt(next);
						if (code === t.newline) {
							offset = next;
							line += 1;
						}
					} while (code === t.space || code === t.newline || code === t.tab || code === t.cr || code === t.feed);
					tokenType = t.space;
					endLine = line;
					endColumn = next - offset - 1;
					end = next;
					break;
				case t.plus:
				case t.greaterThan:
				case t.tilde:
				case t.pipe:
					next = start;
					do {
						next += 1;
						code = css.charCodeAt(next);
					} while (code === t.plus || code === t.greaterThan || code === t.tilde || code === t.pipe);
					tokenType = t.combinator;
					endLine = line;
					endColumn = start - offset;
					end = next;
					break;
				case t.asterisk:
				case t.ampersand:
				case t.bang:
				case t.comma:
				case t.equals:
				case t.dollar:
				case t.caret:
				case t.openSquare:
				case t.closeSquare:
				case t.colon:
				case t.semicolon:
				case t.openParenthesis:
				case t.closeParenthesis:
					next = start;
					tokenType = code;
					endLine = line;
					endColumn = start - offset;
					end = next + 1;
					break;
				case t.singleQuote:
				case t.doubleQuote:
					quote = code === t.singleQuote ? "'" : "\"";
					next = start;
					do {
						escaped = false;
						next = css.indexOf(quote, next + 1);
						if (next === -1) unclosed("quote", quote);
						escapePos = next;
						while (css.charCodeAt(escapePos - 1) === t.backslash) {
							escapePos -= 1;
							escaped = !escaped;
						}
					} while (escaped);
					tokenType = t.str;
					endLine = line;
					endColumn = start - offset;
					end = next + 1;
					break;
				default:
					if (code === t.slash && css.charCodeAt(start + 1) === t.asterisk) {
						next = css.indexOf("*/", start + 2) + 1;
						if (next === 0) unclosed("comment", "*/");
						content = css.slice(start, next + 1);
						lines = content.split("\n");
						last = lines.length - 1;
						if (last > 0) {
							nextLine = line + last;
							nextOffset = next - lines[last].length;
						} else {
							nextLine = line;
							nextOffset = offset;
						}
						tokenType = t.comment;
						line = nextLine;
						endLine = nextLine;
						endColumn = next - nextOffset;
					} else if (code === t.slash) {
						next = start;
						tokenType = code;
						endLine = line;
						endColumn = start - offset;
						end = next + 1;
					} else {
						next = consumeWord(css, start);
						tokenType = t.word;
						endLine = line;
						endColumn = next - offset;
					}
					end = next + 1;
					break;
			}
			tokens.push([
				tokenType,
				line,
				start - offset,
				endLine,
				endColumn,
				start,
				end
			]);
			if (nextOffset) {
				offset = nextOffset;
				nextOffset = null;
			}
			start = end;
		}
		return tokens;
	}
}));
//#endregion
//#region ../../node_modules/.pnpm/postcss-selector-parser@7.1.1/node_modules/postcss-selector-parser/dist/parser.js
var require_parser = /* @__PURE__ */ __commonJSMin(((exports, module) => {
	exports.__esModule = true;
	exports["default"] = void 0;
	var _root = _interopRequireDefault(require_root());
	var _selector = _interopRequireDefault(require_selector());
	var _className = _interopRequireDefault(require_className());
	var _comment = _interopRequireDefault(require_comment());
	var _id = _interopRequireDefault(require_id());
	var _tag = _interopRequireDefault(require_tag());
	var _string = _interopRequireDefault(require_string());
	var _pseudo = _interopRequireDefault(require_pseudo());
	var _attribute = _interopRequireWildcard(require_attribute());
	var _universal = _interopRequireDefault(require_universal());
	var _combinator = _interopRequireDefault(require_combinator());
	var _nesting = _interopRequireDefault(require_nesting());
	var _sortAscending = _interopRequireDefault(require_sortAscending());
	var _tokenize = _interopRequireWildcard(require_tokenize());
	var tokens = _interopRequireWildcard(require_tokenTypes());
	var types = _interopRequireWildcard(require_types());
	var _util = require_util();
	var _WHITESPACE_TOKENS, _Object$assign;
	function _getRequireWildcardCache(nodeInterop) {
		if (typeof WeakMap !== "function") return null;
		var cacheBabelInterop = /* @__PURE__ */ new WeakMap();
		var cacheNodeInterop = /* @__PURE__ */ new WeakMap();
		return (_getRequireWildcardCache = function _getRequireWildcardCache(nodeInterop) {
			return nodeInterop ? cacheNodeInterop : cacheBabelInterop;
		})(nodeInterop);
	}
	function _interopRequireWildcard(obj, nodeInterop) {
		if (!nodeInterop && obj && obj.__esModule) return obj;
		if (obj === null || typeof obj !== "object" && typeof obj !== "function") return { "default": obj };
		var cache = _getRequireWildcardCache(nodeInterop);
		if (cache && cache.has(obj)) return cache.get(obj);
		var newObj = {};
		var hasPropertyDescriptor = Object.defineProperty && Object.getOwnPropertyDescriptor;
		for (var key in obj) if (key !== "default" && Object.prototype.hasOwnProperty.call(obj, key)) {
			var desc = hasPropertyDescriptor ? Object.getOwnPropertyDescriptor(obj, key) : null;
			if (desc && (desc.get || desc.set)) Object.defineProperty(newObj, key, desc);
			else newObj[key] = obj[key];
		}
		newObj["default"] = obj;
		if (cache) cache.set(obj, newObj);
		return newObj;
	}
	function _interopRequireDefault(obj) {
		return obj && obj.__esModule ? obj : { "default": obj };
	}
	function _defineProperties(target, props) {
		for (var i = 0; i < props.length; i++) {
			var descriptor = props[i];
			descriptor.enumerable = descriptor.enumerable || false;
			descriptor.configurable = true;
			if ("value" in descriptor) descriptor.writable = true;
			Object.defineProperty(target, descriptor.key, descriptor);
		}
	}
	function _createClass(Constructor, protoProps, staticProps) {
		if (protoProps) _defineProperties(Constructor.prototype, protoProps);
		if (staticProps) _defineProperties(Constructor, staticProps);
		Object.defineProperty(Constructor, "prototype", { writable: false });
		return Constructor;
	}
	var WHITESPACE_TOKENS = (_WHITESPACE_TOKENS = {}, _WHITESPACE_TOKENS[tokens.space] = true, _WHITESPACE_TOKENS[tokens.cr] = true, _WHITESPACE_TOKENS[tokens.feed] = true, _WHITESPACE_TOKENS[tokens.newline] = true, _WHITESPACE_TOKENS[tokens.tab] = true, _WHITESPACE_TOKENS);
	var WHITESPACE_EQUIV_TOKENS = Object.assign({}, WHITESPACE_TOKENS, (_Object$assign = {}, _Object$assign[tokens.comment] = true, _Object$assign));
	function tokenStart(token) {
		return {
			line: token[_tokenize.FIELDS.START_LINE],
			column: token[_tokenize.FIELDS.START_COL]
		};
	}
	function tokenEnd(token) {
		return {
			line: token[_tokenize.FIELDS.END_LINE],
			column: token[_tokenize.FIELDS.END_COL]
		};
	}
	function getSource(startLine, startColumn, endLine, endColumn) {
		return {
			start: {
				line: startLine,
				column: startColumn
			},
			end: {
				line: endLine,
				column: endColumn
			}
		};
	}
	function getTokenSource(token) {
		return getSource(token[_tokenize.FIELDS.START_LINE], token[_tokenize.FIELDS.START_COL], token[_tokenize.FIELDS.END_LINE], token[_tokenize.FIELDS.END_COL]);
	}
	function getTokenSourceSpan(startToken, endToken) {
		if (!startToken) return;
		return getSource(startToken[_tokenize.FIELDS.START_LINE], startToken[_tokenize.FIELDS.START_COL], endToken[_tokenize.FIELDS.END_LINE], endToken[_tokenize.FIELDS.END_COL]);
	}
	function unescapeProp(node, prop) {
		var value = node[prop];
		if (typeof value !== "string") return;
		if (value.indexOf("\\") !== -1) {
			(0, _util.ensureObject)(node, "raws");
			node[prop] = (0, _util.unesc)(value);
			if (node.raws[prop] === void 0) node.raws[prop] = value;
		}
		return node;
	}
	function indexesOf(array, item) {
		var i = -1;
		var indexes = [];
		while ((i = array.indexOf(item, i + 1)) !== -1) indexes.push(i);
		return indexes;
	}
	function uniqs() {
		var list = Array.prototype.concat.apply([], arguments);
		return list.filter(function(item, i) {
			return i === list.indexOf(item);
		});
	}
	exports["default"] = /* @__PURE__ */ function() {
		function Parser(rule, options) {
			if (options === void 0) options = {};
			this.rule = rule;
			this.options = Object.assign({
				lossy: false,
				safe: false
			}, options);
			this.position = 0;
			this.css = typeof this.rule === "string" ? this.rule : this.rule.selector;
			this.tokens = (0, _tokenize["default"])({
				css: this.css,
				error: this._errorGenerator(),
				safe: this.options.safe
			});
			var rootSource = getTokenSourceSpan(this.tokens[0], this.tokens[this.tokens.length - 1]);
			this.root = new _root["default"]({ source: rootSource });
			this.root.errorGenerator = this._errorGenerator();
			var selector = new _selector["default"]({
				source: { start: {
					line: 1,
					column: 1
				} },
				sourceIndex: 0
			});
			this.root.append(selector);
			this.current = selector;
			this.loop();
		}
		var _proto = Parser.prototype;
		_proto._errorGenerator = function _errorGenerator() {
			var _this = this;
			return function(message, errorOptions) {
				if (typeof _this.rule === "string") return new Error(message);
				return _this.rule.error(message, errorOptions);
			};
		};
		_proto.attribute = function attribute() {
			var attr = [];
			var startingToken = this.currToken;
			this.position++;
			while (this.position < this.tokens.length && this.currToken[_tokenize.FIELDS.TYPE] !== tokens.closeSquare) {
				attr.push(this.currToken);
				this.position++;
			}
			if (this.currToken[_tokenize.FIELDS.TYPE] !== tokens.closeSquare) return this.expected("closing square bracket", this.currToken[_tokenize.FIELDS.START_POS]);
			var len = attr.length;
			var node = {
				source: getSource(startingToken[1], startingToken[2], this.currToken[3], this.currToken[4]),
				sourceIndex: startingToken[_tokenize.FIELDS.START_POS]
			};
			if (len === 1 && !~[tokens.word].indexOf(attr[0][_tokenize.FIELDS.TYPE])) return this.expected("attribute", attr[0][_tokenize.FIELDS.START_POS]);
			var pos = 0;
			var spaceBefore = "";
			var commentBefore = "";
			var lastAdded = null;
			var spaceAfterMeaningfulToken = false;
			while (pos < len) {
				var token = attr[pos];
				var content = this.content(token);
				var next = attr[pos + 1];
				switch (token[_tokenize.FIELDS.TYPE]) {
					case tokens.space:
						spaceAfterMeaningfulToken = true;
						if (this.options.lossy) break;
						if (lastAdded) {
							(0, _util.ensureObject)(node, "spaces", lastAdded);
							node.spaces[lastAdded].after = (node.spaces[lastAdded].after || "") + content;
							var existingComment = (0, _util.getProp)(node, "raws", "spaces", lastAdded, "after") || null;
							if (existingComment) node.raws.spaces[lastAdded].after = existingComment + content;
						} else {
							spaceBefore = spaceBefore + content;
							commentBefore = commentBefore + content;
						}
						break;
					case tokens.asterisk:
						if (next[_tokenize.FIELDS.TYPE] === tokens.equals) {
							node.operator = content;
							lastAdded = "operator";
						} else if ((!node.namespace || lastAdded === "namespace" && !spaceAfterMeaningfulToken) && next) {
							if (spaceBefore) {
								(0, _util.ensureObject)(node, "spaces", "attribute");
								node.spaces.attribute.before = spaceBefore;
								spaceBefore = "";
							}
							if (commentBefore) {
								(0, _util.ensureObject)(node, "raws", "spaces", "attribute");
								node.raws.spaces.attribute.before = spaceBefore;
								commentBefore = "";
							}
							node.namespace = (node.namespace || "") + content;
							if ((0, _util.getProp)(node, "raws", "namespace") || null) node.raws.namespace += content;
							lastAdded = "namespace";
						}
						spaceAfterMeaningfulToken = false;
						break;
					case tokens.dollar: if (lastAdded === "value") {
						var oldRawValue = (0, _util.getProp)(node, "raws", "value");
						node.value += "$";
						if (oldRawValue) node.raws.value = oldRawValue + "$";
						break;
					}
					case tokens.caret:
						if (next[_tokenize.FIELDS.TYPE] === tokens.equals) {
							node.operator = content;
							lastAdded = "operator";
						}
						spaceAfterMeaningfulToken = false;
						break;
					case tokens.combinator:
						if (content === "~" && next[_tokenize.FIELDS.TYPE] === tokens.equals) {
							node.operator = content;
							lastAdded = "operator";
						}
						if (content !== "|") {
							spaceAfterMeaningfulToken = false;
							break;
						}
						if (next[_tokenize.FIELDS.TYPE] === tokens.equals) {
							node.operator = content;
							lastAdded = "operator";
						} else if (!node.namespace && !node.attribute) node.namespace = true;
						spaceAfterMeaningfulToken = false;
						break;
					case tokens.word:
						if (next && this.content(next) === "|" && attr[pos + 2] && attr[pos + 2][_tokenize.FIELDS.TYPE] !== tokens.equals && !node.operator && !node.namespace) {
							node.namespace = content;
							lastAdded = "namespace";
						} else if (!node.attribute || lastAdded === "attribute" && !spaceAfterMeaningfulToken) {
							if (spaceBefore) {
								(0, _util.ensureObject)(node, "spaces", "attribute");
								node.spaces.attribute.before = spaceBefore;
								spaceBefore = "";
							}
							if (commentBefore) {
								(0, _util.ensureObject)(node, "raws", "spaces", "attribute");
								node.raws.spaces.attribute.before = commentBefore;
								commentBefore = "";
							}
							node.attribute = (node.attribute || "") + content;
							if ((0, _util.getProp)(node, "raws", "attribute") || null) node.raws.attribute += content;
							lastAdded = "attribute";
						} else if (!node.value && node.value !== "" || lastAdded === "value" && !(spaceAfterMeaningfulToken || node.quoteMark)) {
							var _unescaped = (0, _util.unesc)(content);
							var _oldRawValue = (0, _util.getProp)(node, "raws", "value") || "";
							var oldValue = node.value || "";
							node.value = oldValue + _unescaped;
							node.quoteMark = null;
							if (_unescaped !== content || _oldRawValue) {
								(0, _util.ensureObject)(node, "raws");
								node.raws.value = (_oldRawValue || oldValue) + content;
							}
							lastAdded = "value";
						} else {
							var insensitive = content === "i" || content === "I";
							if ((node.value || node.value === "") && (node.quoteMark || spaceAfterMeaningfulToken)) {
								node.insensitive = insensitive;
								if (!insensitive || content === "I") {
									(0, _util.ensureObject)(node, "raws");
									node.raws.insensitiveFlag = content;
								}
								lastAdded = "insensitive";
								if (spaceBefore) {
									(0, _util.ensureObject)(node, "spaces", "insensitive");
									node.spaces.insensitive.before = spaceBefore;
									spaceBefore = "";
								}
								if (commentBefore) {
									(0, _util.ensureObject)(node, "raws", "spaces", "insensitive");
									node.raws.spaces.insensitive.before = commentBefore;
									commentBefore = "";
								}
							} else if (node.value || node.value === "") {
								lastAdded = "value";
								node.value += content;
								if (node.raws.value) node.raws.value += content;
							}
						}
						spaceAfterMeaningfulToken = false;
						break;
					case tokens.str:
						if (!node.attribute || !node.operator) return this.error("Expected an attribute followed by an operator preceding the string.", { index: token[_tokenize.FIELDS.START_POS] });
						var _unescapeValue = (0, _attribute.unescapeValue)(content), unescaped = _unescapeValue.unescaped, quoteMark = _unescapeValue.quoteMark;
						node.value = unescaped;
						node.quoteMark = quoteMark;
						lastAdded = "value";
						(0, _util.ensureObject)(node, "raws");
						node.raws.value = content;
						spaceAfterMeaningfulToken = false;
						break;
					case tokens.equals:
						if (!node.attribute) return this.expected("attribute", token[_tokenize.FIELDS.START_POS], content);
						if (node.value) return this.error("Unexpected \"=\" found; an operator was already defined.", { index: token[_tokenize.FIELDS.START_POS] });
						node.operator = node.operator ? node.operator + content : content;
						lastAdded = "operator";
						spaceAfterMeaningfulToken = false;
						break;
					case tokens.comment:
						if (lastAdded) if (spaceAfterMeaningfulToken || next && next[_tokenize.FIELDS.TYPE] === tokens.space || lastAdded === "insensitive") {
							var lastComment = (0, _util.getProp)(node, "spaces", lastAdded, "after") || "";
							var rawLastComment = (0, _util.getProp)(node, "raws", "spaces", lastAdded, "after") || lastComment;
							(0, _util.ensureObject)(node, "raws", "spaces", lastAdded);
							node.raws.spaces[lastAdded].after = rawLastComment + content;
						} else {
							var lastValue = node[lastAdded] || "";
							var rawLastValue = (0, _util.getProp)(node, "raws", lastAdded) || lastValue;
							(0, _util.ensureObject)(node, "raws");
							node.raws[lastAdded] = rawLastValue + content;
						}
						else commentBefore = commentBefore + content;
						break;
					default: return this.error("Unexpected \"" + content + "\" found.", { index: token[_tokenize.FIELDS.START_POS] });
				}
				pos++;
			}
			unescapeProp(node, "attribute");
			unescapeProp(node, "namespace");
			this.newNode(new _attribute["default"](node));
			this.position++;
		};
		_proto.parseWhitespaceEquivalentTokens = function parseWhitespaceEquivalentTokens(stopPosition) {
			if (stopPosition < 0) stopPosition = this.tokens.length;
			var startPosition = this.position;
			var nodes = [];
			var space = "";
			var lastComment = void 0;
			do
				if (WHITESPACE_TOKENS[this.currToken[_tokenize.FIELDS.TYPE]]) {
					if (!this.options.lossy) space += this.content();
				} else if (this.currToken[_tokenize.FIELDS.TYPE] === tokens.comment) {
					var spaces = {};
					if (space) {
						spaces.before = space;
						space = "";
					}
					lastComment = new _comment["default"]({
						value: this.content(),
						source: getTokenSource(this.currToken),
						sourceIndex: this.currToken[_tokenize.FIELDS.START_POS],
						spaces
					});
					nodes.push(lastComment);
				}
			while (++this.position < stopPosition);
			if (space) {
				if (lastComment) lastComment.spaces.after = space;
				else if (!this.options.lossy) {
					var firstToken = this.tokens[startPosition];
					var lastToken = this.tokens[this.position - 1];
					nodes.push(new _string["default"]({
						value: "",
						source: getSource(firstToken[_tokenize.FIELDS.START_LINE], firstToken[_tokenize.FIELDS.START_COL], lastToken[_tokenize.FIELDS.END_LINE], lastToken[_tokenize.FIELDS.END_COL]),
						sourceIndex: firstToken[_tokenize.FIELDS.START_POS],
						spaces: {
							before: space,
							after: ""
						}
					}));
				}
			}
			return nodes;
		};
		_proto.convertWhitespaceNodesToSpace = function convertWhitespaceNodesToSpace(nodes, requiredSpace) {
			var _this2 = this;
			if (requiredSpace === void 0) requiredSpace = false;
			var space = "";
			var rawSpace = "";
			nodes.forEach(function(n) {
				var spaceBefore = _this2.lossySpace(n.spaces.before, requiredSpace);
				var rawSpaceBefore = _this2.lossySpace(n.rawSpaceBefore, requiredSpace);
				space += spaceBefore + _this2.lossySpace(n.spaces.after, requiredSpace && spaceBefore.length === 0);
				rawSpace += spaceBefore + n.value + _this2.lossySpace(n.rawSpaceAfter, requiredSpace && rawSpaceBefore.length === 0);
			});
			if (rawSpace === space) rawSpace = void 0;
			return {
				space,
				rawSpace
			};
		};
		_proto.isNamedCombinator = function isNamedCombinator(position) {
			if (position === void 0) position = this.position;
			return this.tokens[position + 0] && this.tokens[position + 0][_tokenize.FIELDS.TYPE] === tokens.slash && this.tokens[position + 1] && this.tokens[position + 1][_tokenize.FIELDS.TYPE] === tokens.word && this.tokens[position + 2] && this.tokens[position + 2][_tokenize.FIELDS.TYPE] === tokens.slash;
		};
		_proto.namedCombinator = function namedCombinator() {
			if (this.isNamedCombinator()) {
				var nameRaw = this.content(this.tokens[this.position + 1]);
				var name = (0, _util.unesc)(nameRaw).toLowerCase();
				var raws = {};
				if (name !== nameRaw) raws.value = "/" + nameRaw + "/";
				var node = new _combinator["default"]({
					value: "/" + name + "/",
					source: getSource(this.currToken[_tokenize.FIELDS.START_LINE], this.currToken[_tokenize.FIELDS.START_COL], this.tokens[this.position + 2][_tokenize.FIELDS.END_LINE], this.tokens[this.position + 2][_tokenize.FIELDS.END_COL]),
					sourceIndex: this.currToken[_tokenize.FIELDS.START_POS],
					raws
				});
				this.position = this.position + 3;
				return node;
			} else this.unexpected();
		};
		_proto.combinator = function combinator() {
			var _this3 = this;
			if (this.content() === "|") return this.namespace();
			var nextSigTokenPos = this.locateNextMeaningfulToken(this.position);
			if (nextSigTokenPos < 0 || this.tokens[nextSigTokenPos][_tokenize.FIELDS.TYPE] === tokens.comma || this.tokens[nextSigTokenPos][_tokenize.FIELDS.TYPE] === tokens.closeParenthesis) {
				var nodes = this.parseWhitespaceEquivalentTokens(nextSigTokenPos);
				if (nodes.length > 0) {
					var last = this.current.last;
					if (last) {
						var _this$convertWhitespa = this.convertWhitespaceNodesToSpace(nodes), space = _this$convertWhitespa.space, rawSpace = _this$convertWhitespa.rawSpace;
						if (rawSpace !== void 0) last.rawSpaceAfter += rawSpace;
						last.spaces.after += space;
					} else nodes.forEach(function(n) {
						return _this3.newNode(n);
					});
				}
				return;
			}
			var firstToken = this.currToken;
			var spaceOrDescendantSelectorNodes = void 0;
			if (nextSigTokenPos > this.position) spaceOrDescendantSelectorNodes = this.parseWhitespaceEquivalentTokens(nextSigTokenPos);
			var node;
			if (this.isNamedCombinator()) node = this.namedCombinator();
			else if (this.currToken[_tokenize.FIELDS.TYPE] === tokens.combinator) {
				node = new _combinator["default"]({
					value: this.content(),
					source: getTokenSource(this.currToken),
					sourceIndex: this.currToken[_tokenize.FIELDS.START_POS]
				});
				this.position++;
			} else if (WHITESPACE_TOKENS[this.currToken[_tokenize.FIELDS.TYPE]]) {} else if (!spaceOrDescendantSelectorNodes) this.unexpected();
			if (node) {
				if (spaceOrDescendantSelectorNodes) {
					var _this$convertWhitespa2 = this.convertWhitespaceNodesToSpace(spaceOrDescendantSelectorNodes), _space = _this$convertWhitespa2.space, _rawSpace = _this$convertWhitespa2.rawSpace;
					node.spaces.before = _space;
					node.rawSpaceBefore = _rawSpace;
				}
			} else {
				var _this$convertWhitespa3 = this.convertWhitespaceNodesToSpace(spaceOrDescendantSelectorNodes, true), _space2 = _this$convertWhitespa3.space, _rawSpace2 = _this$convertWhitespa3.rawSpace;
				if (!_rawSpace2) _rawSpace2 = _space2;
				var spaces = {};
				var raws = { spaces: {} };
				if (_space2.endsWith(" ") && _rawSpace2.endsWith(" ")) {
					spaces.before = _space2.slice(0, _space2.length - 1);
					raws.spaces.before = _rawSpace2.slice(0, _rawSpace2.length - 1);
				} else if (_space2[0] === " " && _rawSpace2[0] === " ") {
					spaces.after = _space2.slice(1);
					raws.spaces.after = _rawSpace2.slice(1);
				} else raws.value = _rawSpace2;
				node = new _combinator["default"]({
					value: " ",
					source: getTokenSourceSpan(firstToken, this.tokens[this.position - 1]),
					sourceIndex: firstToken[_tokenize.FIELDS.START_POS],
					spaces,
					raws
				});
			}
			if (this.currToken && this.currToken[_tokenize.FIELDS.TYPE] === tokens.space) {
				node.spaces.after = this.optionalSpace(this.content());
				this.position++;
			}
			return this.newNode(node);
		};
		_proto.comma = function comma() {
			if (this.position === this.tokens.length - 1) {
				this.root.trailingComma = true;
				this.position++;
				return;
			}
			this.current._inferEndPosition();
			var selector = new _selector["default"]({
				source: { start: tokenStart(this.tokens[this.position + 1]) },
				sourceIndex: this.tokens[this.position + 1][_tokenize.FIELDS.START_POS]
			});
			this.current.parent.append(selector);
			this.current = selector;
			this.position++;
		};
		_proto.comment = function comment() {
			var current = this.currToken;
			this.newNode(new _comment["default"]({
				value: this.content(),
				source: getTokenSource(current),
				sourceIndex: current[_tokenize.FIELDS.START_POS]
			}));
			this.position++;
		};
		_proto.error = function error(message, opts) {
			throw this.root.error(message, opts);
		};
		_proto.missingBackslash = function missingBackslash() {
			return this.error("Expected a backslash preceding the semicolon.", { index: this.currToken[_tokenize.FIELDS.START_POS] });
		};
		_proto.missingParenthesis = function missingParenthesis() {
			return this.expected("opening parenthesis", this.currToken[_tokenize.FIELDS.START_POS]);
		};
		_proto.missingSquareBracket = function missingSquareBracket() {
			return this.expected("opening square bracket", this.currToken[_tokenize.FIELDS.START_POS]);
		};
		_proto.unexpected = function unexpected() {
			return this.error("Unexpected '" + this.content() + "'. Escaping special characters with \\ may help.", this.currToken[_tokenize.FIELDS.START_POS]);
		};
		_proto.unexpectedPipe = function unexpectedPipe() {
			return this.error("Unexpected '|'.", this.currToken[_tokenize.FIELDS.START_POS]);
		};
		_proto.namespace = function namespace() {
			var before = this.prevToken && this.content(this.prevToken) || true;
			if (this.nextToken[_tokenize.FIELDS.TYPE] === tokens.word) {
				this.position++;
				return this.word(before);
			} else if (this.nextToken[_tokenize.FIELDS.TYPE] === tokens.asterisk) {
				this.position++;
				return this.universal(before);
			}
			this.unexpectedPipe();
		};
		_proto.nesting = function nesting() {
			if (this.nextToken) {
				if (this.content(this.nextToken) === "|") {
					this.position++;
					return;
				}
			}
			var current = this.currToken;
			this.newNode(new _nesting["default"]({
				value: this.content(),
				source: getTokenSource(current),
				sourceIndex: current[_tokenize.FIELDS.START_POS]
			}));
			this.position++;
		};
		_proto.parentheses = function parentheses() {
			var last = this.current.last;
			var unbalanced = 1;
			this.position++;
			if (last && last.type === types.PSEUDO) {
				var selector = new _selector["default"]({
					source: { start: tokenStart(this.tokens[this.position]) },
					sourceIndex: this.tokens[this.position][_tokenize.FIELDS.START_POS]
				});
				var cache = this.current;
				last.append(selector);
				this.current = selector;
				while (this.position < this.tokens.length && unbalanced) {
					if (this.currToken[_tokenize.FIELDS.TYPE] === tokens.openParenthesis) unbalanced++;
					if (this.currToken[_tokenize.FIELDS.TYPE] === tokens.closeParenthesis) unbalanced--;
					if (unbalanced) this.parse();
					else {
						this.current.source.end = tokenEnd(this.currToken);
						this.current.parent.source.end = tokenEnd(this.currToken);
						this.position++;
					}
				}
				this.current = cache;
			} else {
				var parenStart = this.currToken;
				var parenValue = "(";
				var parenEnd;
				while (this.position < this.tokens.length && unbalanced) {
					if (this.currToken[_tokenize.FIELDS.TYPE] === tokens.openParenthesis) unbalanced++;
					if (this.currToken[_tokenize.FIELDS.TYPE] === tokens.closeParenthesis) unbalanced--;
					parenEnd = this.currToken;
					parenValue += this.parseParenthesisToken(this.currToken);
					this.position++;
				}
				if (last) last.appendToPropertyAndEscape("value", parenValue, parenValue);
				else this.newNode(new _string["default"]({
					value: parenValue,
					source: getSource(parenStart[_tokenize.FIELDS.START_LINE], parenStart[_tokenize.FIELDS.START_COL], parenEnd[_tokenize.FIELDS.END_LINE], parenEnd[_tokenize.FIELDS.END_COL]),
					sourceIndex: parenStart[_tokenize.FIELDS.START_POS]
				}));
			}
			if (unbalanced) return this.expected("closing parenthesis", this.currToken[_tokenize.FIELDS.START_POS]);
		};
		_proto.pseudo = function pseudo() {
			var _this4 = this;
			var pseudoStr = "";
			var startingToken = this.currToken;
			while (this.currToken && this.currToken[_tokenize.FIELDS.TYPE] === tokens.colon) {
				pseudoStr += this.content();
				this.position++;
			}
			if (!this.currToken) return this.expected(["pseudo-class", "pseudo-element"], this.position - 1);
			if (this.currToken[_tokenize.FIELDS.TYPE] === tokens.word) this.splitWord(false, function(first, length) {
				pseudoStr += first;
				_this4.newNode(new _pseudo["default"]({
					value: pseudoStr,
					source: getTokenSourceSpan(startingToken, _this4.currToken),
					sourceIndex: startingToken[_tokenize.FIELDS.START_POS]
				}));
				if (length > 1 && _this4.nextToken && _this4.nextToken[_tokenize.FIELDS.TYPE] === tokens.openParenthesis) _this4.error("Misplaced parenthesis.", { index: _this4.nextToken[_tokenize.FIELDS.START_POS] });
			});
			else return this.expected(["pseudo-class", "pseudo-element"], this.currToken[_tokenize.FIELDS.START_POS]);
		};
		_proto.space = function space() {
			var content = this.content();
			if (this.position === 0 || this.prevToken[_tokenize.FIELDS.TYPE] === tokens.comma || this.prevToken[_tokenize.FIELDS.TYPE] === tokens.openParenthesis || this.current.nodes.every(function(node) {
				return node.type === "comment";
			})) {
				this.spaces = this.optionalSpace(content);
				this.position++;
			} else if (this.position === this.tokens.length - 1 || this.nextToken[_tokenize.FIELDS.TYPE] === tokens.comma || this.nextToken[_tokenize.FIELDS.TYPE] === tokens.closeParenthesis) {
				this.current.last.spaces.after = this.optionalSpace(content);
				this.position++;
			} else this.combinator();
		};
		_proto.string = function string() {
			var current = this.currToken;
			this.newNode(new _string["default"]({
				value: this.content(),
				source: getTokenSource(current),
				sourceIndex: current[_tokenize.FIELDS.START_POS]
			}));
			this.position++;
		};
		_proto.universal = function universal(namespace) {
			var nextToken = this.nextToken;
			if (nextToken && this.content(nextToken) === "|") {
				this.position++;
				return this.namespace();
			}
			var current = this.currToken;
			this.newNode(new _universal["default"]({
				value: this.content(),
				source: getTokenSource(current),
				sourceIndex: current[_tokenize.FIELDS.START_POS]
			}), namespace);
			this.position++;
		};
		_proto.splitWord = function splitWord(namespace, firstCallback) {
			var _this5 = this;
			var nextToken = this.nextToken;
			var word = this.content();
			while (nextToken && ~[
				tokens.dollar,
				tokens.caret,
				tokens.equals,
				tokens.word
			].indexOf(nextToken[_tokenize.FIELDS.TYPE])) {
				this.position++;
				var current = this.content();
				word += current;
				if (current.lastIndexOf("\\") === current.length - 1) {
					var next = this.nextToken;
					if (next && next[_tokenize.FIELDS.TYPE] === tokens.space) {
						word += this.requiredSpace(this.content(next));
						this.position++;
					}
				}
				nextToken = this.nextToken;
			}
			var hasClass = indexesOf(word, ".").filter(function(i) {
				var escapedDot = word[i - 1] === "\\";
				var isKeyframesPercent = /^\d+\.\d+%$/.test(word);
				return !escapedDot && !isKeyframesPercent;
			});
			var hasId = indexesOf(word, "#").filter(function(i) {
				return word[i - 1] !== "\\";
			});
			var interpolations = indexesOf(word, "#{");
			if (interpolations.length) hasId = hasId.filter(function(hashIndex) {
				return !~interpolations.indexOf(hashIndex);
			});
			var indices = (0, _sortAscending["default"])(uniqs([0].concat(hasClass, hasId)));
			indices.forEach(function(ind, i) {
				var index = indices[i + 1] || word.length;
				var value = word.slice(ind, index);
				if (i === 0 && firstCallback) return firstCallback.call(_this5, value, indices.length);
				var node;
				var current = _this5.currToken;
				var sourceIndex = current[_tokenize.FIELDS.START_POS] + indices[i];
				var source = getSource(current[1], current[2] + ind, current[3], current[2] + (index - 1));
				if (~hasClass.indexOf(ind)) {
					var classNameOpts = {
						value: value.slice(1),
						source,
						sourceIndex
					};
					node = new _className["default"](unescapeProp(classNameOpts, "value"));
				} else if (~hasId.indexOf(ind)) {
					var idOpts = {
						value: value.slice(1),
						source,
						sourceIndex
					};
					node = new _id["default"](unescapeProp(idOpts, "value"));
				} else {
					var tagOpts = {
						value,
						source,
						sourceIndex
					};
					unescapeProp(tagOpts, "value");
					node = new _tag["default"](tagOpts);
				}
				_this5.newNode(node, namespace);
				namespace = null;
			});
			this.position++;
		};
		_proto.word = function word(namespace) {
			var nextToken = this.nextToken;
			if (nextToken && this.content(nextToken) === "|") {
				this.position++;
				return this.namespace();
			}
			return this.splitWord(namespace);
		};
		_proto.loop = function loop() {
			while (this.position < this.tokens.length) this.parse(true);
			this.current._inferEndPosition();
			return this.root;
		};
		_proto.parse = function parse(throwOnParenthesis) {
			switch (this.currToken[_tokenize.FIELDS.TYPE]) {
				case tokens.space:
					this.space();
					break;
				case tokens.comment:
					this.comment();
					break;
				case tokens.openParenthesis:
					this.parentheses();
					break;
				case tokens.closeParenthesis:
					if (throwOnParenthesis) this.missingParenthesis();
					break;
				case tokens.openSquare:
					this.attribute();
					break;
				case tokens.dollar:
				case tokens.caret:
				case tokens.equals:
				case tokens.word:
					this.word();
					break;
				case tokens.colon:
					this.pseudo();
					break;
				case tokens.comma:
					this.comma();
					break;
				case tokens.asterisk:
					this.universal();
					break;
				case tokens.ampersand:
					this.nesting();
					break;
				case tokens.slash:
				case tokens.combinator:
					this.combinator();
					break;
				case tokens.str:
					this.string();
					break;
				case tokens.closeSquare: this.missingSquareBracket();
				case tokens.semicolon: this.missingBackslash();
				default: this.unexpected();
			}
		};
		_proto.expected = function expected(description, index, found) {
			if (Array.isArray(description)) {
				var last = description.pop();
				description = description.join(", ") + " or " + last;
			}
			var an = /^[aeiou]/.test(description[0]) ? "an" : "a";
			if (!found) return this.error("Expected " + an + " " + description + ".", { index });
			return this.error("Expected " + an + " " + description + ", found \"" + found + "\" instead.", { index });
		};
		_proto.requiredSpace = function requiredSpace(space) {
			return this.options.lossy ? " " : space;
		};
		_proto.optionalSpace = function optionalSpace(space) {
			return this.options.lossy ? "" : space;
		};
		_proto.lossySpace = function lossySpace(space, required) {
			if (this.options.lossy) return required ? " " : "";
			else return space;
		};
		_proto.parseParenthesisToken = function parseParenthesisToken(token) {
			var content = this.content(token);
			if (token[_tokenize.FIELDS.TYPE] === tokens.space) return this.requiredSpace(content);
			else return content;
		};
		_proto.newNode = function newNode(node, namespace) {
			if (namespace) {
				if (/^ +$/.test(namespace)) {
					if (!this.options.lossy) this.spaces = (this.spaces || "") + namespace;
					namespace = true;
				}
				node.namespace = namespace;
				unescapeProp(node, "namespace");
			}
			if (this.spaces) {
				node.spaces.before = this.spaces;
				this.spaces = "";
			}
			return this.current.append(node);
		};
		_proto.content = function content(token) {
			if (token === void 0) token = this.currToken;
			return this.css.slice(token[_tokenize.FIELDS.START_POS], token[_tokenize.FIELDS.END_POS]);
		};
		/**
		* returns the index of the next non-whitespace, non-comment token.
		* returns -1 if no meaningful token is found.
		*/
		_proto.locateNextMeaningfulToken = function locateNextMeaningfulToken(startPosition) {
			if (startPosition === void 0) startPosition = this.position + 1;
			var searchPosition = startPosition;
			while (searchPosition < this.tokens.length) if (WHITESPACE_EQUIV_TOKENS[this.tokens[searchPosition][_tokenize.FIELDS.TYPE]]) {
				searchPosition++;
				continue;
			} else return searchPosition;
			return -1;
		};
		_createClass(Parser, [
			{
				key: "currToken",
				get: function get() {
					return this.tokens[this.position];
				}
			},
			{
				key: "nextToken",
				get: function get() {
					return this.tokens[this.position + 1];
				}
			},
			{
				key: "prevToken",
				get: function get() {
					return this.tokens[this.position - 1];
				}
			}
		]);
		return Parser;
	}();
	module.exports = exports.default;
}));
//#endregion
//#region ../../node_modules/.pnpm/postcss-selector-parser@7.1.1/node_modules/postcss-selector-parser/dist/processor.js
var require_processor = /* @__PURE__ */ __commonJSMin(((exports, module) => {
	exports.__esModule = true;
	exports["default"] = void 0;
	var _parser = _interopRequireDefault(require_parser());
	function _interopRequireDefault(obj) {
		return obj && obj.__esModule ? obj : { "default": obj };
	}
	exports["default"] = /* @__PURE__ */ function() {
		function Processor(func, options) {
			this.func = func || function noop() {};
			this.funcRes = null;
			this.options = options;
		}
		var _proto = Processor.prototype;
		_proto._shouldUpdateSelector = function _shouldUpdateSelector(rule, options) {
			if (options === void 0) options = {};
			if (Object.assign({}, this.options, options).updateSelector === false) return false;
			else return typeof rule !== "string";
		};
		_proto._isLossy = function _isLossy(options) {
			if (options === void 0) options = {};
			if (Object.assign({}, this.options, options).lossless === false) return true;
			else return false;
		};
		_proto._root = function _root(rule, options) {
			if (options === void 0) options = {};
			return new _parser["default"](rule, this._parseOptions(options)).root;
		};
		_proto._parseOptions = function _parseOptions(options) {
			return { lossy: this._isLossy(options) };
		};
		_proto._run = function _run(rule, options) {
			var _this = this;
			if (options === void 0) options = {};
			return new Promise(function(resolve, reject) {
				try {
					var root = _this._root(rule, options);
					Promise.resolve(_this.func(root)).then(function(transform) {
						var string = void 0;
						if (_this._shouldUpdateSelector(rule, options)) {
							string = root.toString();
							rule.selector = string;
						}
						return {
							transform,
							root,
							string
						};
					}).then(resolve, reject);
				} catch (e) {
					reject(e);
					return;
				}
			});
		};
		_proto._runSync = function _runSync(rule, options) {
			if (options === void 0) options = {};
			var root = this._root(rule, options);
			var transform = this.func(root);
			if (transform && typeof transform.then === "function") throw new Error("Selector processor returned a promise to a synchronous call.");
			var string = void 0;
			if (options.updateSelector && typeof rule !== "string") {
				string = root.toString();
				rule.selector = string;
			}
			return {
				transform,
				root,
				string
			};
		};
		_proto.ast = function ast(rule, options) {
			return this._run(rule, options).then(function(result) {
				return result.root;
			});
		};
		_proto.astSync = function astSync(rule, options) {
			return this._runSync(rule, options).root;
		};
		_proto.transform = function transform(rule, options) {
			return this._run(rule, options).then(function(result) {
				return result.transform;
			});
		};
		_proto.transformSync = function transformSync(rule, options) {
			return this._runSync(rule, options).transform;
		};
		_proto.process = function process(rule, options) {
			return this._run(rule, options).then(function(result) {
				return result.string || result.root.toString();
			});
		};
		_proto.processSync = function processSync(rule, options) {
			var result = this._runSync(rule, options);
			return result.string || result.root.toString();
		};
		return Processor;
	}();
	module.exports = exports.default;
}));
//#endregion
//#region ../../node_modules/.pnpm/postcss-selector-parser@7.1.1/node_modules/postcss-selector-parser/dist/selectors/constructors.js
var require_constructors = /* @__PURE__ */ __commonJSMin(((exports) => {
	exports.__esModule = true;
	exports.universal = exports.tag = exports.string = exports.selector = exports.root = exports.pseudo = exports.nesting = exports.id = exports.comment = exports.combinator = exports.className = exports.attribute = void 0;
	var _attribute = _interopRequireDefault(require_attribute());
	var _className = _interopRequireDefault(require_className());
	var _combinator = _interopRequireDefault(require_combinator());
	var _comment = _interopRequireDefault(require_comment());
	var _id = _interopRequireDefault(require_id());
	var _nesting = _interopRequireDefault(require_nesting());
	var _pseudo = _interopRequireDefault(require_pseudo());
	var _root = _interopRequireDefault(require_root());
	var _selector = _interopRequireDefault(require_selector());
	var _string = _interopRequireDefault(require_string());
	var _tag = _interopRequireDefault(require_tag());
	var _universal = _interopRequireDefault(require_universal());
	function _interopRequireDefault(obj) {
		return obj && obj.__esModule ? obj : { "default": obj };
	}
	exports.attribute = function attribute(opts) {
		return new _attribute["default"](opts);
	};
	exports.className = function className(opts) {
		return new _className["default"](opts);
	};
	exports.combinator = function combinator(opts) {
		return new _combinator["default"](opts);
	};
	exports.comment = function comment(opts) {
		return new _comment["default"](opts);
	};
	exports.id = function id(opts) {
		return new _id["default"](opts);
	};
	exports.nesting = function nesting(opts) {
		return new _nesting["default"](opts);
	};
	exports.pseudo = function pseudo(opts) {
		return new _pseudo["default"](opts);
	};
	exports.root = function root(opts) {
		return new _root["default"](opts);
	};
	exports.selector = function selector(opts) {
		return new _selector["default"](opts);
	};
	exports.string = function string(opts) {
		return new _string["default"](opts);
	};
	exports.tag = function tag(opts) {
		return new _tag["default"](opts);
	};
	exports.universal = function universal(opts) {
		return new _universal["default"](opts);
	};
}));
//#endregion
//#region ../../node_modules/.pnpm/postcss-selector-parser@7.1.1/node_modules/postcss-selector-parser/dist/selectors/guards.js
var require_guards = /* @__PURE__ */ __commonJSMin(((exports) => {
	exports.__esModule = true;
	exports.isComment = exports.isCombinator = exports.isClassName = exports.isAttribute = void 0;
	exports.isContainer = isContainer;
	exports.isIdentifier = void 0;
	exports.isNamespace = isNamespace;
	exports.isNesting = void 0;
	exports.isNode = isNode;
	exports.isPseudo = void 0;
	exports.isPseudoClass = isPseudoClass;
	exports.isPseudoElement = isPseudoElement;
	exports.isUniversal = exports.isTag = exports.isString = exports.isSelector = exports.isRoot = void 0;
	var _types = require_types();
	var _IS_TYPE;
	var IS_TYPE = (_IS_TYPE = {}, _IS_TYPE[_types.ATTRIBUTE] = true, _IS_TYPE[_types.CLASS] = true, _IS_TYPE[_types.COMBINATOR] = true, _IS_TYPE[_types.COMMENT] = true, _IS_TYPE[_types.ID] = true, _IS_TYPE[_types.NESTING] = true, _IS_TYPE[_types.PSEUDO] = true, _IS_TYPE[_types.ROOT] = true, _IS_TYPE[_types.SELECTOR] = true, _IS_TYPE[_types.STRING] = true, _IS_TYPE[_types.TAG] = true, _IS_TYPE[_types.UNIVERSAL] = true, _IS_TYPE);
	function isNode(node) {
		return typeof node === "object" && IS_TYPE[node.type];
	}
	function isNodeType(type, node) {
		return isNode(node) && node.type === type;
	}
	var isAttribute = isNodeType.bind(null, _types.ATTRIBUTE);
	exports.isAttribute = isAttribute;
	exports.isClassName = isNodeType.bind(null, _types.CLASS);
	exports.isCombinator = isNodeType.bind(null, _types.COMBINATOR);
	exports.isComment = isNodeType.bind(null, _types.COMMENT);
	exports.isIdentifier = isNodeType.bind(null, _types.ID);
	exports.isNesting = isNodeType.bind(null, _types.NESTING);
	var isPseudo = isNodeType.bind(null, _types.PSEUDO);
	exports.isPseudo = isPseudo;
	exports.isRoot = isNodeType.bind(null, _types.ROOT);
	exports.isSelector = isNodeType.bind(null, _types.SELECTOR);
	exports.isString = isNodeType.bind(null, _types.STRING);
	var isTag = isNodeType.bind(null, _types.TAG);
	exports.isTag = isTag;
	exports.isUniversal = isNodeType.bind(null, _types.UNIVERSAL);
	function isPseudoElement(node) {
		return isPseudo(node) && node.value && (node.value.startsWith("::") || node.value.toLowerCase() === ":before" || node.value.toLowerCase() === ":after" || node.value.toLowerCase() === ":first-letter" || node.value.toLowerCase() === ":first-line");
	}
	function isPseudoClass(node) {
		return isPseudo(node) && !isPseudoElement(node);
	}
	function isContainer(node) {
		return !!(isNode(node) && node.walk);
	}
	function isNamespace(node) {
		return isAttribute(node) || isTag(node);
	}
}));
//#endregion
//#region ../../node_modules/.pnpm/postcss-selector-parser@7.1.1/node_modules/postcss-selector-parser/dist/selectors/index.js
var require_selectors = /* @__PURE__ */ __commonJSMin(((exports) => {
	exports.__esModule = true;
	var _types = require_types();
	Object.keys(_types).forEach(function(key) {
		if (key === "default" || key === "__esModule") return;
		if (key in exports && exports[key] === _types[key]) return;
		exports[key] = _types[key];
	});
	var _constructors = require_constructors();
	Object.keys(_constructors).forEach(function(key) {
		if (key === "default" || key === "__esModule") return;
		if (key in exports && exports[key] === _constructors[key]) return;
		exports[key] = _constructors[key];
	});
	var _guards = require_guards();
	Object.keys(_guards).forEach(function(key) {
		if (key === "default" || key === "__esModule") return;
		if (key in exports && exports[key] === _guards[key]) return;
		exports[key] = _guards[key];
	});
}));
//#endregion
//#region ../../node_modules/.pnpm/postcss-selector-parser@7.1.1/node_modules/postcss-selector-parser/dist/index.js
var require_dist = /* @__PURE__ */ __commonJSMin(((exports, module) => {
	exports.__esModule = true;
	exports["default"] = void 0;
	var _processor = _interopRequireDefault(require_processor());
	var selectors = _interopRequireWildcard(require_selectors());
	function _getRequireWildcardCache(nodeInterop) {
		if (typeof WeakMap !== "function") return null;
		var cacheBabelInterop = /* @__PURE__ */ new WeakMap();
		var cacheNodeInterop = /* @__PURE__ */ new WeakMap();
		return (_getRequireWildcardCache = function _getRequireWildcardCache(nodeInterop) {
			return nodeInterop ? cacheNodeInterop : cacheBabelInterop;
		})(nodeInterop);
	}
	function _interopRequireWildcard(obj, nodeInterop) {
		if (!nodeInterop && obj && obj.__esModule) return obj;
		if (obj === null || typeof obj !== "object" && typeof obj !== "function") return { "default": obj };
		var cache = _getRequireWildcardCache(nodeInterop);
		if (cache && cache.has(obj)) return cache.get(obj);
		var newObj = {};
		var hasPropertyDescriptor = Object.defineProperty && Object.getOwnPropertyDescriptor;
		for (var key in obj) if (key !== "default" && Object.prototype.hasOwnProperty.call(obj, key)) {
			var desc = hasPropertyDescriptor ? Object.getOwnPropertyDescriptor(obj, key) : null;
			if (desc && (desc.get || desc.set)) Object.defineProperty(newObj, key, desc);
			else newObj[key] = obj[key];
		}
		newObj["default"] = obj;
		if (cache) cache.set(obj, newObj);
		return newObj;
	}
	function _interopRequireDefault(obj) {
		return obj && obj.__esModule ? obj : { "default": obj };
	}
	var parser = function parser(processor) {
		return new _processor["default"](processor);
	};
	Object.assign(parser, selectors);
	delete parser.__esModule;
	exports["default"] = parser;
	module.exports = exports.default;
}));
//#endregion
//#region ../../node_modules/.pnpm/postcss-modules-local-by-default@4.2.0_postcss@8.5.15/node_modules/postcss-modules-local-by-default/src/index.js
var require_src$2 = /* @__PURE__ */ __commonJSMin(((exports, module) => {
	const selectorParser = require_dist();
	const valueParser = require_lib();
	const { extractICSS } = require_src$4();
	const IGNORE_FILE_MARKER = "cssmodules-pure-no-check";
	const IGNORE_NEXT_LINE_MARKER = "cssmodules-pure-ignore";
	const isSpacing = (node) => node.type === "combinator" && node.value === " ";
	const isPureCheckDisabled = (root) => {
		for (const node of root.nodes) {
			if (node.type !== "comment") return false;
			if (node.text.trim().startsWith(IGNORE_FILE_MARKER)) return true;
		}
		return false;
	};
	function getIgnoreComment(node) {
		if (!node.parent) return;
		const indexInParent = node.parent.index(node);
		for (let i = indexInParent - 1; i >= 0; i--) {
			const prevNode = node.parent.nodes[i];
			if (prevNode.type === "comment") {
				if (prevNode.text.trimStart().startsWith(IGNORE_NEXT_LINE_MARKER)) return prevNode;
			} else break;
		}
	}
	function normalizeNodeArray(nodes) {
		const array = [];
		nodes.forEach((x) => {
			if (Array.isArray(x)) normalizeNodeArray(x).forEach((item) => {
				array.push(item);
			});
			else if (x) array.push(x);
		});
		if (array.length > 0 && isSpacing(array[array.length - 1])) array.pop();
		return array;
	}
	const isPureSelectorSymbol = Symbol("is-pure-selector");
	function localizeNode(rule, mode, localAliasMap) {
		const transform = (node, context) => {
			if (context.ignoreNextSpacing && !isSpacing(node)) throw new Error("Missing whitespace after " + context.ignoreNextSpacing);
			if (context.enforceNoSpacing && isSpacing(node)) throw new Error("Missing whitespace before " + context.enforceNoSpacing);
			let newNodes;
			switch (node.type) {
				case "root": {
					let resultingGlobal;
					context.hasPureGlobals = false;
					newNodes = node.nodes.map((n) => {
						const nContext = {
							global: context.global,
							lastWasSpacing: true,
							hasLocals: false,
							explicit: false
						};
						n = transform(n, nContext);
						if (typeof resultingGlobal === "undefined") resultingGlobal = nContext.global;
						else if (resultingGlobal !== nContext.global) throw new Error("Inconsistent rule global/local result in rule \"" + node + "\" (multiple selectors must result in the same mode for the rule)");
						if (!nContext.hasLocals) context.hasPureGlobals = true;
						return n;
					});
					context.global = resultingGlobal;
					node.nodes = normalizeNodeArray(newNodes);
					break;
				}
				case "selector":
					newNodes = node.map((childNode) => transform(childNode, context));
					node = node.clone();
					node.nodes = normalizeNodeArray(newNodes);
					break;
				case "combinator":
					if (isSpacing(node)) {
						if (context.ignoreNextSpacing) {
							context.ignoreNextSpacing = false;
							context.lastWasSpacing = false;
							context.enforceNoSpacing = false;
							return null;
						}
						context.lastWasSpacing = true;
						return node;
					}
					break;
				case "pseudo": {
					let childContext;
					const isNested = !!node.length;
					const isScoped = node.value === ":local" || node.value === ":global";
					if (node.value === ":import" || node.value === ":export") context.hasLocals = true;
					else if (isNested) {
						if (isScoped) {
							if (node.nodes.length === 0) throw new Error(`${node.value}() can't be empty`);
							if (context.inside) throw new Error(`A ${node.value} is not allowed inside of a ${context.inside}(...)`);
							childContext = {
								global: node.value === ":global",
								inside: node.value,
								hasLocals: false,
								explicit: true
							};
							newNodes = node.map((childNode) => transform(childNode, childContext)).reduce((acc, next) => acc.concat(next.nodes), []);
							if (newNodes.length) {
								const { before, after } = node.spaces;
								const first = newNodes[0];
								const last = newNodes[newNodes.length - 1];
								first.spaces = {
									before,
									after: first.spaces.after
								};
								last.spaces = {
									before: last.spaces.before,
									after
								};
							}
							node = newNodes;
							break;
						} else {
							childContext = {
								global: context.global,
								inside: context.inside,
								lastWasSpacing: true,
								hasLocals: false,
								explicit: context.explicit
							};
							newNodes = node.map((childNode) => {
								const newContext = {
									...childContext,
									enforceNoSpacing: false
								};
								const result = transform(childNode, newContext);
								childContext.global = newContext.global;
								childContext.hasLocals = newContext.hasLocals;
								return result;
							});
							node = node.clone();
							node.nodes = normalizeNodeArray(newNodes);
							if (childContext.hasLocals) context.hasLocals = true;
						}
						break;
					} else if (isScoped) {
						if (context.inside) throw new Error(`A ${node.value} is not allowed inside of a ${context.inside}(...)`);
						const addBackSpacing = !!node.spaces.before;
						context.ignoreNextSpacing = context.lastWasSpacing ? node.value : false;
						context.enforceNoSpacing = context.lastWasSpacing ? false : node.value;
						context.global = node.value === ":global";
						context.explicit = true;
						return addBackSpacing ? selectorParser.combinator({ value: " " }) : null;
					}
					break;
				}
				case "id":
				case "class": {
					if (!node.value) throw new Error("Invalid class or id selector syntax");
					if (context.global) break;
					const isImportedValue = localAliasMap.has(node.value);
					if (!isImportedValue || isImportedValue && context.explicit) {
						const innerNode = node.clone();
						innerNode.spaces = {
							before: "",
							after: ""
						};
						node = selectorParser.pseudo({
							value: ":local",
							nodes: [innerNode],
							spaces: node.spaces
						});
						context.hasLocals = true;
					}
					break;
				}
				case "nesting": if (node.value === "&") context.hasLocals = rule.parent[isPureSelectorSymbol];
			}
			context.lastWasSpacing = false;
			context.ignoreNextSpacing = false;
			context.enforceNoSpacing = false;
			return node;
		};
		const rootContext = {
			global: mode === "global",
			hasPureGlobals: false
		};
		rootContext.selector = selectorParser((root) => {
			transform(root, rootContext);
		}).processSync(rule, {
			updateSelector: false,
			lossless: true
		});
		return rootContext;
	}
	function localizeDeclNode(node, context) {
		switch (node.type) {
			case "word":
				if (context.localizeNextItem) {
					if (!context.localAliasMap.has(node.value)) {
						node.value = ":local(" + node.value + ")";
						context.localizeNextItem = false;
					}
				}
				break;
			case "function":
				if (context.options && context.options.rewriteUrl && node.value.toLowerCase() === "url") node.nodes.map((nestedNode) => {
					if (nestedNode.type !== "string" && nestedNode.type !== "word") return;
					let newUrl = context.options.rewriteUrl(context.global, nestedNode.value);
					switch (nestedNode.type) {
						case "string":
							if (nestedNode.quote === "'") newUrl = newUrl.replace(/(\\)/g, "\\$1").replace(/'/g, "\\'");
							if (nestedNode.quote === "\"") newUrl = newUrl.replace(/(\\)/g, "\\$1").replace(/"/g, "\\\"");
							break;
						case "word":
							newUrl = newUrl.replace(/("|'|\)|\\)/g, "\\$1");
							break;
					}
					nestedNode.value = newUrl;
				});
				break;
		}
		return node;
	}
	const specialKeywords = [
		"none",
		"inherit",
		"initial",
		"revert",
		"revert-layer",
		"unset"
	];
	function localizeDeclarationValues(localize, declaration, context) {
		const valueNodes = valueParser(declaration.value);
		valueNodes.walk((node, index, nodes) => {
			if (node.type === "function" && (node.value.toLowerCase() === "var" || node.value.toLowerCase() === "env")) return false;
			if (node.type === "word" && specialKeywords.includes(node.value.toLowerCase())) return;
			nodes[index] = localizeDeclNode(node, {
				options: context.options,
				global: context.global,
				localizeNextItem: localize && !context.global,
				localAliasMap: context.localAliasMap
			});
		});
		declaration.value = valueNodes.toString();
	}
	const validIdent = /^-?([a-z\u0080-\uFFFF_]|(\\[^\r\n\f])|-(?![0-9]))((\\[^\r\n\f])|[a-z\u0080-\uFFFF_0-9-])*$/i;
	const animationKeywords = {
		$normal: 1,
		$reverse: 1,
		$alternate: 1,
		"$alternate-reverse": 1,
		$forwards: 1,
		$backwards: 1,
		$both: 1,
		$infinite: 1,
		$paused: 1,
		$running: 1,
		$ease: 1,
		"$ease-in": 1,
		"$ease-out": 1,
		"$ease-in-out": 1,
		$linear: 1,
		"$step-end": 1,
		"$step-start": 1,
		$none: Infinity,
		$initial: Infinity,
		$inherit: Infinity,
		$unset: Infinity,
		$revert: Infinity,
		"$revert-layer": Infinity
	};
	function localizeDeclaration(declaration, context) {
		if (/animation(-name)?$/i.test(declaration.prop)) {
			let parsedAnimationKeywords = {};
			declaration.value = valueParser(declaration.value).walk((node) => {
				if (node.type === "div") {
					parsedAnimationKeywords = {};
					return;
				} else if (node.type === "function" && node.value.toLowerCase() === "local" && node.nodes.length === 1) {
					node.type = "word";
					node.value = node.nodes[0].value;
					return localizeDeclNode(node, {
						options: context.options,
						global: context.global,
						localizeNextItem: true,
						localAliasMap: context.localAliasMap
					});
				} else if (node.type === "function") {
					if (node.value.toLowerCase() === "global" && node.nodes.length === 1) {
						node.type = "word";
						node.value = node.nodes[0].value;
					}
					return false;
				} else if (node.type !== "word") return;
				const value = node.type === "word" ? node.value.toLowerCase() : null;
				let shouldParseAnimationName = false;
				if (value && validIdent.test(value)) if ("$" + value in animationKeywords) {
					parsedAnimationKeywords["$" + value] = "$" + value in parsedAnimationKeywords ? parsedAnimationKeywords["$" + value] + 1 : 0;
					shouldParseAnimationName = parsedAnimationKeywords["$" + value] >= animationKeywords["$" + value];
				} else shouldParseAnimationName = true;
				return localizeDeclNode(node, {
					options: context.options,
					global: context.global,
					localizeNextItem: shouldParseAnimationName && !context.global,
					localAliasMap: context.localAliasMap
				});
			}).toString();
			return;
		}
		if (/url\(/i.test(declaration.value)) return localizeDeclarationValues(false, declaration, context);
	}
	const isPureSelector = (context, rule) => {
		if (!rule.parent || rule.type === "root") return !context.hasPureGlobals;
		if (rule.type === "rule" && rule[isPureSelectorSymbol]) return rule[isPureSelectorSymbol] || isPureSelector(context, rule.parent);
		return !context.hasPureGlobals || isPureSelector(context, rule.parent);
	};
	const isNodeWithoutDeclarations = (rule) => {
		if (rule.nodes.length > 0) return !rule.nodes.every((item) => item.type === "rule" || item.type === "atrule" && !isNodeWithoutDeclarations(item));
		return true;
	};
	module.exports = (options = {}) => {
		if (options && options.mode && options.mode !== "global" && options.mode !== "local" && options.mode !== "pure") throw new Error("options.mode must be either \"global\", \"local\" or \"pure\" (default \"local\")");
		const pureMode = options && options.mode === "pure";
		const globalMode = options && options.mode === "global";
		return {
			postcssPlugin: "postcss-modules-local-by-default",
			prepare() {
				const localAliasMap = /* @__PURE__ */ new Map();
				return { Once(root) {
					const { icssImports } = extractICSS(root, false);
					const enforcePureMode = pureMode && !isPureCheckDisabled(root);
					Object.keys(icssImports).forEach((key) => {
						Object.keys(icssImports[key]).forEach((prop) => {
							localAliasMap.set(prop, icssImports[key][prop]);
						});
					});
					root.walkAtRules((atRule) => {
						if (/keyframes$/i.test(atRule.name)) {
							const globalMatch = /^\s*:global\s*\((.+)\)\s*$/.exec(atRule.params);
							const localMatch = /^\s*:local\s*\((.+)\)\s*$/.exec(atRule.params);
							let globalKeyframes = globalMode;
							if (globalMatch) {
								if (enforcePureMode) {
									const ignoreComment = getIgnoreComment(atRule);
									if (!ignoreComment) throw atRule.error("@keyframes :global(...) is not allowed in pure mode");
									else ignoreComment.remove();
								}
								atRule.params = globalMatch[1];
								globalKeyframes = true;
							} else if (localMatch) {
								atRule.params = localMatch[0];
								globalKeyframes = false;
							} else if (atRule.params && !globalMode && !localAliasMap.has(atRule.params)) atRule.params = ":local(" + atRule.params + ")";
							atRule.walkDecls((declaration) => {
								localizeDeclaration(declaration, {
									localAliasMap,
									options,
									global: globalKeyframes
								});
							});
						} else if (/scope$/i.test(atRule.name)) {
							if (atRule.params) {
								const ignoreComment = pureMode ? getIgnoreComment(atRule) : void 0;
								if (ignoreComment) ignoreComment.remove();
								atRule.params = atRule.params.split("to").map((item) => {
									const selector = item.trim().slice(1, -1).trim();
									const context = localizeNode(selector, options.mode, localAliasMap);
									context.options = options;
									context.localAliasMap = localAliasMap;
									if (enforcePureMode && context.hasPureGlobals && !ignoreComment) throw atRule.error("Selector in at-rule\"" + selector + "\" is not pure (pure selectors must contain at least one local class or id)");
									return `(${context.selector})`;
								}).join(" to ");
							}
							atRule.nodes.forEach((declaration) => {
								if (declaration.type === "decl") localizeDeclaration(declaration, {
									localAliasMap,
									options,
									global: globalMode
								});
							});
						} else if (atRule.nodes) atRule.nodes.forEach((declaration) => {
							if (declaration.type === "decl") localizeDeclaration(declaration, {
								localAliasMap,
								options,
								global: globalMode
							});
						});
					});
					root.walkRules((rule) => {
						if (rule.parent && rule.parent.type === "atrule" && /keyframes$/i.test(rule.parent.name)) return;
						const context = localizeNode(rule, options.mode, localAliasMap);
						context.options = options;
						context.localAliasMap = localAliasMap;
						const ignoreComment = enforcePureMode ? getIgnoreComment(rule) : void 0;
						const isNotPure = enforcePureMode && !isPureSelector(context, rule);
						if (isNotPure && isNodeWithoutDeclarations(rule) && !ignoreComment) throw rule.error("Selector \"" + rule.selector + "\" is not pure (pure selectors must contain at least one local class or id)");
						else if (ignoreComment) ignoreComment.remove();
						if (pureMode) rule[isPureSelectorSymbol] = !isNotPure;
						rule.selector = context.selector;
						if (rule.nodes) rule.nodes.forEach((declaration) => localizeDeclaration(declaration, context));
					});
				} };
			}
		};
	};
	module.exports.postcss = true;
}));
//#endregion
//#region ../../node_modules/.pnpm/postcss-modules-scope@3.2.1_postcss@8.5.15/node_modules/postcss-modules-scope/src/index.js
var require_src$1 = /* @__PURE__ */ __commonJSMin(((exports, module) => {
	const selectorParser = require_dist();
	const hasOwnProperty = Object.prototype.hasOwnProperty;
	function isNestedRule(rule) {
		if (!rule.parent || rule.parent.type === "root") return false;
		if (rule.parent.type === "rule") return true;
		return isNestedRule(rule.parent);
	}
	function getSingleLocalNamesForComposes(root, rule) {
		if (isNestedRule(rule)) throw new Error(`composition is not allowed in nested rule \n\n${rule}`);
		return root.nodes.map((node) => {
			if (node.type !== "selector" || node.nodes.length !== 1) throw new Error(`composition is only allowed when selector is single :local class name not in "${root}"`);
			node = node.nodes[0];
			if (node.type !== "pseudo" || node.value !== ":local" || node.nodes.length !== 1) throw new Error("composition is only allowed when selector is single :local class name not in \"" + root + "\", \"" + node + "\" is weird");
			node = node.first;
			if (node.type !== "selector" || node.length !== 1) throw new Error("composition is only allowed when selector is single :local class name not in \"" + root + "\", \"" + node + "\" is weird");
			node = node.first;
			if (node.type !== "class") throw new Error("composition is only allowed when selector is single :local class name not in \"" + root + "\", \"" + node + "\" is weird");
			return node.value;
		});
	}
	const unescapeRegExp = /* @__PURE__ */ new RegExp("\\\\([\\da-f]{1,6}[\\x20\\t\\r\\n\\f]?|([\\x20\\t\\r\\n\\f])|.)", "ig");
	function unescape(str) {
		return str.replace(unescapeRegExp, (_, escaped, escapedWhitespace) => {
			const high = "0x" + escaped - 65536;
			return high !== high || escapedWhitespace ? escaped : high < 0 ? String.fromCharCode(high + 65536) : String.fromCharCode(high >> 10 | 55296, high & 1023 | 56320);
		});
	}
	const plugin = (options = {}) => {
		const generateScopedName = options && options.generateScopedName || plugin.generateScopedName;
		const generateExportEntry = options && options.generateExportEntry || plugin.generateExportEntry;
		const exportGlobals = options && options.exportGlobals;
		return {
			postcssPlugin: "postcss-modules-scope",
			Once(root, { rule }) {
				const exports$1 = Object.create(null);
				function exportScopedName(name, rawName, node) {
					const scopedName = generateScopedName(rawName ? rawName : name, root.source.input.from, root.source.input.css, node);
					const { key, value } = generateExportEntry(rawName ? rawName : name, scopedName, root.source.input.from, root.source.input.css, node);
					exports$1[key] = exports$1[key] || [];
					if (exports$1[key].indexOf(value) < 0) exports$1[key].push(value);
					return scopedName;
				}
				function localizeNode(node) {
					switch (node.type) {
						case "selector":
							node.nodes = node.map((item) => localizeNode(item));
							return node;
						case "class": return selectorParser.className({ value: exportScopedName(node.value, node.raws && node.raws.value ? node.raws.value : null, node) });
						case "id": return selectorParser.id({ value: exportScopedName(node.value, node.raws && node.raws.value ? node.raws.value : null, node) });
						case "attribute": if (node.attribute === "class" && node.operator === "=") return selectorParser.attribute({
							attribute: node.attribute,
							operator: node.operator,
							quoteMark: "'",
							value: exportScopedName(node.value, null, null)
						});
					}
					throw new Error(`${node.type} ("${node}") is not allowed in a :local block`);
				}
				function traverseNode(node) {
					switch (node.type) {
						case "pseudo": if (node.value === ":local") {
							if (node.nodes.length !== 1) throw new Error("Unexpected comma (\",\") in :local block");
							const selector = localizeNode(node.first);
							selector.first.spaces = node.spaces;
							const nextNode = node.next();
							if (nextNode && nextNode.type === "combinator" && nextNode.value === " " && /\\[A-F0-9]{1,6}$/.test(selector.last.value)) selector.last.spaces.after = " ";
							node.replaceWith(selector);
							return;
						}
						case "root":
						case "selector":
							node.each((item) => traverseNode(item));
							break;
						case "id":
						case "class":
							if (exportGlobals) exports$1[node.value] = [node.value];
							break;
					}
					return node;
				}
				const importedNames = {};
				root.walkRules(/^:import\(.+\)$/, (rule) => {
					rule.walkDecls((decl) => {
						importedNames[decl.prop] = true;
					});
				});
				root.walkRules((rule) => {
					let parsedSelector = selectorParser().astSync(rule);
					rule.selector = traverseNode(parsedSelector.clone()).toString();
					rule.walkDecls(/^(composes|compose-with)$/i, (decl) => {
						const localNames = getSingleLocalNamesForComposes(parsedSelector, decl.parent);
						decl.value.split(",").forEach((value) => {
							value.trim().split(/\s+/).forEach((className) => {
								const global = /^global\(([^)]+)\)$/.exec(className);
								if (global) localNames.forEach((exportedName) => {
									exports$1[exportedName].push(global[1]);
								});
								else if (hasOwnProperty.call(importedNames, className)) localNames.forEach((exportedName) => {
									exports$1[exportedName].push(className);
								});
								else if (hasOwnProperty.call(exports$1, className)) localNames.forEach((exportedName) => {
									exports$1[className].forEach((item) => {
										exports$1[exportedName].push(item);
									});
								});
								else throw decl.error(`referenced class name "${className}" in ${decl.prop} not found`);
							});
						});
						decl.remove();
					});
					rule.walkDecls((decl) => {
						if (!/:local\s*\((.+?)\)/.test(decl.value)) return;
						let tokens = decl.value.split(/(,|'[^']*'|"[^"]*")/);
						tokens = tokens.map((token, idx) => {
							if (idx === 0 || tokens[idx - 1] === ",") {
								let result = token;
								const localMatch = /:local\s*\((.+?)\)/.exec(token);
								if (localMatch) {
									const input = localMatch.input;
									const matchPattern = localMatch[0];
									const matchVal = localMatch[1];
									const newVal = exportScopedName(matchVal);
									result = input.replace(matchPattern, newVal);
								} else return token;
								return result;
							} else return token;
						});
						decl.value = tokens.join("");
					});
				});
				root.walkAtRules(/keyframes$/i, (atRule) => {
					const localMatch = /^\s*:local\s*\((.+?)\)\s*$/.exec(atRule.params);
					if (!localMatch) return;
					atRule.params = exportScopedName(localMatch[1]);
				});
				root.walkAtRules(/scope$/i, (atRule) => {
					if (atRule.params) atRule.params = atRule.params.split("to").map((item) => {
						const selector = item.trim().slice(1, -1).trim();
						if (!/^\s*:local\s*\((.+?)\)\s*$/.exec(selector)) return `(${selector})`;
						return `(${traverseNode(selectorParser().astSync(selector)).toString()})`;
					}).join(" to ");
				});
				const exportedNames = Object.keys(exports$1);
				if (exportedNames.length > 0) {
					const exportRule = rule({ selector: ":export" });
					exportedNames.forEach((exportedName) => exportRule.append({
						prop: exportedName,
						value: exports$1[exportedName].join(" "),
						raws: { before: "\n  " }
					}));
					root.append(exportRule);
				}
			}
		};
	};
	plugin.postcss = true;
	plugin.generateScopedName = function(name, path) {
		return `_${path.replace(/\.[^./\\]+$/, "").replace(/[\W_]+/g, "_").replace(/^_|_$/g, "")}__${name}`.trim();
	};
	plugin.generateExportEntry = function(name, scopedName) {
		return {
			key: unescape(name),
			value: unescape(scopedName)
		};
	};
	module.exports = plugin;
}));
//#endregion
//#region ../../node_modules/.pnpm/string-hash@1.1.3/node_modules/string-hash/index.js
var require_string_hash = /* @__PURE__ */ __commonJSMin(((exports, module) => {
	function hash(str) {
		var hash = 5381, i = str.length;
		while (i) hash = hash * 33 ^ str.charCodeAt(--i);
		return hash >>> 0;
	}
	module.exports = hash;
}));
//#endregion
//#region ../../node_modules/.pnpm/postcss-modules-values@4.0.0_postcss@8.5.15/node_modules/postcss-modules-values/src/index.js
var require_src = /* @__PURE__ */ __commonJSMin(((exports, module) => {
	const ICSSUtils = require_src$4();
	const matchImports = /^(.+?|\([\s\S]+?\))\s+from\s+("[^"]*"|'[^']*'|[\w-]+)$/;
	const matchValueDefinition = /(?:\s+|^)([\w-]+):?(.*?)$/;
	const matchImport = /^([\w-]+)(?:\s+as\s+([\w-]+))?/;
	module.exports = (options) => {
		let importIndex = 0;
		const createImportedName = options && options.createImportedName || ((importName) => `i__const_${importName.replace(/\W/g, "_")}_${importIndex++}`);
		return {
			postcssPlugin: "postcss-modules-values",
			prepare(result) {
				const importAliases = [];
				const definitions = {};
				return { Once(root, postcss) {
					root.walkAtRules(/value/i, (atRule) => {
						const matches = atRule.params.match(matchImports);
						if (matches) {
							let [, aliases, path] = matches;
							if (definitions[path]) path = definitions[path];
							const imports = aliases.replace(/^\(\s*([\s\S]+)\s*\)$/, "$1").split(/\s*,\s*/).map((alias) => {
								const tokens = matchImport.exec(alias);
								if (tokens) {
									const [, theirName, myName = theirName] = tokens;
									const importedName = createImportedName(myName);
									definitions[myName] = importedName;
									return {
										theirName,
										importedName
									};
								} else throw new Error(`@import statement "${alias}" is invalid!`);
							});
							importAliases.push({
								path,
								imports
							});
							atRule.remove();
							return;
						}
						if (atRule.params.indexOf("@value") !== -1) result.warn("Invalid value definition: " + atRule.params);
						let [, key, value] = `${atRule.params}${atRule.raws.between}`.match(matchValueDefinition);
						const normalizedValue = value.replace(/\/\*((?!\*\/).*?)\*\//g, "");
						if (normalizedValue.length === 0) {
							result.warn("Invalid value definition: " + atRule.params);
							atRule.remove();
							return;
						}
						if (!/^\s+$/.test(normalizedValue)) value = value.trim();
						definitions[key] = ICSSUtils.replaceValueSymbols(value, definitions);
						atRule.remove();
					});
					if (!Object.keys(definitions).length) return;
					ICSSUtils.replaceSymbols(root, definitions);
					const exportDeclarations = Object.keys(definitions).map((key) => postcss.decl({
						value: definitions[key],
						prop: key,
						raws: { before: "\n  " }
					}));
					if (exportDeclarations.length > 0) {
						const exportRule = postcss.rule({
							selector: ":export",
							raws: { after: "\n" }
						});
						exportRule.append(exportDeclarations);
						root.prepend(exportRule);
					}
					importAliases.reverse().forEach(({ path, imports }) => {
						const importRule = postcss.rule({
							selector: `:import(${path})`,
							raws: { after: "\n" }
						});
						imports.forEach(({ theirName, importedName }) => {
							importRule.append({
								value: theirName,
								prop: importedName,
								raws: { before: "\n  " }
							});
						});
						root.prepend(importRule);
					});
				} };
			}
		};
	};
	module.exports.postcss = true;
}));
//#endregion
//#region ../../node_modules/.pnpm/postcss-modules@6.0.1_postcss@8.5.15/node_modules/postcss-modules/build/scoping.js
var require_scoping = /* @__PURE__ */ __commonJSMin(((exports) => {
	Object.defineProperty(exports, "__esModule", { value: true });
	exports.behaviours = void 0;
	exports.getDefaultPlugins = getDefaultPlugins;
	exports.getDefaultScopeBehaviour = getDefaultScopeBehaviour;
	exports.getScopedNameGenerator = getScopedNameGenerator;
	var _postcssModulesExtractImports = _interopRequireDefault(require_src$3());
	var _genericNames = _interopRequireDefault(require_generic_names());
	var _postcssModulesLocalByDefault = _interopRequireDefault(require_src$2());
	var _postcssModulesScope = _interopRequireDefault(require_src$1());
	var _stringHash = _interopRequireDefault(require_string_hash());
	var _postcssModulesValues = _interopRequireDefault(require_src());
	function _interopRequireDefault(obj) {
		return obj && obj.__esModule ? obj : { default: obj };
	}
	const behaviours = {
		LOCAL: "local",
		GLOBAL: "global"
	};
	exports.behaviours = behaviours;
	function getDefaultPlugins({ behaviour, generateScopedName, exportGlobals }) {
		const scope = (0, _postcssModulesScope.default)({
			generateScopedName,
			exportGlobals
		});
		return {
			[behaviours.LOCAL]: [
				_postcssModulesValues.default,
				(0, _postcssModulesLocalByDefault.default)({ mode: "local" }),
				_postcssModulesExtractImports.default,
				scope
			],
			[behaviours.GLOBAL]: [
				_postcssModulesValues.default,
				(0, _postcssModulesLocalByDefault.default)({ mode: "global" }),
				_postcssModulesExtractImports.default,
				scope
			]
		}[behaviour];
	}
	function isValidBehaviour(behaviour) {
		return Object.keys(behaviours).map((key) => behaviours[key]).indexOf(behaviour) > -1;
	}
	function getDefaultScopeBehaviour(scopeBehaviour) {
		return scopeBehaviour && isValidBehaviour(scopeBehaviour) ? scopeBehaviour : behaviours.LOCAL;
	}
	function generateScopedNameDefault(name, filename, css) {
		const i = css.indexOf(`.${name}`);
		const lineNumber = css.substr(0, i).split(/[\r\n]/).length;
		return `_${name}_${(0, _stringHash.default)(css).toString(36).substr(0, 5)}_${lineNumber}`;
	}
	function getScopedNameGenerator(generateScopedName, hashPrefix) {
		const scopedNameGenerator = generateScopedName || generateScopedNameDefault;
		if (typeof scopedNameGenerator === "function") return scopedNameGenerator;
		return (0, _genericNames.default)(scopedNameGenerator, {
			context: process.cwd(),
			hashPrefix
		});
	}
}));
//#endregion
//#region ../../node_modules/.pnpm/postcss-modules@6.0.1_postcss@8.5.15/node_modules/postcss-modules/build/pluginFactory.js
var require_pluginFactory = /* @__PURE__ */ __commonJSMin(((exports) => {
	Object.defineProperty(exports, "__esModule", { value: true });
	exports.makePlugin = makePlugin;
	var _postcss = _interopRequireDefault(__require("postcss"));
	var _unquote = _interopRequireDefault(require_unquote());
	var _Parser = _interopRequireDefault(require_Parser());
	var _saveJSON = _interopRequireDefault(require_saveJSON());
	var _localsConvention = require_localsConvention();
	var _FileSystemLoader = _interopRequireDefault(require_FileSystemLoader());
	var _scoping = require_scoping();
	function _interopRequireDefault(obj) {
		return obj && obj.__esModule ? obj : { default: obj };
	}
	const PLUGIN_NAME = "postcss-modules";
	function isGlobalModule(globalModules, inputFile) {
		return globalModules.some((regex) => inputFile.match(regex));
	}
	function getDefaultPluginsList(opts, inputFile) {
		const globalModulesList = opts.globalModulePaths || null;
		const exportGlobals = opts.exportGlobals || false;
		const defaultBehaviour = (0, _scoping.getDefaultScopeBehaviour)(opts.scopeBehaviour);
		const generateScopedName = (0, _scoping.getScopedNameGenerator)(opts.generateScopedName, opts.hashPrefix);
		if (globalModulesList && isGlobalModule(globalModulesList, inputFile)) return (0, _scoping.getDefaultPlugins)({
			behaviour: _scoping.behaviours.GLOBAL,
			generateScopedName,
			exportGlobals
		});
		return (0, _scoping.getDefaultPlugins)({
			behaviour: defaultBehaviour,
			generateScopedName,
			exportGlobals
		});
	}
	function getLoader(opts, plugins) {
		const root = typeof opts.root === "undefined" ? "/" : opts.root;
		return typeof opts.Loader === "function" ? new opts.Loader(root, plugins, opts.resolve) : new _FileSystemLoader.default(root, plugins, opts.resolve);
	}
	function isOurPlugin(plugin) {
		return plugin.postcssPlugin === PLUGIN_NAME;
	}
	function makePlugin(opts) {
		return {
			postcssPlugin: PLUGIN_NAME,
			async OnceExit(css, { result }) {
				const getJSON = opts.getJSON || _saveJSON.default;
				const inputFile = css.source.input.file;
				const pluginList = getDefaultPluginsList(opts, inputFile);
				const resultPluginIndex = result.processor.plugins.findIndex((plugin) => isOurPlugin(plugin));
				if (resultPluginIndex === -1) throw new Error("Plugin missing from options.");
				const loader = getLoader(opts, [...result.processor.plugins.slice(0, resultPluginIndex), ...pluginList]);
				const fetcher = async (file, relativeTo, depTrace) => {
					const unquoteFile = (0, _unquote.default)(file);
					return loader.fetch.call(loader, unquoteFile, relativeTo, depTrace);
				};
				const parser = new _Parser.default(fetcher);
				await (0, _postcss.default)([...pluginList, parser.plugin()]).process(css, { from: inputFile });
				const out = loader.finalSource;
				if (out) css.prepend(out);
				if (opts.localsConvention) {
					const reducer = (0, _localsConvention.makeLocalsConventionReducer)(opts.localsConvention, inputFile);
					parser.exportTokens = Object.entries(parser.exportTokens).reduce(reducer, {});
				}
				result.messages.push({
					type: "export",
					plugin: "postcss-modules",
					exportTokens: parser.exportTokens
				});
				return getJSON(css.source.input.file, parser.exportTokens, result.opts.to);
			}
		};
	}
}));
//#endregion
//#region ../../node_modules/.pnpm/postcss-modules@6.0.1_postcss@8.5.15/node_modules/postcss-modules/build/index.js
var require_build = /* @__PURE__ */ __commonJSMin(((exports, module) => {
	var _fs = __require("fs");
	var _fs2 = require_fs();
	var _pluginFactory = require_pluginFactory();
	(0, _fs2.setFileSystem)({
		readFile: _fs.readFile,
		writeFile: _fs.writeFile
	});
	module.exports = (opts = {}) => (0, _pluginFactory.makePlugin)(opts);
	module.exports.postcss = true;
}));
//#endregion
export default require_build();
export {};
