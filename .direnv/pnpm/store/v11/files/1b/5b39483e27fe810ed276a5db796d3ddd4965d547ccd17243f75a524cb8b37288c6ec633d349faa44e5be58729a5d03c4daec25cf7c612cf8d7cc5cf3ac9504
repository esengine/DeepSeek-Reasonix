(function (global, factory) {
	typeof exports === 'object' && typeof module !== 'undefined' ? factory(exports) :
	typeof define === 'function' && define.amd ? define(['exports'], factory) :
	(global = global || self, factory(global.window = global.window || {}));
}(this, (function (exports) { 'use strict';

	var _trimExp = /(?:^\s+|\s+$)/g;
	var emojiExp = /([\uD800-\uDBFF][\uDC00-\uDFFF](?:[\u200D\uFE0F][\uD800-\uDBFF][\uDC00-\uDFFF]){2,}|\uD83D\uDC69(?:\u200D(?:(?:\uD83D\uDC69\u200D)?\uD83D\uDC67|(?:\uD83D\uDC69\u200D)?\uD83D\uDC66)|\uD83C[\uDFFB-\uDFFF])|\uD83D\uDC69\u200D(?:\uD83D\uDC69\u200D)?\uD83D\uDC66\u200D\uD83D\uDC66|\uD83D\uDC69\u200D(?:\uD83D\uDC69\u200D)?\uD83D\uDC67\u200D(?:\uD83D[\uDC66\uDC67])|\uD83C\uDFF3\uFE0F\u200D\uD83C\uDF08|(?:\uD83C[\uDFC3\uDFC4\uDFCA]|\uD83D[\uDC6E\uDC71\uDC73\uDC77\uDC81\uDC82\uDC86\uDC87\uDE45-\uDE47\uDE4B\uDE4D\uDE4E\uDEA3\uDEB4-\uDEB6]|\uD83E[\uDD26\uDD37-\uDD39\uDD3D\uDD3E\uDDD6-\uDDDD])(?:\uD83C[\uDFFB-\uDFFF])\u200D[\u2642\u2640]\uFE0F|\uD83D\uDC69(?:\uD83C[\uDFFB-\uDFFF])\u200D(?:\uD83C[\uDF3E\uDF73\uDF93\uDFA4\uDFA8\uDFEB\uDFED]|\uD83D[\uDCBB\uDD27\uDCBC\uDD2C\uDE80\uDE92])|(?:\uD83C[\uDFC3\uDFC4\uDFCA]|\uD83D[\uDC6E\uDC6F\uDC71\uDC73\uDC77\uDC81\uDC82\uDC86\uDC87\uDE45-\uDE47\uDE4B\uDE4D\uDE4E\uDEA3\uDEB4-\uDEB6]|\uD83E[\uDD26\uDD37-\uDD39\uDD3C-\uDD3E\uDDD6-\uDDDF])\u200D[\u2640\u2642]\uFE0F|\uD83C\uDDFD\uD83C\uDDF0|\uD83C\uDDF6\uD83C\uDDE6|\uD83C\uDDF4\uD83C\uDDF2|\uD83C\uDDE9(?:\uD83C[\uDDEA\uDDEC\uDDEF\uDDF0\uDDF2\uDDF4\uDDFF])|\uD83C\uDDF7(?:\uD83C[\uDDEA\uDDF4\uDDF8\uDDFA\uDDFC])|\uD83C\uDDE8(?:\uD83C[\uDDE6\uDDE8\uDDE9\uDDEB-\uDDEE\uDDF0-\uDDF5\uDDF7\uDDFA-\uDDFF])|(?:\u26F9|\uD83C[\uDFCC\uDFCB]|\uD83D\uDD75)(?:\uFE0F\u200D[\u2640\u2642]|(?:\uD83C[\uDFFB-\uDFFF])\u200D[\u2640\u2642])\uFE0F|(?:\uD83D\uDC41\uFE0F\u200D\uD83D\uDDE8|\uD83D\uDC69(?:\uD83C[\uDFFB-\uDFFF])\u200D[\u2695\u2696\u2708]|\uD83D\uDC69\u200D[\u2695\u2696\u2708]|\uD83D\uDC68(?:(?:\uD83C[\uDFFB-\uDFFF])\u200D[\u2695\u2696\u2708]|\u200D[\u2695\u2696\u2708]))\uFE0F|\uD83C\uDDF2(?:\uD83C[\uDDE6\uDDE8-\uDDED\uDDF0-\uDDFF])|\uD83D\uDC69\u200D(?:\uD83C[\uDF3E\uDF73\uDF93\uDFA4\uDFA8\uDFEB\uDFED]|\uD83D[\uDCBB\uDCBC\uDD27\uDD2C\uDE80\uDE92]|\u2764\uFE0F\u200D(?:\uD83D\uDC8B\u200D(?:\uD83D[\uDC68\uDC69])|\uD83D[\uDC68\uDC69]))|\uD83C\uDDF1(?:\uD83C[\uDDE6-\uDDE8\uDDEE\uDDF0\uDDF7-\uDDFB\uDDFE])|\uD83C\uDDEF(?:\uD83C[\uDDEA\uDDF2\uDDF4\uDDF5])|\uD83C\uDDED(?:\uD83C[\uDDF0\uDDF2\uDDF3\uDDF7\uDDF9\uDDFA])|\uD83C\uDDEB(?:\uD83C[\uDDEE-\uDDF0\uDDF2\uDDF4\uDDF7])|[#\*0-9]\uFE0F\u20E3|\uD83C\uDDE7(?:\uD83C[\uDDE6\uDDE7\uDDE9-\uDDEF\uDDF1-\uDDF4\uDDF6-\uDDF9\uDDFB\uDDFC\uDDFE\uDDFF])|\uD83C\uDDE6(?:\uD83C[\uDDE8-\uDDEC\uDDEE\uDDF1\uDDF2\uDDF4\uDDF6-\uDDFA\uDDFC\uDDFD\uDDFF])|\uD83C\uDDFF(?:\uD83C[\uDDE6\uDDF2\uDDFC])|\uD83C\uDDF5(?:\uD83C[\uDDE6\uDDEA-\uDDED\uDDF0-\uDDF3\uDDF7-\uDDF9\uDDFC\uDDFE])|\uD83C\uDDFB(?:\uD83C[\uDDE6\uDDE8\uDDEA\uDDEC\uDDEE\uDDF3\uDDFA])|\uD83C\uDDF3(?:\uD83C[\uDDE6\uDDE8\uDDEA-\uDDEC\uDDEE\uDDF1\uDDF4\uDDF5\uDDF7\uDDFA\uDDFF])|\uD83C\uDFF4\uDB40\uDC67\uDB40\uDC62(?:\uDB40\uDC77\uDB40\uDC6C\uDB40\uDC73|\uDB40\uDC73\uDB40\uDC63\uDB40\uDC74|\uDB40\uDC65\uDB40\uDC6E\uDB40\uDC67)\uDB40\uDC7F|\uD83D\uDC68(?:\u200D(?:\u2764\uFE0F\u200D(?:\uD83D\uDC8B\u200D)?\uD83D\uDC68|(?:(?:\uD83D[\uDC68\uDC69])\u200D)?\uD83D\uDC66\u200D\uD83D\uDC66|(?:(?:\uD83D[\uDC68\uDC69])\u200D)?\uD83D\uDC67\u200D(?:\uD83D[\uDC66\uDC67])|\uD83C[\uDF3E\uDF73\uDF93\uDFA4\uDFA8\uDFEB\uDFED]|\uD83D[\uDCBB\uDCBC\uDD27\uDD2C\uDE80\uDE92])|(?:\uD83C[\uDFFB-\uDFFF])\u200D(?:\uD83C[\uDF3E\uDF73\uDF93\uDFA4\uDFA8\uDFEB\uDFED]|\uD83D[\uDCBB\uDCBC\uDD27\uDD2C\uDE80\uDE92]))|\uD83C\uDDF8(?:\uD83C[\uDDE6-\uDDEA\uDDEC-\uDDF4\uDDF7-\uDDF9\uDDFB\uDDFD-\uDDFF])|\uD83C\uDDF0(?:\uD83C[\uDDEA\uDDEC-\uDDEE\uDDF2\uDDF3\uDDF5\uDDF7\uDDFC\uDDFE\uDDFF])|\uD83C\uDDFE(?:\uD83C[\uDDEA\uDDF9])|\uD83C\uDDEE(?:\uD83C[\uDDE8-\uDDEA\uDDF1-\uDDF4\uDDF6-\uDDF9])|\uD83C\uDDF9(?:\uD83C[\uDDE6\uDDE8\uDDE9\uDDEB-\uDDED\uDDEF-\uDDF4\uDDF7\uDDF9\uDDFB\uDDFC\uDDFF])|\uD83C\uDDEC(?:\uD83C[\uDDE6\uDDE7\uDDE9-\uDDEE\uDDF1-\uDDF3\uDDF5-\uDDFA\uDDFC\uDDFE])|\uD83C\uDDFA(?:\uD83C[\uDDE6\uDDEC\uDDF2\uDDF3\uDDF8\uDDFE\uDDFF])|\uD83C\uDDEA(?:\uD83C[\uDDE6\uDDE8\uDDEA\uDDEC\uDDED\uDDF7-\uDDFA])|\uD83C\uDDFC(?:\uD83C[\uDDEB\uDDF8])|(?:\u26F9|\uD83C[\uDFCB\uDFCC]|\uD83D\uDD75)(?:\uD83C[\uDFFB-\uDFFF])|(?:\uD83C[\uDFC3\uDFC4\uDFCA]|\uD83D[\uDC6E\uDC71\uDC73\uDC77\uDC81\uDC82\uDC86\uDC87\uDE45-\uDE47\uDE4B\uDE4D\uDE4E\uDEA3\uDEB4-\uDEB6]|\uD83E[\uDD26\uDD37-\uDD39\uDD3D\uDD3E\uDDD6-\uDDDD])(?:\uD83C[\uDFFB-\uDFFF])|(?:[\u261D\u270A-\u270D]|\uD83C[\uDF85\uDFC2\uDFC7]|\uD83D[\uDC42\uDC43\uDC46-\uDC50\uDC66\uDC67\uDC70\uDC72\uDC74-\uDC76\uDC78\uDC7C\uDC83\uDC85\uDCAA\uDD74\uDD7A\uDD90\uDD95\uDD96\uDE4C\uDE4F\uDEC0\uDECC]|\uD83E[\uDD18-\uDD1C\uDD1E\uDD1F\uDD30-\uDD36\uDDD1-\uDDD5])(?:\uD83C[\uDFFB-\uDFFF])|\uD83D\uDC68(?:\u200D(?:(?:(?:\uD83D[\uDC68\uDC69])\u200D)?\uD83D\uDC67|(?:(?:\uD83D[\uDC68\uDC69])\u200D)?\uD83D\uDC66)|\uD83C[\uDFFB-\uDFFF])|(?:[\u261D\u26F9\u270A-\u270D]|\uD83C[\uDF85\uDFC2-\uDFC4\uDFC7\uDFCA-\uDFCC]|\uD83D[\uDC42\uDC43\uDC46-\uDC50\uDC66-\uDC69\uDC6E\uDC70-\uDC78\uDC7C\uDC81-\uDC83\uDC85-\uDC87\uDCAA\uDD74\uDD75\uDD7A\uDD90\uDD95\uDD96\uDE45-\uDE47\uDE4B-\uDE4F\uDEA3\uDEB4-\uDEB6\uDEC0\uDECC]|\uD83E[\uDD18-\uDD1C\uDD1E\uDD1F\uDD26\uDD30-\uDD39\uDD3D\uDD3E\uDDD1-\uDDDD])(?:\uD83C[\uDFFB-\uDFFF])?|(?:[\u231A\u231B\u23E9-\u23EC\u23F0\u23F3\u25FD\u25FE\u2614\u2615\u2648-\u2653\u267F\u2693\u26A1\u26AA\u26AB\u26BD\u26BE\u26C4\u26C5\u26CE\u26D4\u26EA\u26F2\u26F3\u26F5\u26FA\u26FD\u2705\u270A\u270B\u2728\u274C\u274E\u2753-\u2755\u2757\u2795-\u2797\u27B0\u27BF\u2B1B\u2B1C\u2B50\u2B55]|\uD83C[\uDC04\uDCCF\uDD8E\uDD91-\uDD9A\uDDE6-\uDDFF\uDE01\uDE1A\uDE2F\uDE32-\uDE36\uDE38-\uDE3A\uDE50\uDE51\uDF00-\uDF20\uDF2D-\uDF35\uDF37-\uDF7C\uDF7E-\uDF93\uDFA0-\uDFCA\uDFCF-\uDFD3\uDFE0-\uDFF0\uDFF4\uDFF8-\uDFFF]|\uD83D[\uDC00-\uDC3E\uDC40\uDC42-\uDCFC\uDCFF-\uDD3D\uDD4B-\uDD4E\uDD50-\uDD67\uDD7A\uDD95\uDD96\uDDA4\uDDFB-\uDE4F\uDE80-\uDEC5\uDECC\uDED0-\uDED2\uDEEB\uDEEC\uDEF4-\uDEF8]|\uD83E[\uDD10-\uDD3A\uDD3C-\uDD3E\uDD40-\uDD45\uDD47-\uDD4C\uDD50-\uDD6B\uDD80-\uDD97\uDDC0\uDDD0-\uDDE6])|(?:[#\*0-9\xA9\xAE\u203C\u2049\u2122\u2139\u2194-\u2199\u21A9\u21AA\u231A\u231B\u2328\u23CF\u23E9-\u23F3\u23F8-\u23FA\u24C2\u25AA\u25AB\u25B6\u25C0\u25FB-\u25FE\u2600-\u2604\u260E\u2611\u2614\u2615\u2618\u261D\u2620\u2622\u2623\u2626\u262A\u262E\u262F\u2638-\u263A\u2640\u2642\u2648-\u2653\u2660\u2663\u2665\u2666\u2668\u267B\u267F\u2692-\u2697\u2699\u269B\u269C\u26A0\u26A1\u26AA\u26AB\u26B0\u26B1\u26BD\u26BE\u26C4\u26C5\u26C8\u26CE\u26CF\u26D1\u26D3\u26D4\u26E9\u26EA\u26F0-\u26F5\u26F7-\u26FA\u26FD\u2702\u2705\u2708-\u270D\u270F\u2712\u2714\u2716\u271D\u2721\u2728\u2733\u2734\u2744\u2747\u274C\u274E\u2753-\u2755\u2757\u2763\u2764\u2795-\u2797\u27A1\u27B0\u27BF\u2934\u2935\u2B05-\u2B07\u2B1B\u2B1C\u2B50\u2B55\u3030\u303D\u3297\u3299]|\uD83C[\uDC04\uDCCF\uDD70\uDD71\uDD7E\uDD7F\uDD8E\uDD91-\uDD9A\uDDE6-\uDDFF\uDE01\uDE02\uDE1A\uDE2F\uDE32-\uDE3A\uDE50\uDE51\uDF00-\uDF21\uDF24-\uDF93\uDF96\uDF97\uDF99-\uDF9B\uDF9E-\uDFF0\uDFF3-\uDFF5\uDFF7-\uDFFF]|\uD83D[\uDC00-\uDCFD\uDCFF-\uDD3D\uDD49-\uDD4E\uDD50-\uDD67\uDD6F\uDD70\uDD73-\uDD7A\uDD87\uDD8A-\uDD8D\uDD90\uDD95\uDD96\uDDA4\uDDA5\uDDA8\uDDB1\uDDB2\uDDBC\uDDC2-\uDDC4\uDDD1-\uDDD3\uDDDC-\uDDDE\uDDE1\uDDE3\uDDE8\uDDEF\uDDF3\uDDFA-\uDE4F\uDE80-\uDEC5\uDECB-\uDED2\uDEE0-\uDEE5\uDEE9\uDEEB\uDEEC\uDEF0\uDEF3-\uDEF8]|\uD83E[\uDD10-\uDD3A\uDD3C-\uDD3E\uDD40-\uDD45\uDD47-\uDD4C\uDD50-\uDD6B\uDD80-\uDD97\uDDC0\uDDD0-\uDDE6])\uFE0F)/;
	function getText(e) {
	  var type = e.nodeType,
	      result = "";

	  if (type === 1 || type === 9 || type === 11) {
	    if (typeof e.textContent === "string") {
	      return e.textContent;
	    } else {
	      for (e = e.firstChild; e; e = e.nextSibling) {
	        result += getText(e);
	      }
	    }
	  } else if (type === 3 || type === 4) {
	    return e.nodeValue;
	  }

	  return result;
	}
	function emojiSafeSplit(text, delimiter, trim, preserveSpaces, unescapedCharCodes) {
	  text += "";
	  trim && (text = text.trim ? text.trim() : text.replace(_trimExp, ""));

	  if (delimiter && delimiter !== "") {
	    return text.replace(/>/g, "&gt;").replace(/</g, "&lt;").split(delimiter);
	  }

	  var result = [],
	      l = text.length,
	      i = 0,
	      j,
	      character;

	  for (; i < l; i++) {
	    character = text.charAt(i);

	    if (character.charCodeAt(0) >= 0xD800 && character.charCodeAt(0) <= 0xDBFF || text.charCodeAt(i + 1) >= 0xFE00 && text.charCodeAt(i + 1) <= 0xFE0F) {
	      j = ((text.substr(i, 12).split(emojiExp) || [])[1] || "").length || 2;
	      character = text.substr(i, j);
	      result.emoji = 1;
	      i += j - 1;
	    }

	    result.push(unescapedCharCodes ? character : character === ">" ? "&gt;" : character === "<" ? "&lt;" : preserveSpaces && character === " " && (text.charAt(i - 1) === " " || text.charAt(i + 1) === " ") ? "&nbsp;" : character);
	  }

	  return result;
	}

	/*!
	 * ScrambleTextPlugin 3.15.0
	 * https://gsap.com
	 *
	 * @license Copyright 2008-2026, GreenSock. All rights reserved.
	 * Subject to the terms at https://gsap.com/standard-license
	 * @author: Jack Doyle, jack@greensock.com
	*/

	var CharSet = function () {
	  function CharSet(chars) {
	    this.chars = emojiSafeSplit(chars);
	    this.sets = [];
	    this.length = 50;

	    for (var i = 0; i < 20; i++) {
	      this.sets[i] = _scrambleText(80, this.chars);
	    }
	  }

	  var _proto = CharSet.prototype;

	  _proto.grow = function grow(newLength) {
	    for (var i = 0; i < 20; i++) {
	      this.sets[i] += _scrambleText(newLength - this.length, this.chars);
	    }

	    this.length = newLength;
	  };

	  return CharSet;
	}();

	var gsap,
	    _coreInitted,
	    _getGSAP = function _getGSAP() {
	  return gsap || typeof window !== "undefined" && (gsap = window.gsap) && gsap.registerPlugin && gsap;
	},
	    _bonusValidated = 1,
	    _spacesExp = /\s+/g,
	    _scrambleText = function _scrambleText(length, chars) {
	  var l = chars.length,
	      s = "";

	  while (--length > -1) {
	    s += chars[~~(Math.random() * l)];
	  }

	  return s;
	},
	    _upper = "ABCDEFGHIJKLMNOPQRSTUVWXYZ",
	    _lower = _upper.toLowerCase(),
	    _charsLookup = {
	  upperCase: new CharSet(_upper),
	  lowerCase: new CharSet(_lower),
	  upperAndLowerCase: new CharSet(_upper + _lower)
	},
	    _initCore = function _initCore() {
	  _coreInitted = gsap = _getGSAP();
	};

	var ScrambleTextPlugin = {
	  version: "3.15.0",
	  name: "scrambleText",
	  register: function register(core, Plugin, propTween) {
	    gsap = core;

	    _initCore();
	  },
	  init: function init(target, value, tween, index, targets) {
	    _coreInitted || _initCore();
	    this.prop = "innerHTML" in target ? "innerHTML" : "textContent" in target ? "textContent" : 0;

	    if (!this.prop) {
	      return;
	    }

	    this.target = target;

	    if (typeof value !== "object") {
	      value = {
	        text: value
	      };
	    }

	    var text = value.text || value.value || "",
	        trim = value.trim !== false,
	        data = this,
	        delim,
	        maxLength,
	        charset,
	        splitByChars;
	    data.delimiter = delim = value.delimiter || "";
	    data.original = emojiSafeSplit(getText(target).replace(_spacesExp, " ").split("&nbsp;").join(""), delim, trim);

	    if (text === "{original}" || text === true || text == null) {
	      text = data.original.join(delim);
	    }

	    data.text = emojiSafeSplit((text || "").replace(_spacesExp, " "), delim, trim);
	    data.hasClass = !!(value.newClass || value.oldClass);
	    data.newClass = value.newClass;
	    data.oldClass = value.oldClass;
	    splitByChars = delim === "";
	    data.textHasEmoji = splitByChars && !!data.text.emoji;
	    data.charsHaveEmoji = !!value.chars && !!emojiSafeSplit(value.chars).emoji;
	    data.length = splitByChars ? data.original.length : data.original.join(delim).length;
	    data.lengthDif = (splitByChars ? data.text.length : data.text.join(delim).length) - data.length;
	    data.fillChar = value.fillChar || value.chars && ~value.chars.indexOf(" ") ? "&nbsp;" : "";
	    data.charSet = charset = _charsLookup[value.chars || "upperCase"] || new CharSet(value.chars);
	    data.speed = 0.05 / (value.speed || 1);
	    data.prevScrambleTime = 0;
	    data.setIndex = Math.random() * 20 | 0;
	    maxLength = data.length + Math.max(data.lengthDif, 0);

	    if (maxLength > charset.length) {
	      charset.grow(maxLength);
	    }

	    data.chars = charset.sets[data.setIndex];
	    data.revealDelay = value.revealDelay || 0;
	    data.tweenLength = value.tweenLength !== false;
	    data.tween = tween;
	    data.rightToLeft = !!value.rightToLeft;

	    data._props.push("scrambleText", "text");

	    return _bonusValidated;
	  },
	  render: function render(ratio, data) {
	    var target = data.target,
	        prop = data.prop,
	        text = data.text,
	        delimiter = data.delimiter,
	        tween = data.tween,
	        prevScrambleTime = data.prevScrambleTime,
	        revealDelay = data.revealDelay,
	        setIndex = data.setIndex,
	        chars = data.chars,
	        charSet = data.charSet,
	        length = data.length,
	        textHasEmoji = data.textHasEmoji,
	        charsHaveEmoji = data.charsHaveEmoji,
	        lengthDif = data.lengthDif,
	        tweenLength = data.tweenLength,
	        oldClass = data.oldClass,
	        newClass = data.newClass,
	        rightToLeft = data.rightToLeft,
	        fillChar = data.fillChar,
	        speed = data.speed,
	        original = data.original,
	        hasClass = data.hasClass,
	        l = text.length,
	        time = tween._time,
	        timeDif = time - prevScrambleTime,
	        i,
	        i2,
	        startText,
	        endText,
	        applyNew,
	        applyOld,
	        str,
	        startClass,
	        endClass,
	        position,
	        r;

	    if (revealDelay) {
	      if (tween._from) {
	        time = tween._dur - time;
	      }

	      ratio = time === 0 ? 0 : time < revealDelay ? 0.000001 : time === tween._dur ? 1 : tween._ease((time - revealDelay) / (tween._dur - revealDelay));
	    }

	    if (ratio < 0) {
	      ratio = 0;
	    } else if (ratio > 1) {
	      ratio = 1;
	    }

	    if (rightToLeft) {
	      ratio = 1 - ratio;
	    }

	    i = ~~(ratio * l + 0.5);

	    if (ratio) {
	      if (timeDif > speed || timeDif < -speed) {
	        data.setIndex = setIndex = (setIndex + (Math.random() * 19 | 0)) % 20;
	        data.chars = charSet.sets[setIndex];
	        data.prevScrambleTime += timeDif;
	      }

	      endText = chars;
	    } else {
	      endText = original.join(delimiter);
	    }

	    r = tween._from ? ratio : 1 - ratio;
	    position = length + (tweenLength ? tween._from ? r * r * r : 1 - r * r * r : 1) * lengthDif;

	    if (rightToLeft) {
	      if (ratio === 1 && (tween._from || tween.data === "isFromStart")) {
	        startText = "";
	        endText = original.join(delimiter);
	      } else {
	        str = text.slice(i).join(delimiter);

	        if (charsHaveEmoji) {
	          startText = emojiSafeSplit(endText).slice(0, position - (textHasEmoji ? emojiSafeSplit(str) : str).length + 0.5 | 0).join("");
	        } else {
	          startText = endText.substr(0, position - (textHasEmoji ? emojiSafeSplit(str) : str).length + 0.5 | 0);
	        }

	        endText = str;
	      }
	    } else {
	      startText = text.slice(0, i).join(delimiter);
	      i2 = (textHasEmoji ? emojiSafeSplit(startText) : startText).length;

	      if (charsHaveEmoji) {
	        endText = emojiSafeSplit(endText).slice(i2, position + 0.5 | 0).join("");
	      } else {
	        endText = endText.substr(i2, position - i2 + 0.5 | 0);
	      }
	    }

	    if (hasClass) {
	      startClass = rightToLeft ? oldClass : newClass;
	      endClass = rightToLeft ? newClass : oldClass;
	      applyNew = startClass && i !== 0;
	      applyOld = endClass && i !== l;
	      str = (applyNew ? "<span class='" + startClass + "'>" : "") + startText + (applyNew ? "</span>" : "") + (applyOld ? "<span class='" + endClass + "'>" : "") + delimiter + endText + (applyOld ? "</span>" : "");
	    } else {
	      str = startText + delimiter + endText;
	    }

	    target[prop] = fillChar === "&nbsp;" && ~str.indexOf("  ") ? str.split("  ").join("&nbsp;&nbsp;") : str;
	  }
	};
	ScrambleTextPlugin.emojiSafeSplit = emojiSafeSplit;
	ScrambleTextPlugin.getText = getText;
	_getGSAP() && gsap.registerPlugin(ScrambleTextPlugin);

	exports.ScrambleTextPlugin = ScrambleTextPlugin;
	exports.default = ScrambleTextPlugin;

	Object.defineProperty(exports, '__esModule', { value: true });

})));
